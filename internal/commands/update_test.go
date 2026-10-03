package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeDecidesEachFile(t *testing.T) {
	base := map[string][]byte{
		"untouched.go": []byte("a\nb\nc\n"),
		"edited.go":    []byte("a\nb\nc\n"),
		"both.go":      []byte("a\nb\nc\n"),
		"clash.go":     []byte("a\nb\nc\n"),
		"deleted.go":   []byte("gone\n"),
		"retired.go":   []byte("old\n"),
		"same.go":      []byte("same\n"),
		"go.mod":       []byte("module x\n"),
	}
	theirs := map[string][]byte{
		"untouched.go": []byte("a\nB\nc\n"),
		"edited.go":    []byte("a\nb\nc\n"),
		"both.go":      []byte("a\nB\nc\n"),
		"clash.go":     []byte("a\nB\nc\n"),
		"deleted.go":   []byte("gone\n"),
		"same.go":      []byte("same\n"),
		"new.go":       []byte("new\n"),
		"go.mod":       []byte("module x\n\ngo 1.99\n"),
	}
	ours := map[string][]byte{
		"untouched.go": []byte("a\nb\nc\n"),
		"edited.go":    []byte("a\nb\nc\nmine\n"),
		"both.go":      []byte("a\nb\nc\nmine\n"),
		"clash.go":     []byte("a\nMINE\nc\n"),
		"retired.go":   []byte("old, edited\n"),
		"same.go":      []byte("same\n"),
		"go.mod":       []byte("module x\n\ngo 1.26.0\n"),
	}
	results, err := merge(ours, base, theirs, "v1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]result{}
	for _, r := range results {
		got[r.path] = r
	}
	want := map[string]op{
		"untouched.go": opUpdated, "edited.go": opKept, "both.go": opMerged, "clash.go": opConflict,
		"deleted.go": opKept, "retired.go": opRetired, "same.go": opIdentical, "new.go": opCreate,
	}
	for p, o := range want {
		if got[p].op != o {
			t.Errorf("%s: %s, want %s", p, got[p].op, o)
		}
	}
	if _, ok := got["go.mod"]; ok {
		t.Error("go.mod is tidied, never merged")
	}
	if string(got["both.go"].content) != "a\nB\nc\nmine\n" {
		t.Errorf("both.go merged to %q", got["both.go"].content)
	}
	if c := string(got["clash.go"].content); !strings.Contains(c, "<<<<<<< yours") || !strings.Contains(c, ">>>>>>> bogie v2") {
		t.Errorf("clash.go lacks labelled conflict markers:\n%s", c)
	}
	if got["untouched.go"].content == nil || got["edited.go"].content != nil || got["retired.go"].content != nil {
		t.Error("content written for the wrong files")
	}
}

// Without a base there is nothing to merge: a two-way report, nothing to write.
func TestMergeWithoutBaseIsAReport(t *testing.T) {
	theirs := map[string][]byte{"a.go": []byte("x\n"), "b.go": []byte("y\n")}
	ours := map[string][]byte{"a.go": []byte("x\n"), "b.go": []byte("z\n")}
	results, err := merge(ours, nil, theirs, "v1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		switch r.path {
		case "a.go":
			if r.op != opIdentical {
				t.Errorf("a.go: %s", r.op)
			}
		case "b.go":
			if r.op != opConflict || r.content != nil {
				t.Errorf("b.go: %s with content %q", r.op, r.content)
			}
		}
	}
}

func TestReadSettingsAndRewriteVersion(t *testing.T) {
	root := t.TempDir()
	toml := "# written by new\nbogie = \"v0.1.0\"\nname = \"blog\"\nmodule = \"example.com/blog\"\njobs = true\n"
	if err := os.WriteFile(filepath.Join(root, Marker), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := readSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "v0.1.0" || s.Name != "blog" || s.Module != "example.com/blog" || !s.Jobs {
		t.Errorf("settings = %+v", s)
	}
	if err := rewriteVersion(root, "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, Marker))
	if !strings.Contains(string(b), "bogie = \"v0.2.0\"") || !strings.Contains(string(b), "# written by new") {
		t.Errorf("rewritten marker:\n%s", b)
	}
}
