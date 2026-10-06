package commands

import (
	"io"
	"os"
	"os/exec"
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

func TestRewriteJobs(t *testing.T) {
	root := t.TempDir()
	toml := "bogie = \"v0.1.0\"\nname = \"blog\"\nmodule = \"example.com/blog\"\njobs = false\n"
	if err := os.WriteFile(filepath.Join(root, Marker), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rewriteJobs(root, true); err != nil {
		t.Fatal(err)
	}
	s, err := readSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Jobs {
		t.Errorf("settings after rewriteJobs(true) = %+v, want Jobs true", s)
	}
	b, _ := os.ReadFile(filepath.Join(root, Marker))
	if !strings.Contains(string(b), "jobs = true") {
		t.Errorf("rewritten marker:\n%s", b)
	}
}

// enableJobs refuses an app that has not been brought to this bogie's
// version yet: base would be rendered from the wrong templates and every
// unrelated template change since would show up as noise in what is meant
// to be a jobs-only diff.
func TestEnableJobsRefusesAStaleVersion(t *testing.T) {
	root := t.TempDir()
	toml := "bogie = \"v0.0.0-stale\"\nname = \"blog\"\nmodule = \"example.com/blog\"\njobs = false\n"
	if err := os.WriteFile(filepath.Join(root, Marker), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	err := enableJobs(root, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "app:update") {
		t.Errorf("enableJobs on a stale version: err = %v, want a mention of app:update", err)
	}
}

// enableJobs touches several files in one diff, so it refuses on an
// uncommitted change exactly as app:update does, and for the same reason.
func TestEnableJobsRefusesADirtyTree(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := New([]string{"blog", "--skip-tidy"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("blog")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "commit", "-q", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = enableJobs(root, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("enableJobs on a dirty tree: err = %v, want a mention of uncommitted changes", err)
	}
}

func TestToolDirectivesReadsBothForms(t *testing.T) {
	block := []byte(`module blog

go 1.26.0

require github.com/gin-gonic/gin v1.12.0

// A comment that is not a tool.
tool (
	github.com/air-verse/air
	github.com/pressly/goose/v3/cmd/goose
)
`)
	got := toolDirectives(block)
	want := []string{"github.com/air-verse/air", "github.com/pressly/goose/v3/cmd/goose"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("toolDirectives(block) = %v, want %v", got, want)
	}

	if got := toolDirectives([]byte("module blog\n\ntool github.com/air-verse/air\n")); len(got) != 1 || got[0] != "github.com/air-verse/air" {
		t.Errorf("toolDirectives(single) = %v", got)
	}
	if got := toolDirectives([]byte("module blog\n")); len(got) != 0 {
		t.Errorf("toolDirectives(none) = %v, want empty", got)
	}
}

// The case a developer actually hits: app:update brings .air.toml to an app
// generated before reload existed, and go mod tidy cannot add the tool
// because nothing imports it.
func TestMissingToolsNamesWhatTidyWouldNotAdd(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module blog\n\ngo 1.26.0\n\ntool (\n\tgithub.com/pressly/goose/v3/cmd/goose\n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	template := []byte("module blog\n\ntool (\n\tgithub.com/air-verse/air\n\tgithub.com/pressly/goose/v3/cmd/goose\n)\n")

	got := missingTools(template, root)
	if len(got) != 1 || got[0] != "github.com/air-verse/air" {
		t.Errorf("missingTools = %v, want just the reload tool", got)
	}

	// Once it is there, nothing is missing.
	if err := os.WriteFile(filepath.Join(root, "go.mod"), template, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := missingTools(template, root); len(got) != 0 {
		t.Errorf("missingTools = %v, want none", got)
	}
}
