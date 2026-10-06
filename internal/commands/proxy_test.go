package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func joined(steps [][]string) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = strings.Join(s, " ")
	}
	return strings.Join(parts, " && ")
}

func TestPlanSpellsCommandsAsRailsDoes(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    string
	}{
		// Rails tasks, colon-spelled
		{"db:prepare", nil, "go run . db prepare"},
		{"db:migrate", nil, "go run . migrate up"},
		{"db:migrate:status", nil, "go run . migrate status"},
		{"db:rollback", nil, "go run . migrate down --yes"},
		{"db:reset", nil, "go run . db drop --yes && go run . db prepare"},
		{"db:migrate:redo", nil, "go run . migrate down --yes && go run . migrate up"},
		{"db:seed", nil, "go run . db seed"},
		{"credentials:edit", []string{"-e", "production"}, "go tool credentials edit -e production"},
		// Rails commands, space-spelled
		{"server", nil, "go run . serve"},
		{"server", []string{"--help"}, "go run . serve --help"},
		{"worker", nil, "go run . worker"},
		{"test", nil, "go test -race -cover ./..."},
		{"test", []string{"./config/..."}, "go test -race -cover ./config/..."},
		{"lint", nil, "make lint"},
		{"ci", nil, "bin/ci"},
		// the app binary's own form, accepted too
		{"db", []string{"create"}, "go run . db create"},
		{"migrate", []string{"down", "--yes"}, "go run . migrate down --yes"},
		{"credentials", []string{"show"}, "go tool credentials show"},
	}
	for _, c := range cases {
		steps, err := plan(c.command, c.args)
		if err != nil {
			t.Errorf("plan(%s): %v", c.command, err)
			continue
		}
		if got := joined(steps); got != c.want {
			t.Errorf("plan(%s %v) = %q, want %q", c.command, c.args, got, c.want)
		}
	}
}

func TestPlanUnknownTaskNamesTheNearest(t *testing.T) {
	_, err := plan("db:migrate:up", nil)
	if err == nil || !strings.Contains(err.Error(), "db:migrate") {
		t.Errorf("err = %v; want a hint naming db:migrate", err)
	}
	if _, err := plan("deploy", nil); err == nil {
		t.Error("plan(deploy) succeeded; the app has no such command")
	}
	// A colon task outside Bogie's own families is the binary's: a
	// generator may have added it.
	steps, err := plan("api_keys:create", []string{"billing"})
	if err != nil || joined(steps) != "go run . api_keys create billing" {
		t.Errorf("api_keys:create = %q, %v", joined(steps), err)
	}
}

// Appending args must not mutate the shared task table.
func TestPlanDoesNotMutateTheTable(t *testing.T) {
	_, _ = plan("credentials:edit", []string{"-e", "staging"})
	steps, _ := plan("credentials:edit", nil)
	if got := joined(steps); got != "go tool credentials edit" {
		t.Errorf("table mutated: %q", got)
	}
}

func TestAppRootWalksUp(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, Marker), []byte("bogie = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "app", "controllers")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := appRoot(deep)
	if err != nil {
		t.Fatalf("appRoot: %v", err)
	}
	if got != root {
		t.Errorf("appRoot(%s) = %s, want %s", deep, got, root)
	}
}

func TestAppRootOutsideAnApp(t *testing.T) {
	if _, err := appRoot(t.TempDir()); err == nil {
		t.Error("appRoot outside an app succeeded")
	}
}

// reloadApp writes the three files reloadSteps reads: the marker that names
// the app, the reload config whose presence turns reload on, and a go.mod
// holding the reload tool.
func reloadApp(t *testing.T, withAir bool) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(Marker, "bogie = \"0.1.0\"\nname = \"blog\"\nmodule = \"blog\"\njobs = true\n")
	write(airConfig, "root = \".\"\n")
	gomod := "module blog\n\ngo 1.25\n"
	if withAir {
		gomod += "\ntool (\n\t" + airModule + "\n)\n"
	}
	write("go.mod", gomod)
	t.Setenv("BLOG_ENV", "")
	return root
}

func TestReloadRunsAirInDevelopment(t *testing.T) {
	root := reloadApp(t, true)

	steps, hint := reloadSteps(root, "server", nil)
	if hint != "" {
		t.Errorf("hint = %q, want none", hint)
	}
	if got, want := joined(steps), "go tool air -c .air.toml"; got != want {
		t.Errorf("server = %q, want %q", got, want)
	}

	// The worker reuses the one config, pointed at its own binary so the two
	// builds cannot overwrite each other.
	steps, _ = reloadSteps(root, "worker", nil)
	got := joined(steps)
	for _, want := range []string{
		"go tool air -c .air.toml",
		"--build.cmd go build -o ./tmp/blog-worker .",
		"--build.full_bin ./tmp/blog-worker worker",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("worker = %q, missing %q", got, want)
		}
	}
}

func TestReloadIsDevelopmentOnly(t *testing.T) {
	root := reloadApp(t, true)
	for _, env := range []string{"production", "test"} {
		t.Setenv("BLOG_ENV", env)
		if steps, _ := reloadSteps(root, "server", nil); steps != nil {
			t.Errorf("BLOG_ENV=%s reloads; want the plain binary", env)
		}
	}
}

func TestReloadStandsAsideForOneOffsAndOtherCommands(t *testing.T) {
	root := reloadApp(t, true)
	// `bogie server --help` is a question, not the development loop.
	if steps, _ := reloadSteps(root, "server", []string{"--help"}); steps != nil {
		t.Error("server --help reloads; want the plain binary")
	}
	for _, command := range []string{"db:migrate", "test", "ci", "console"} {
		if steps, _ := reloadSteps(root, command, nil); steps != nil {
			t.Errorf("%s reloads; only server and worker do", command)
		}
	}
}

func TestReloadWithoutItsConfigOrToolFallsBack(t *testing.T) {
	// No config: reload is off, silently, which is how it is turned off.
	root := reloadApp(t, true)
	if err := os.Remove(filepath.Join(root, airConfig)); err != nil {
		t.Fatal(err)
	}
	steps, hint := reloadSteps(root, "server", nil)
	if steps != nil || hint != "" {
		t.Errorf("steps = %v, hint = %q; want neither", steps, hint)
	}

	// Config but no tool: an app that has not tidied since app:update. It
	// still serves, and says what to run.
	root = reloadApp(t, false)
	steps, hint = reloadSteps(root, "server", nil)
	if steps != nil {
		t.Error("ran air although go.mod does not have it")
	}
	if !strings.Contains(hint, "go get -tool "+airModule) {
		t.Errorf("hint = %q, want the command that adds the tool", hint)
	}
}
