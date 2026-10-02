package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    string
	}{
		{"server", nil, "go run . serve"},
		{"server", []string{"--help"}, "go run . serve --help"},
		{"test", nil, "go test -race -cover ./..."},
		{"test", []string{"./config/..."}, "go test -race -cover ./config/..."},
		{"lint", nil, "make lint"},
		{"ci", nil, "bin/ci"},
		{"db", []string{"create"}, "go run . db create"},
		{"migrate", []string{"down", "--yes"}, "go run . migrate down --yes"},
		{"credentials", []string{"edit", "-e", "production"}, "go tool credentials edit -e production"},
	}
	for _, c := range cases {
		argv, err := plan(c.command, c.args)
		if err != nil {
			t.Errorf("plan(%s): %v", c.command, err)
			continue
		}
		if got := strings.Join(argv, " "); got != c.want {
			t.Errorf("plan(%s %v) = %q, want %q", c.command, c.args, got, c.want)
		}
	}
	if _, err := plan("deploy", nil); err == nil {
		t.Error("plan(deploy) succeeded; the app has no such command")
	}
}

func TestAppRootWalksUp(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, Marker), []byte("layout = \"0.1.0\"\n"), 0o644); err != nil {
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
