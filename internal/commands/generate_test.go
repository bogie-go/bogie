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
	if got := existingMigration(root, "app/domain/comment.go"); got != "" {
		t.Errorf("a non-migration matched: %q", got)
	}
}

// The development credentials file is edited as text: a key lands at the end
// of its own group's block, a group that is missing is appended, and a key
// already there, commented or not, is left alone.
func TestInsertGroupKeys(t *testing.T) {
	const plain = "# header\nserver:\n  addr: \":8080\"\n\ndatabase:\n  url: postgres://x\n"

	got, added := insertGroupKeys(plain, "smtp", []devKey{{"address", `address: "localhost:1025"`}, {"password", "# password: \"\""}})
	want := plain + "smtp:\n  address: \"localhost:1025\"\n  # password: \"\"\n"
	if got != want || len(added) != 2 {
		t.Errorf("new group:\n%q\nwant\n%q (added %v)", got, want, added)
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

	for _, bad := range [][]string{{"config", "server", "x"}, {"config", "smtp"}, {"config", "smtp", "port:integer"}} {
		if err := generateIn(root, bad, &out); err == nil {
			t.Errorf("g %v accepted", bad)
		}
	}
}
