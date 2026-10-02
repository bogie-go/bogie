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
