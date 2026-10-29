package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSyncFile(t *testing.T) {
	base := "one\ntwo\nthree\n"
	theirs := "one\ntwo\nthree\nfour\n"
	cases := []struct {
		name, mine, wantOp, wantFile string
	}{
		{"an untouched copy is replaced", base, "update", theirs},
		{"a hand edit elsewhere is kept", "ONE\ntwo\nthree\n", "merge", "ONE\ntwo\nthree\nfour\n"},
		{"a hand edit on the same lines conflicts", "one\ntwo\nthree\nFOUR\n", "conflict", ""},
		{"a copy that already has the change is left as it is", theirs, "", theirs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "f.txt", c.mine)
			op, err := syncFile(root, "f.txt", base, theirs)
			if err != nil {
				t.Fatal(err)
			}
			if op != c.wantOp {
				t.Errorf("op = %q, want %q", op, c.wantOp)
			}
			got := readFile(t, root, "f.txt")
			if c.wantOp == "conflict" {
				if !strings.Contains(got, "<<<<<<< yours") || !strings.Contains(got, ">>>>>>> bogie model:sync") {
					t.Errorf("no conflict markers:\n%s", got)
				}
				return
			}
			if got != c.wantFile {
				t.Errorf("file = %q, want %q", got, c.wantFile)
			}
		})
	}

	t.Run("a file that is not there is left", func(t *testing.T) {
		op, err := syncFile(t.TempDir(), "gone.txt", base, theirs)
		if err != nil || op != "left" {
			t.Errorf("op = %q, err = %v", op, err)
		}
	})
}

func TestSyncModelsSelectsWhatBogieWrote(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"user", "cart"} {
		writeFile(t, root, "app/models/"+name+".go", "package models\n")
		writeFile(t, root, "app/models/"+name+"_test.go", "package models_test\n")
	}
	writeFile(t, root, "db/queries/users.sql", "")
	writeFile(t, root, "db/adapter/users.go", "// "+managedHeader+"\npackage adapter\n")
	// A model written by hand has a store, but not bogie's header.
	writeFile(t, root, "db/queries/carts.sql", "")
	writeFile(t, root, "db/adapter/carts.go", "package adapter\n")

	got, err := syncModels(root, "")
	if err != nil || strings.Join(got, ",") != "user" {
		t.Errorf("every model = %v, %v", got, err)
	}
	for _, target := range []string{"users", "user"} {
		if got, err := syncModels(root, target); err != nil || strings.Join(got, ",") != "user" {
			t.Errorf("%s = %v, %v", target, got, err)
		}
	}
	if _, err := syncModels(root, "carts"); err == nil || !strings.Contains(err.Error(), "yours") {
		t.Errorf("a model written by hand: %v", err)
	}
	if _, err := syncModels(root, "nope"); err == nil {
		t.Error("a model that is not there was accepted")
	}
}

func TestFindUsesNarrowsByTheModelsType(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app/a.go", "package app\n\nfunc f(u models.User) string { return u.Name }\n")
	writeFile(t, root, "app/b.go", "package app\n\nfunc g(p models.Product) string { return p.Name }\n")
	writeFile(t, root, "db/sqlc/c.go", "package sqlc\n\nvar _ = User{Name: \"x\"}\n")
	got := findUses(root, "User", "Name")
	if len(got) != 1 || got[0] != "app/a.go:3" {
		t.Errorf("uses = %v", got)
	}
}
