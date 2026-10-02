package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

var src = fstest.MapFS{
	"app/main.go.tmpl":           {Data: []byte("package main // {{.Name}} at {{.Module}}\n")},
	"app/dot_gitignore.tmpl":     {Data: []byte("/bin/\n")},
	"app/config/env.txt":         {Data: []byte("{{.Name}} is copied verbatim, not rendered\n")},
	"app/lib/dot_keep/x.go.tmpl": {Data: []byte("package keep\n")},
}

var vars = Vars{Name: "blog", Module: "example.com/blog", EnvPrefix: "BLOG", LayoutVersion: "0.1.0"}

func ops(actions []Action) map[string]Op {
	m := map[string]Op{}
	for _, a := range actions {
		m[a.Path] = a.Op
	}
	return m
}

func TestRenderCreatesEveryFile(t *testing.T) {
	dest := t.TempDir()
	actions, err := Render(src, "app", dest, vars, Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := ops(actions)
	for _, p := range []string{"main.go", ".gitignore", "config/env.txt", "lib/.keep/x.go"} {
		if got[p] != OpCreate {
			t.Errorf("%s: op = %q, want create", p, got[p])
		}
	}

	b, _ := os.ReadFile(filepath.Join(dest, "main.go"))
	if string(b) != "package main // blog at example.com/blog\n" {
		t.Errorf("main.go not rendered: %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(dest, "config/env.txt"))
	if string(b) != "{{.Name}} is copied verbatim, not rendered\n" {
		t.Errorf("non-.tmpl file was rendered: %q", b)
	}
}

func TestRenderAgainIsIdentical(t *testing.T) {
	dest := t.TempDir()
	if _, err := Render(src, "app", dest, vars, Options{}); err != nil {
		t.Fatal(err)
	}
	actions, err := Render(src, "app", dest, vars, Options{})
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	for _, a := range actions {
		if a.Op != OpIdentical {
			t.Errorf("%s: op = %q, want identical", a.Path, a.Op)
		}
	}
}

func TestRenderReportsConflictAndWritesNothing(t *testing.T) {
	dest := t.TempDir()
	if _, err := Render(src, "app", dest, vars, Options{}); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(dest, "main.go")
	if err := os.WriteFile(edited, []byte("package main // edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	actions, err := Render(src, "app", dest, vars, Options{})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	got := ops(actions)
	if got["main.go"] != OpConflict {
		t.Errorf("main.go: op = %q, want conflict", got["main.go"])
	}
	if got[".gitignore"] != OpIdentical {
		t.Errorf("render stopped at the conflict; .gitignore op = %q", got[".gitignore"])
	}
	b, _ := os.ReadFile(edited)
	if string(b) != "package main // edited by hand\n" {
		t.Errorf("conflicting file was overwritten: %q", b)
	}
}

func TestRenderForceOverwrites(t *testing.T) {
	dest := t.TempDir()
	if _, err := Render(src, "app", dest, vars, Options{}); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(dest, "main.go")
	if err := os.WriteFile(edited, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	actions, err := Render(src, "app", dest, vars, Options{Force: true})
	if err != nil {
		t.Fatalf("Render --force: %v", err)
	}
	if got := ops(actions)["main.go"]; got != OpForce {
		t.Errorf("main.go: op = %q, want force", got)
	}
	b, _ := os.ReadFile(edited)
	if string(b) != "package main // blog at example.com/blog\n" {
		t.Errorf("not overwritten: %q", b)
	}
}

func TestRenderPretendWritesNothing(t *testing.T) {
	dest := t.TempDir()
	var reported []Action
	actions, err := Render(src, "app", dest, vars, Options{Pretend: true, Report: func(a Action) { reported = append(reported, a) }})
	if err != nil {
		t.Fatalf("Render --pretend: %v", err)
	}
	if len(actions) != 4 || len(reported) != 4 {
		t.Errorf("reported %d actions, want 4", len(reported))
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Errorf("pretend wrote %d entries", len(entries))
	}
}

func TestOutputPath(t *testing.T) {
	cases := map[string]string{
		"main.go.tmpl":           "main.go",
		"dot_gitignore.tmpl":     "/.gitignore"[1:],
		"dot_github/ci.yml":      ".github/ci.yml",
		"app/models/store.go":    "app/models/store.go",
		"config/dot_env.example": "config/.env.example",
	}
	for in, want := range cases {
		if got := OutputPath(in); got != want {
			t.Errorf("OutputPath(%q) = %q, want %q", in, got, want)
		}
	}
}
