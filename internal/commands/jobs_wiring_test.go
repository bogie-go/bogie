package commands

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newGitApp makes `bogie new blog` (jobs or not), commits it, and returns its root.
func newGitApp(t *testing.T, jobs bool) string {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	args := []string{"blog", "--skip-tidy", "--module=example.com/blog"}
	if jobs {
		args = []string{"blog", "--jobs", "--module=example.com/blog"}
	}
	var out bytes.Buffer
	if err := New(args, &out); err != nil {
		t.Fatalf("new: %v\n%s", err, out.String())
	}
	app := filepath.Join(root, "blog")
	t.Chdir(app)
	gitIn(t, app, "init", "-q")
	commitAll(t, app, "init")
	return app
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", msg)
}

func assertFullyWired(t *testing.T, app string) {
	t.Helper()
	if !workerCommand.MatchString(readFile(t, app, "main.go")) {
		t.Errorf(`main.go has no "worker": runWorker,`)
	}
	if !strings.Contains(readFile(t, app, "app/application.go"), `"example.com/blog/app/jobs"`) {
		t.Errorf("app/application.go does not import app/jobs")
	}
	if !strings.Contains(readFile(t, app, Marker), "jobs = true") {
		t.Errorf("bogie.toml does not say jobs = true")
	}
}

// A conflict in an unrelated file must not stop the wiring: v1.0.0 returned
// before it, leaving `blog worker` to fall through to serve, and a second
// `g job` skipped enabling because app/jobs already existed.
func TestGenerateJobConflictElsewhereStillWires(t *testing.T) {
	app := newGitApp(t, false)
	if err := os.WriteFile(filepath.Join(app, "README.md"), []byte("my own readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, app, "custom readme")

	var out bytes.Buffer
	err := generateIn(app, []string{"job", "ping"}, &out)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want a conflict error\n%s", err, out.String())
	}
	if !strings.Contains(readFile(t, app, "README.md"), "<<<<<<<") {
		t.Errorf("README.md has no conflict markers")
	}
	assertFullyWired(t, app)
	if !strings.Contains(readFile(t, app, "go.mod"), "riverqueue/river") {
		t.Errorf("go.mod was not tidied: no river")
	}
}

// When the file the wiring edits is itself in conflict, it is named, not
// skipped, and the other steps still run.
func TestWireJobsNamesAConflictedMainGoAndApplicationGo(t *testing.T) {
	for _, file := range []string{"main.go", "app/application.go"} {
		for _, alreadyThere := range []bool{false, true} {
			name := file
			if alreadyThere {
				name += " with the line already present"
			}
			t.Run(name, func(t *testing.T) {
				app := newGitApp(t, true) // a jobs app has both pieces
				if !alreadyThere {
					// Take the piece out, as an unwired app would be.
					unwireJobsPiece(t, app, file)
				}
				before := readFile(t, app, file)
				err := wireJobs(app, io.Discard, map[string]bool{file: true})
				if err == nil || !strings.Contains(err.Error(), file+" has conflicts") {
					t.Fatalf("err = %v, want an error naming %s", err, file)
				}
				if got := readFile(t, app, file); got != before {
					t.Errorf("%s was edited although it conflicted", file)
				}
				if !strings.Contains(readFile(t, app, Marker), "jobs = true") {
					t.Errorf("bogie.toml step did not run")
				}
			})
		}
	}
}

// Conflict markers on disk are caught even if the merge did not say so.
func TestWireJobsRefusesConflictMarkersOnDisk(t *testing.T) {
	app := newGitApp(t, true)
	unwireJobsPiece(t, app, "main.go")
	body := readFile(t, app, "main.go")
	writeFile(t, app, "main.go", "<<<<<<< yours\n"+body+"=======\n"+body+">>>>>>> bogie with jobs\n")
	err := wireJobs(app, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "main.go has conflicts") {
		t.Fatalf("err = %v, want main.go named", err)
	}
}

func unwireJobsPiece(t *testing.T, app, file string) {
	t.Helper()
	body := readFile(t, app, file)
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		if file == "main.go" && strings.Contains(l, `"worker"`) {
			continue
		}
		if file == "app/application.go" && strings.Contains(l, `"example.com/blog/app/jobs"`) {
			continue
		}
		lines = append(lines, l)
	}
	writeFile(t, app, file, strings.Join(lines, "\n"))
}

func halfEnable(t *testing.T, app string) {
	t.Helper()
	unwireJobsPiece(t, app, "main.go")
	unwireJobsPiece(t, app, "app/application.go")
	writeFile(t, app, Marker, strings.Replace(readFile(t, app, Marker), "jobs = true", "jobs = false", 1))
}

func TestDoctorNamesAHalfEnabledJobsApp(t *testing.T) {
	app := newGitApp(t, true)
	var out bytes.Buffer
	if err := Doctor(nil, &out); err != nil {
		t.Fatalf("healthy jobs app: %v\n%s", err, out.String())
	}

	cases := []struct {
		name   string
		break_ func()
		want   string
	}{
		{"worker", func() { unwireJobsPiece(t, app, "main.go") }, `"worker": runWorker, above // bogie:commands`},
		{"import", func() { unwireJobsPiece(t, app, "app/application.go") }, `"example.com/blog/app/jobs"`},
		{"toml", func() {
			writeFile(t, app, Marker, strings.Replace(readFile(t, app, Marker), "jobs = true", "jobs = false", 1))
		}, "set jobs = true in bogie.toml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gitIn(t, app, "stash", "-u", "-q") // no-op when clean; keeps cases independent
			c.break_()
			var out bytes.Buffer
			err := Doctor(nil, &out)
			if err == nil || !strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), c.want) {
				t.Fatalf("err = %v, want a FAIL naming %q\n%s", err, c.want, out.String())
			}
			gitIn(t, app, "checkout", "-q", "--", ".")
		})
	}
}

func TestDoctorJobsAppAfterGenerateThenDestroyJobIsHealthy(t *testing.T) {
	app := newGitApp(t, true)
	if err := generateIn(app, []string{"job", "ping"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := destroyIn(app, []string{"job", "ping"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Doctor(nil, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
}

func TestDoctorIsUnchangedForAnAppWithoutJobs(t *testing.T) {
	newGitApp(t, false)
	var out bytes.Buffer
	if err := Doctor(nil, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "jobs:") {
		t.Errorf("doctor checked jobs wiring in an app with no app/jobs:\n%s", out.String())
	}
}

func TestGenerateJobRepairsAHalfEnabledApp(t *testing.T) {
	app := newGitApp(t, true)
	halfEnable(t, app)
	commitAll(t, app, "half enabled")

	// --pretend names the repair and writes nothing.
	before := treeBytes(t, app)
	var out bytes.Buffer
	if err := generateIn(app, []string{"job", "ping", "--pretend"}, &out); err != nil {
		t.Fatalf("pretend: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "would be repaired") {
		t.Errorf("--pretend did not say it would repair:\n%s", out.String())
	}
	after := treeBytes(t, app)
	for p, b := range before {
		if after[p] != b {
			t.Errorf("--pretend changed %s", p)
		}
	}
	if len(after) != len(before) {
		t.Errorf("--pretend wrote files")
	}

	out.Reset()
	if err := generateIn(app, []string{"job", "ping"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	assertFullyWired(t, app)
	if _, err := os.Stat(filepath.Join(app, "app/jobs/ping.go")); err != nil {
		t.Errorf("the job was not written: %v", err)
	}
	if err := goCmd(app, "build", "./..."); err != nil {
		t.Fatalf("build: %v", err)
	}
	var d bytes.Buffer
	if err := Doctor(nil, &d); err != nil {
		t.Fatalf("doctor after repair: %v\n%s", err, d.String())
	}
}

// A healthy jobs app gets no repair noise on the next job.
func TestGenerateJobOnAHealthyJobsAppDoesNotRepair(t *testing.T) {
	app := newGitApp(t, true)
	var out bytes.Buffer
	if err := generateIn(app, []string{"job", "ping"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "repair") {
		t.Errorf("unexpected repair:\n%s", out.String())
	}
}

// A --jobs app with only jobs = false already has the gofmt-aligned
// `"worker":  runWorker,`. Wiring must recognise it, not add a second key.
func TestGenerateJobDoesNotDuplicateAlignedWorkerEntry(t *testing.T) {
	app := newGitApp(t, true)
	toml := readFile(t, app, Marker)
	if !strings.Contains(toml, "jobs = true") {
		t.Fatalf("fixture has no jobs = true:\n%s", toml)
	}
	writeFile(t, app, Marker, strings.Replace(toml, "jobs = true", "jobs = false", 1))

	var out bytes.Buffer
	if err := generateIn(app, []string{"job", "x"}, &out); err != nil {
		t.Fatalf("g job: %v\n%s", err, out.String())
	}
	if n := len(workerCommand.FindAllString(readFile(t, app, "main.go"), -1)); n != 1 {
		t.Errorf("main.go has %d worker entries, want 1", n)
	}
	if err := goCmd(app, "build", "./..."); err != nil {
		t.Errorf("go build: %v", err)
	}
	if !strings.Contains(readFile(t, app, Marker), "jobs = true") {
		t.Errorf("bogie.toml was not repaired to jobs = true")
	}
	if _, err := os.Stat(filepath.Join(app, "app", "jobs", "x.go")); err != nil {
		t.Errorf("job file not written: %v", err)
	}
}
