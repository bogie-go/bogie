package commands

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bogie-go/bogie/internal/generate"
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

// An app from before v0.3.0 pins credentials v1.5.0, which lacks the function
// the new config.go calls; tidy keeps the old pin, so app:update must raise it.
func TestStaleBogieModulesRaisesOnlyAnOlderCredentials(t *testing.T) {
	root := t.TempDir()
	write := func(src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	template := []byte("module blog\n\nrequire (\n\tgithub.com/bogie-go/credentials v1.6.0\n\tgithub.com/gin-gonic/gin v1.12.0\n)\n")

	write("module blog\n\nrequire (\n\tgithub.com/bogie-go/credentials v1.5.0\n\tgithub.com/gin-gonic/gin v1.9.0\n)\n")
	got := staleBogieModules(template, root)
	if len(got) != 1 || got[0] != "github.com/bogie-go/credentials@v1.6.0" {
		t.Errorf("staleBogieModules = %v, want just credentials@v1.6.0 (gin stays the app's own)", got)
	}

	write("module blog\n\nrequire github.com/bogie-go/credentials v1.10.0\n")
	if got := staleBogieModules(template, root); len(got) != 0 {
		t.Errorf("a newer pin (v1.10.0 > v1.6.0) must be left alone, got %v", got)
	}

	write("module blog\n\nrequire github.com/bogie-go/credentials v1.6.0\n")
	if got := staleBogieModules(template, root); len(got) != 0 {
		t.Errorf("an equal pin must be left alone, got %v", got)
	}

	write("module blog\n")
	if got := staleBogieModules(template, root); len(got) != 0 {
		t.Errorf("no requirement is left to go mod tidy, got %v", got)
	}
}

// A file the templates stopped generating is reported only while it is still
// on disk; one the user already deleted is nothing to warn about.
func TestMergeRetiredNeedsTheFileOnDisk(t *testing.T) {
	base := map[string][]byte{"old_test.go": []byte("old\n"), "gone.go": []byte("gone\n")}
	ours := map[string][]byte{"old_test.go": []byte("old\n")}
	results, err := merge(ours, base, map[string][]byte{}, "v1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].path != "old_test.go" || results[0].op != opRetired {
		t.Errorf("results = %+v, want only old_test.go retired", results)
	}
}

func TestWarnRetiredNamesEachFileAndTouchesNothing(t *testing.T) {
	base := map[string][]byte{"app/models/store_test.go": []byte("old\n"), "app/x.go": []byte("x\n")}
	ours := map[string][]byte{"app/models/store_test.go": []byte("old\n"), "app/x.go": []byte("x, edited\n")}
	results, err := merge(ours, base, map[string][]byte{}, "v0.2.12", "v0.4.1")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	warnRetired(&out, results, ours, base, "v0.2.12", "v0.4.1")
	got := out.String()
	for _, want := range []string{
		"WARN  app/models/store_test.go: generated by v0.2.12, not by v0.4.1; left in place (unmodified)",
		"WARN  app/x.go: generated by v0.2.12, not by v0.4.1; left in place (edited since generated)",
		"hint  nothing was removed; review the file(s) above, and any *_test.go that depends on them",
		"scaffolded model tests may need regenerating",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	for _, r := range results {
		if r.content != nil {
			t.Errorf("%s: retired files are never written, got content", r.path)
		}
	}
}

func TestCrossesLayoutBreak(t *testing.T) {
	for _, c := range []struct {
		old, current string
		want         bool
	}{
		{"v0.4.1", "v0.5.0", true},
		{"v0.4.1", "v0.5.1", true},
		{"v0.2.12", "v0.5.1", true},
		{"v0.4.2-0.20261020120000-abcdef123456", "v0.5.0", true},
		{"v0.4.1", "v0.5.1+dirty", true},
		{"v0.5.0", "v0.5.1", false},
		{"v0.5.1", "v0.5.1", false},
		{"v0.4.0", "v0.4.1", false},
		{"v0.4.1", "v0.5.0-rc1", false},
		{"v0.4.1", "dev", false},
		{"v0.4.1", "v0.0.0-20261020120000-abcdef123456+dirty", false},
		{"", "v0.5.1", false},
		{"v0.4", "v0.5.1", false},
	} {
		if got := crossesLayoutBreak(c.old, c.current); got != c.want {
			t.Errorf("crossesLayoutBreak(%q, %q) = %v, want %v", c.old, c.current, got, c.want)
		}
	}
}

func TestWarnRetiredIsSilentWithNothingRetired(t *testing.T) {
	var out bytes.Buffer
	warnRetired(&out, []result{{"a.go", opIdentical, nil}}, nil, nil, "v1", "v2")
	if out.Len() != 0 {
		t.Errorf("output = %q, want none", out.String())
	}
}

func TestLayoutBreakNoticeNamesTheMoveAndUUIDHelper(t *testing.T) {
	root := t.TempDir()
	models := filepath.Join(root, "app", "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	bigint := layoutBreakNotice(root, Settings{Version: "v0.4.1", IDScheme: generate.IDBigint}, "v0.5.1")
	for _, want := range []string{"v0.5.0", "app/models/models.go", "v0.4.1", "v0.5.1", layoutBreakGuide, "--pretend"} {
		if !strings.Contains(bigint, want) {
			t.Errorf("notice lacks %q:\n%s", want, bigint)
		}
	}
	if strings.Contains(bigint, "uuid.go") {
		t.Errorf("a bigint app has no uuid.go to retire:\n%s", bigint)
	}

	// A uuid app without the file (never generated a model) has nothing to name;
	// with it, the notice lists it as retired.
	uuid := Settings{Version: "v0.4.1", IDScheme: generate.IDUUID}
	if n := layoutBreakNotice(root, uuid, "v0.5.1"); strings.Contains(n, "uuid.go") {
		t.Errorf("no uuid.go on disk, yet named:\n%s", n)
	}
	if err := os.WriteFile(filepath.Join(models, "uuid.go"), []byte("package models\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := layoutBreakNotice(root, uuid, "v0.5.1"); !strings.Contains(n, "retired  app/models/uuid.go") {
		t.Errorf("uuid.go not listed as retired:\n%s", n)
	}
	if n := layoutBreakNotice(root, Settings{Version: "v0.4.1", IDScheme: generate.IDBigint}, "v0.5.1"); strings.Contains(n, "uuid.go") {
		t.Errorf("a bigint app's notice names uuid.go:\n%s", n)
	}
}
