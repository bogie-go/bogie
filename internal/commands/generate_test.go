package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNextStampSkipsTakenVersions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "db", "migrate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)
	for _, name := range []string{"20261002093000_a.sql", "20261002093001_b.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := nextStamp(root, now)
	if got.Format("20060102150405") != "20261002093002" {
		t.Errorf("nextStamp = %s, want 20261002093002", got.Format("20060102150405"))
	}
}

func TestExistingMigrationIgnoresTheStamp(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "db", "migrate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20261002093000_create_comments.sql"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := existingMigration(root, "db/migrate/20261002101010_create_comments.sql"); got != "db/migrate/20261002093000_create_comments.sql" {
		t.Errorf("existingMigration = %q", got)
	}
	if got := existingMigration(root, "db/migrate/20261002101010_create_posts.sql"); got != "" {
		t.Errorf("a different migration matched: %q", got)
	}
	if got := existingMigration(root, "app/models/comment.go"); got != "" {
		t.Errorf("a non-migration matched: %q", got)
	}
}

// The development credentials file is edited as text: a key lands at the end
// of its own group's block, a group that is missing is appended, and a key
// already there, commented or not, is left alone.
func TestInsertGroupKeys(t *testing.T) {
	const plain = "# header\nserver:\n  addr: \":8080\"\n\ndatabase:\n  url: postgres://x\n"

	// A new group goes where its name sorts among the top-level entries,
	// its own keys in the order given.
	got, added := insertGroupKeys(plain, "smtp", []devKey{{"password", "# password: \"\""}, {"address", `address: "localhost:1025"`}})
	want := plain + "smtp:\n  # password: \"\"\n  address: \"localhost:1025\"\n"
	if got != want || len(added) != 2 {
		t.Errorf("new group at the end:\n%q\nwant\n%q (added %v)", got, want, added)
	}
	// The file's opening comment stays at the top: a group sorting first goes
	// below it, not above.
	got, _ = insertGroupKeys(plain, "cache", []devKey{{"ttl", `ttl: "5m"`}})
	if !strings.HasPrefix(got, "# header\ncache:\n  ttl: \"5m\"\nserver:") {
		t.Errorf("cache should go before the first entry that sorts after it, below the header:\n%s", got)
	}

	// Flat keys are top-level lines, placed alphabetically, and one already
	// there (as a value or a commented placeholder) is left alone.
	sorted := "# h\ndatabase:\n  url: x\nsmtp:\n  address: y\n"
	got, added = insertGroupKeys(sorted, "", []devKey{{"hello", `hello: "world"`}, {"retries", "# retries: 0"}, {"aaa", `aaa: "1"`}})
	want = "# h\naaa: \"1\"\ndatabase:\n  url: x\nhello: \"world\"\n# retries: 0\nsmtp:\n  address: y\n"
	if got != want || len(added) != 3 {
		t.Errorf("flat keys:\n%q\nwant\n%q (added %v)", got, want, added)
	}
	if _, added := insertGroupKeys(got, "", []devKey{{"hello", `hello: "again"`}, {"retries", "# retries: 0"}}); len(added) != 0 {
		t.Errorf("flat keys already there were added again: %v", added)
	}

	got, added = insertGroupKeys(plain, "server", []devKey{{"log_level", "log_level: debug"}})
	want = "# header\nserver:\n  addr: \":8080\"\n  log_level: debug\n\ndatabase:\n  url: postgres://x\n"
	if got != want || len(added) != 1 {
		t.Errorf("key into an existing group:\n%q\nwant\n%q", got, want)
	}

	// Present already, as a value or as a comment: nothing changes.
	got, added = insertGroupKeys(plain+"smtp:\n  # address: \"\"\n", "smtp", []devKey{{"address", `address: "x"`}})
	if len(added) != 0 || got != plain+"smtp:\n  # address: \"\"\n" {
		t.Errorf("an existing key was added again: %q (added %v)", got, added)
	}
}

// g config writes a group once, registers it, and a second run adds only the
// keys that are new; d config takes the file and the registration away.
func TestConfigGeneratorAndDestroy(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write("go.mod", "module example.com/blog\n")
	write("config/config.go", `package config

type Config struct {
	Env string
	// bogie:config
}

func defaults() Config {
	c := Config{}
	// bogie:defaults
	return c
}

func (c Config) validate() error {
	for _, err := range []error{
		// bogie:validate
	} {
		if err != nil {
			return err
		}
	}
	return nil
}
`)
	var out strings.Builder
	if err := generateIn(root, []string{"config", "smtp", "address:req", "port:int=1025"}, &out); err != nil {
		t.Fatalf("g config: %v\n%s", err, out.String())
	}
	cfg := read("config/config.go")
	for _, line := range []string{"Smtp SmtpConfig", "c.Smtp.defaults(env)", "c.Smtp.validate(c.Env),"} {
		if !strings.Contains(strings.Join(strings.Fields(cfg), " "), line) {
			t.Errorf("config.go lacks %q:\n%s", line, cfg)
		}
	}
	first := read("config/smtp.go")
	if !strings.Contains(first, `Address string `+"`mapstructure:\"address\"`") && !strings.Contains(first, `mapstructure:"address"`) {
		t.Errorf("config/smtp.go lacks the address key:\n%s", first)
	}

	// Hand-edit defaults, then run again with one old key and one new: the
	// edit survives, the old key is not added twice, the new one is.
	write("config/smtp.go", strings.Replace(first, "func (c *SmtpConfig) defaults(env string) {}", "func (c *SmtpConfig) defaults(env string) { c.Port = 1 }", 1))
	out.Reset()
	if err := generateIn(root, []string{"config", "smtp", "address", "username"}, &out); err != nil {
		t.Fatalf("second g config: %v\n%s", err, out.String())
	}
	second := read("config/smtp.go")
	if !strings.Contains(second, "c.Port = 1") {
		t.Error("a hand-written defaults was overwritten")
	}
	if strings.Count(second, `mapstructure:"address"`) != 1 || strings.Count(second, `mapstructure:"username"`) != 1 {
		t.Errorf("second run should add username once and address not again:\n%s", second)
	}
	if strings.Count(read("config/config.go"), "SmtpConfig") != 1 {
		t.Errorf("the group was registered twice:\n%s", read("config/config.go"))
	}

	out.Reset()
	if err := destroyIn(root, []string{"config", "smtp"}, &out); err != nil {
		t.Fatalf("d config: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "config", "smtp.go")); err == nil {
		t.Error("d config left config/smtp.go")
	}
	if strings.Contains(read("config/config.go"), "Smtp") {
		t.Errorf("d config left its registration:\n%s", read("config/config.go"))
	}

	// Flat keys: fields on Config itself, sorted in among the groups.
	out.Reset()
	if err := generateIn(root, []string{"config", "support_email=help@example.com", "retries:int:req"}, &out); err != nil {
		t.Fatalf("flat g config: %v\n%s", err, out.String())
	}
	if err := generateIn(root, []string{"config", "alpha", "x"}, &out); err != nil {
		t.Fatalf("g config alpha: %v", err)
	}
	cfg = read("config/config.go")
	flat := strings.Join(strings.Fields(cfg), " ")
	for _, line := range []string{"SupportEmail string", "Retries int", `requireKey(c.Env, "RETRIES", c.Retries != 0),`} {
		if !strings.Contains(flat, line) {
			t.Errorf("config.go lacks %q after a flat key:\n%s", line, cfg)
		}
	}
	order := []string{`mapstructure:"alpha"`, `mapstructure:"retries"`, `mapstructure:"support_email"`}
	for i := 1; i < len(order); i++ {
		if strings.Index(cfg, order[i-1]) > strings.Index(cfg, order[i]) {
			t.Errorf("top-level fields are not alphabetical (%s before %s):\n%s", order[i], order[i-1], cfg)
		}
	}
	out.Reset()
	if err := destroyIn(root, []string{"config", "support_email"}, &out); err != nil {
		t.Fatalf("d config flat: %v\n%s", err, out.String())
	}
	if strings.Contains(read("config/config.go"), "SupportEmail") || !strings.Contains(read("config/config.go"), "Retries") {
		t.Errorf("d config support_email should remove only that key:\n%s", read("config/config.go"))
	}

	for _, bad := range [][]string{{"config", "server", "x"}, {"config", "smtp"}, {"config", "smtp", "port:float"}, {"config", "env=x"}, {"config", "lonely"}} {
		if err := generateIn(root, bad, &out); err == nil {
			t.Errorf("g %v accepted", bad)
		}
	}
}

// Keys can be added to the app's own groups, whose structs sit in config.go
// with a marker inside each; the group itself is neither created nor removed.
func TestConfigKeysIntoACoreGroup(t *testing.T) {
	root := t.TempDir()
	cfg := `package config

type Config struct {
	Env string
	Database DatabaseConfig ` + "`mapstructure:\"database\"`" + `
	// bogie:config
}

type DatabaseConfig struct {
	URL string ` + "`mapstructure:\"url\"`" + `
	// bogie:config:database
}

func defaults() Config {
	c := Config{}
	// bogie:defaults
	return c
}

func (c Config) validate() error {
	for _, err := range []error{
		// bogie:validate
	} {
		if err != nil {
			return err
		}
	}
	return nil
}
`
	for rel, content := range map[string]string{"go.mod": "module example.com/blog\n", "config/config.go": cfg} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	if err := generateIn(root, []string{"config", "database", "username:req", "password=yyyy", "url"}, &out); err != nil {
		t.Fatalf("g config database: %v\n%s", err, out.String())
	}
	b, _ := os.ReadFile(filepath.Join(root, "config", "config.go"))
	got := string(b)
	flat := strings.Join(strings.Fields(got), " ")
	for _, want := range []string{`Username string`, `Password string`, `requireKey(c.Env, "DATABASE_USERNAME", c.Database.Username != ""),`} {
		if !strings.Contains(flat, want) {
			t.Errorf("config.go lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, `mapstructure:"url"`) != 1 {
		t.Errorf("url was added again though DatabaseConfig has it:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "database.go")); err == nil {
		t.Error("a file was created for a core group")
	}
	if err := destroyIn(root, []string{"config", "database"}, &out); err == nil {
		t.Error("d config database accepted")
	}
}
