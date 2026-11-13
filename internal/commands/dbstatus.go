package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bogie-go/credentials/pkg/credentials"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// toolTasks are colon tasks the tool runs itself. They are not in the proxy
// table: that table shells out to the app, and these must not.
var toolTasks = []string{"db:status"}

// The three hints db:status adds to a connection failure. The wording is the
// whole hint: one line, after the error the driver returned.
const (
	hintNotRunning  = "Postgres isn't running (try `docker compose up -d`)"
	hintCredentials = "check credentials"
	hintPrepare     = "run `bogie db:prepare`"
)

// How long a status check waits. A refused connection returns at once; this
// only bounds a server that accepts the TCP handshake and then says nothing,
// so the command fails in the foreground instead of sitting on the kernel's
// timeout.
const statusTimeout = 15 * time.Second

const dbStatusUsage = `Usage: bogie db:status
       bogie db status

Prints where the app's server would connect, and what is there: the host,
port and database, the Postgres version, the latest applied goose migration
and how many migration files are not applied yet, and how many tables exist.
It only reads. A generated app gains no command and no import.

On a connection failure it keeps the driver's error, adds a one-line hint,
and exits non-zero.

Run it from anywhere inside the app (found by its bogie.toml).
`

// DBStatus handles `bogie db:status` (and `bogie db status`).
//
// It prints where the app's server would connect, and what is there: the
// Postgres version, the latest applied goose migration and how many migration
// files are not applied yet, and how many tables exist. It only reads. The
// generated app gains no command and no import; the question is asked from a
// laptop, and the binary in the container does not need it.
func DBStatus(args []string, out io.Writer) error {
	if WantsHelp(args) {
		_, _ = fmt.Fprint(out, dbStatusUsage)
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("db:status: takes no arguments (bogie db:status -h)")
	}
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	settings, err := readSettings(root)
	if err != nil {
		return err
	}
	databaseURL, err := databaseURL(root, settings.Name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	report, err := inspectDatabase(ctx, databaseURL, filepath.Join(root, "db", "migrate"))
	if err != nil {
		return withHint(err)
	}
	_, err = fmt.Fprint(out, report.String())
	return err
}

// dbReport is what a successful db:status prints, one fact per line.
type dbReport struct {
	Host      string
	Port      string
	Database  string
	Postgres  string
	Migration int64
	Pending   int
	Tables    int
}

func (r dbReport) String() string {
	return fmt.Sprintf("%-10s %s\n%-10s %s\n%-10s %s\n%-10s %s\n%-10s %d (%d pending)\n%-10s %d\n",
		"host:", r.Host,
		"port:", r.Port,
		"database:", r.Database,
		"postgres:", r.Postgres,
		"migration:", r.Migration, r.Pending,
		"tables:", r.Tables,
	)
}

// appConfig is the slice of the app's config db:status needs. The tags match
// config.Load, so credentials.ApplyEnvOverrides and a credentials file resolve
// database.url to DATABASE_URL the same way the server does.
type appConfig struct {
	Database struct {
		URL string `mapstructure:"url"`
	} `mapstructure:"database"`
}

// databaseURL is the URL `bogie server` would use, resolved from the app root
// rather than the process's working directory: the server is started with its
// cwd set to the app, and this command is reachable from any subdirectory.
//
// The order matches config.Load. Test never reads a credentials file and never
// reads DATABASE_URL; its URL is <NAME>_TEST_DATABASE_URL or the compose
// default. Everywhere else a credentials file is read when one exists, and an
// environment variable wins over the file.
func databaseURL(root, name string) (string, error) {
	prefix := strings.ToUpper(name)
	envName := prefix + "_ENV"
	env := envOr(envName, "development")
	switch env {
	case "development", "test", "production":
	default:
		return "", fmt.Errorf("config: %s=%q; must be one of %v", envName, env, []string{"development", "test", "production"})
	}
	credentialsEnv := envOr(prefix+"_CREDENTIALS", env)

	cfg := appConfig{}
	cfg.Database.URL = defaultDatabaseURL(name, env)

	dir := envOr(prefix+"_CONFIG_DIR", "config")
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}

	restore, err := useMasterKey(prefix)
	if err != nil {
		return "", err
	}
	defer restore()

	scoped, _ := credentials.EnvironmentPaths(dir, credentialsEnv)
	shared, _ := credentials.EnvironmentPaths(dir, "")

	switch {
	case env == "test":
		// ApplyEnvOverrides may fill the URL from DATABASE_URL. Test throws
		// that away: the suite truncates every table, so a URL exported for
		// development must not be the one this environment uses.
		if err := credentials.ApplyEnvOverrides(&cfg); err != nil {
			return "", fmt.Errorf("config: %w", err)
		}
		cfg.Database.URL = envOr(prefix+"_TEST_DATABASE_URL", defaultDatabaseURL(name, "test"))
	case fileExists(scoped), fileExists(shared):
		reader := credentials.NewConfigReader(dir)
		// The app's files are flat: one environment's settings, no section
		// per environment. Without this the shared file's database.url is
		// looked up under a "development:" key that is not there, and the
		// URL silently stays the default.
		reader.FlatShared = true
		if err := reader.Read(credentialsEnv, &cfg); err != nil {
			return "", fmt.Errorf("config: %w", err)
		}
	default:
		if err := credentials.ApplyEnvOverrides(&cfg); err != nil {
			return "", fmt.Errorf("config: %w", err)
		}
	}

	if cfg.Database.URL == "" {
		return "", fmt.Errorf("config: DATABASE_URL is required in %s", env)
	}
	if _, _, _, err := databaseTarget(cfg.Database.URL); err != nil {
		return "", err
	}
	return cfg.Database.URL, nil
}

// defaultDatabaseURL is the compose stack's database for the environment, and
// nothing for production, where the URL carries a password and must be given.
// The same string config/config.go renders.
func defaultDatabaseURL(name, env string) string {
	switch env {
	case "development", "test":
		return "postgres://postgres:postgres@localhost:5440/" + name + "_" + env + "?sslmode=disable"
	default:
		return ""
	}
}

// useMasterKey hands <NAME>_MASTER_KEY to the credentials reader for the
// duration of one read, the way config.Load does, and puts the environment
// back afterwards. A key already in CREDENTIALS_KEY is left alone: that is
// the variable the reader looks at, and the app only fills it when it is empty.
func useMasterKey(prefix string) (func(), error) {
	key := os.Getenv(prefix + "_MASTER_KEY")
	if key == "" || os.Getenv(credentials.EnvKeyVar) != "" {
		return func() {}, nil
	}
	prev, had := os.LookupEnv(credentials.EnvKeyVar)
	if err := os.Setenv(credentials.EnvKeyVar, key); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return func() {
		if had {
			_ = os.Setenv(credentials.EnvKeyVar, prev)
			return
		}
		_ = os.Unsetenv(credentials.EnvKeyVar)
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fileExists(path string) bool {
	_, err := os.Stat(filepath.Clean(path))
	return err == nil
}

// databaseTarget is the host, port and database name a URL points at. An
// absent port is Postgres's own default, 5432, and an absent host is
// localhost, which is where a URL with no host connects.
func databaseTarget(databaseURL string) (host, port, name string, err error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", "", "", fmt.Errorf("config: DATABASE_URL: %w", err)
	}
	host = u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port = u.Port()
	if port == "" {
		port = "5432"
	}
	name = strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "", "", "", fmt.Errorf("config: DATABASE_URL names no database")
	}
	return host, port, name, nil
}

// inspectDatabase connects and reads. The session is read-only before any
// query runs, so a mistake here cannot migrate, create or drop.
func inspectDatabase(ctx context.Context, databaseURL, migrateDir string) (dbReport, error) {
	host, port, name, err := databaseTarget(databaseURL)
	if err != nil {
		return dbReport{}, err
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return dbReport{}, err
	}
	// A startup parameter, so the session is read-only before the first query.
	// ParseConfig always allocates the map; the guard keeps a hand-built
	// config from panicking.
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return dbReport{}, err
	}
	defer func() { _ = conn.Close(ctx) }()

	var pgVersion string
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version')").Scan(&pgVersion); err != nil {
		return dbReport{}, err
	}
	migration, applied, err := gooseState(ctx, conn)
	if err != nil {
		return dbReport{}, err
	}
	pending, err := pendingMigrations(migrateDir, applied)
	if err != nil {
		return dbReport{}, err
	}
	var tables int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r'
		  AND n.nspname <> 'pg_catalog'
		  AND n.nspname <> 'information_schema'
		  AND n.nspname NOT LIKE 'pg_toast%'
		  AND n.nspname NOT LIKE 'pg_temp_%'`).Scan(&tables); err != nil {
		return dbReport{}, err
	}
	return dbReport{
		Host:      host,
		Port:      port,
		Database:  name,
		Postgres:  pgVersion,
		Migration: migration,
		Pending:   pending,
		Tables:    tables,
	}, nil
}

// gooseState is the latest applied goose version and the set of versions
// whose newest row is applied. goose keeps a row per attempt and deletes it
// on the way down; a historic database can also have a later row with
// is_applied false, and that later row is the one that counts. No table means
// nothing has been applied: version 0, and an empty set. The table is not
// created. This command does not migrate.
func gooseState(ctx context.Context, conn *pgx.Conn) (int64, map[int64]bool, error) {
	var rel *string
	if err := conn.QueryRow(ctx, "SELECT to_regclass('goose_db_version')::text").Scan(&rel); err != nil {
		return 0, nil, err
	}
	applied := map[int64]bool{}
	if rel == nil {
		return 0, applied, nil
	}
	rows, err := conn.Query(ctx, "SELECT version_id, is_applied FROM goose_db_version ORDER BY id DESC")
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	seen := map[int64]bool{}
	var latest int64
	for rows.Next() {
		var version int64
		var isApplied bool
		if err := rows.Scan(&version, &isApplied); err != nil {
			return 0, nil, err
		}
		if seen[version] {
			continue
		}
		seen[version] = true
		if !isApplied {
			continue
		}
		applied[version] = true
		if version > latest {
			latest = version
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	return latest, applied, nil
}

// pendingMigrations counts goose migration files under dir whose version is
// not applied. Files goose would not treat as migrations (no numeric prefix,
// not .sql or .go) are ignored, the same way a .keep in db/migrate is.
func pendingMigrations(dir string, applied map[int64]bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	pending := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		version, ok := migrationVersion(e.Name())
		if !ok {
			continue
		}
		if !applied[version] {
			pending++
		}
	}
	return pending, nil
}

// migrationVersion parses the goose version from a file name: the integer
// before the first underscore, greater than zero, on a .sql or .go file.
func migrationVersion(name string) (int64, bool) {
	ext := filepath.Ext(name)
	if ext != ".sql" && ext != ".go" {
		return 0, false
	}
	before, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(before, 10, 64)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// withHint appends the one-line hint for a connection failure and leaves any
// other error as the driver reported it.
func withHint(err error) error {
	hint := connectHint(err)
	if hint == "" {
		return err
	}
	return fmt.Errorf("%w\n%s", err, hint)
}

func connectHint(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case refused(err):
		return hintNotRunning
	case errors.As(err, &pgErr) && (pgErr.Code == "28P01" || pgErr.Code == "28000"):
		return hintCredentials
	case errors.As(err, &pgErr) && pgErr.Code == "3D000":
		return hintPrepare
	default:
		return ""
	}
}

// refused reports a connection the kernel turned away. pgx wraps the errno,
// and some paths only keep the text, so both are checked.
func refused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(err.Error(), "connection refused")
}
