package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// jobsGo is the registration file a current --jobs app has: a periodic slice
// for --every to append to, and the marker the generator inserts above.
const jobsGo = "package jobs\n" +
	"\n" +
	"import (\n" +
	"\t\"log/slog\"\n" +
	"\n" +
	"\t\"github.com/riverqueue/river\"\n" +
	")\n" +
	"\n" +
	"func Workers(log *slog.Logger) (*river.Workers, []*river.PeriodicJob, bool) {\n" +
	"\tworkers := river.NewWorkers()\n" +
	"\tvar periodic []*river.PeriodicJob\n" +
	"\tregistered := 0\n" +
	"\t// bogie:jobs\n" +
	"\treturn workers, periodic, registered > 0\n" +
	"}\n" +
	"\n" +
	"func add[T river.JobArgs](workers *river.Workers, worker river.Worker[T]) int {\n" +
	"\triver.AddWorker(workers, worker)\n" +
	"\treturn 1\n" +
	"}\n"

func jobsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/blog\n\ngo 1.25.0\n")
	writeFile(t, root, Marker, "bogie = \"dev\"\nname = \"blog\"\nmodule = \"example.com/blog\"\njobs = true\n")
	writeFile(t, root, "app/jobs/jobs.go", jobsGo)
	return root
}

func treeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A periodic job is registered above the same marker as its worker, then
// destroy takes both lines and both files back out. go get adds the cron
// module the generated file imports.
func TestPeriodicJobGenerateAndDestroy(t *testing.T) {
	root := jobsFixture(t)
	var out bytes.Buffer
	// A plain job stays registered when the periodic one is removed.
	if err := generateIn(root, []string{"job", "send_welcome"}, &out); err != nil {
		t.Fatalf("generate send_welcome: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := generateIn(root, []string{"job", "daily_digest", "--every", "0 9 * * *"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	jobs := readFile(t, root, "app/jobs/jobs.go")
	for _, want := range []string{
		"registered += add(workers, &DailyDigestWorker{Log: log})",
		"periodic = append(periodic, DailyDigestPeriodic())",
		"// bogie:jobs",
	} {
		if !strings.Contains(jobs, want) {
			t.Errorf("jobs.go lacks %q:\n%s", want, jobs)
		}
	}
	src := readFile(t, root, "app/jobs/daily_digest.go")
	for _, want := range []string{
		`cron.ParseStandard("0 9 * * *")`,
		"river.NewPeriodicJob(",
		"RunOnStart: false",
		"func DailyDigestPeriodic() *river.PeriodicJob",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("daily_digest.go lacks %q:\n%s", want, src)
		}
	}
	if !strings.Contains(readFile(t, root, "go.mod"), "github.com/robfig/cron/v3") {
		t.Fatalf("go.mod did not gain robfig/cron:\n%s", readFile(t, root, "go.mod"))
	}
	for _, want := range []string{"create  app/jobs/daily_digest.go", "insert  app/jobs/jobs.go: periodic = append"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := destroyIn(root, []string{"job", "daily_digest"}, &out); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "app/jobs/daily_digest.go")); err == nil {
		t.Error("daily_digest.go survived destroy")
	}
	if _, err := os.Stat(filepath.Join(root, "app/jobs/daily_digest_test.go")); err == nil {
		t.Error("daily_digest_test.go survived destroy")
	}
	jobs = readFile(t, root, "app/jobs/jobs.go")
	if strings.Contains(jobs, "DailyDigest") {
		t.Errorf("periodic registration survived destroy:\n%s", jobs)
	}
	if !strings.Contains(jobs, "registered += add(workers, &SendWelcomeWorker{Log: log})") {
		t.Errorf("destroy removed the other job:\n%s", jobs)
	}
	if strings.Contains(out.String(), "absent") {
		t.Errorf("destroy reported an absent line:\n%s", out.String())
	}
}

// Without --every the registration is the one worker line, and destroy does
// not go looking for a periodic line that was never written.
func TestJobWithoutEveryIsUnchanged(t *testing.T) {
	root := jobsFixture(t)
	var out bytes.Buffer
	if err := generateIn(root, []string{"job", "send_welcome"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	jobs := readFile(t, root, "app/jobs/jobs.go")
	if strings.Contains(jobs, "periodic = append") || strings.Contains(jobs, "SendWelcomePeriodic") {
		t.Errorf("a plain job registered a schedule:\n%s", jobs)
	}
	if strings.Contains(readFile(t, root, "app/jobs/send_welcome.go"), "robfig/cron") {
		t.Error("a plain job imported cron")
	}
	out.Reset()
	if err := destroyIn(root, []string{"job", "send_welcome"}, &out); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "periodic") {
		t.Errorf("destroy of a plain job mentioned a schedule:\n%s", out.String())
	}
}

// --pretend writes nothing: not the job, not the registration, not a cron
// requirement, and not the River wiring a first job would add.
func TestJobEveryPretendWritesNothing(t *testing.T) {
	t.Run("no jobs yet", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "go.mod", "module example.com/blog\n\ngo 1.25.0\n")
		writeFile(t, root, Marker, "bogie = \"v0.0.0\"\nname = \"blog\"\nmodule = \"example.com/blog\"\n")
		before := treeBytes(t, root)
		var out bytes.Buffer
		if err := generateIn(root, []string{"job", "daily_digest", "--every", "0 9 * * *", "--pretend"}, &out); err != nil {
			t.Fatal(err)
		}
		if diff := treeDiff(before, treeBytes(t, root)); diff != "" {
			t.Errorf("--pretend wrote files:\n%s", diff)
		}
		for _, want := range []string{
			"--jobs would be added first",
			"create  app/jobs/daily_digest.go",
			"periodic = append(periodic, DailyDigestPeriodic())",
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output lacks %q:\n%s", want, out.String())
			}
		}
		if strings.Contains(out.String(), "go get") {
			t.Errorf("--pretend ran go get:\n%s", out.String())
		}
	})

	t.Run("jobs already there", func(t *testing.T) {
		root := jobsFixture(t)
		before := treeBytes(t, root)
		var out bytes.Buffer
		if err := generateIn(root, []string{"job", "daily_digest", "--pretend", "--every", "0 9 * * *"}, &out); err != nil {
			t.Fatal(err)
		}
		if diff := treeDiff(before, treeBytes(t, root)); diff != "" {
			t.Errorf("--pretend changed files:\n%s", diff)
		}
		if !strings.Contains(out.String(), "insert  app/jobs/jobs.go: periodic = append(periodic, DailyDigestPeriodic())") {
			t.Errorf("output:\n%s", out.String())
		}
	})
}

func TestJobEveryRefusesWhatItCannotRegister(t *testing.T) {
	root := jobsFixture(t)
	before := treeBytes(t, root)
	var out bytes.Buffer
	if err := generateIn(root, []string{"controller", "posts", "index", "--every", "0 9 * * *"}, &out); err == nil || !strings.Contains(err.Error(), "--every is for job") {
		t.Fatalf("controller --every: %v", err)
	}
	if diff := treeDiff(before, treeBytes(t, root)); diff != "" {
		t.Errorf("controller --every wrote files:\n%s", diff)
	}
	// An app from before periodic jobs has the marker and no slice. Nothing
	// is written: the error comes before the files.
	writeFile(t, root, "app/jobs/jobs.go", strings.ReplaceAll(jobsGo, "\tvar periodic []*river.PeriodicJob\n", ""))
	before = treeBytes(t, root)
	out.Reset()
	err := generateIn(root, []string{"job", "daily_digest", "--every", "0 9 * * *"}, &out)
	if err == nil || !strings.Contains(err.Error(), "app:update") {
		t.Fatalf("old jobs.go: %v", err)
	}
	if diff := treeDiff(before, treeBytes(t, root)); diff != "" {
		t.Errorf("a refused --every wrote files:\n%s", diff)
	}
	out.Reset()
	if err := generateIn(root, []string{"job", "daily_digest", "--every", "not a cron"}, &out); err == nil {
		t.Fatal("bad cron accepted")
	}
	if diff := treeDiff(before, treeBytes(t, root)); diff != "" {
		t.Errorf("a bad cron wrote files:\n%s", diff)
	}
}

func treeDiff(before, after map[string]string) string {
	var b strings.Builder
	seen := map[string]bool{}
	for path, body := range after {
		seen[path] = true
		if prev, ok := before[path]; !ok {
			b.WriteString("added " + path + "\n")
		} else if prev != body {
			b.WriteString("changed " + path + "\n")
		}
	}
	for path := range before {
		if !seen[path] {
			b.WriteString("removed " + path + "\n")
		}
	}
	return b.String()
}

// The periodic job has to compile in an app, and destroy has to leave that
// app compiling with the schedule gone. This is the generator against the
// real templates, modules included; the worker boot lives in bin/ci, which
// has the database.
func TestPeriodicJobAppBuildsThenDestroyRemovesIt(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	var out bytes.Buffer
	if err := New([]string{"blog", "--jobs", "--module=example.com/blog"}, &out); err != nil {
		t.Fatalf("new: %v\n%s", err, out.String())
	}
	app := filepath.Join(root, "blog")
	out.Reset()
	if err := generateIn(app, []string{"job", "daily_digest", "--every", "0 9 * * *"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	t.Logf("generator output:\n%s", out.String())
	for _, rel := range []string{"app/jobs/daily_digest.go", "app/jobs/jobs.go", "app/application.go"} {
		body := readFile(t, app, rel)
		switch rel {
		case "app/jobs/daily_digest.go":
			if !strings.Contains(body, "RunOnStart: false") || !strings.Contains(body, "NewPeriodicJob") {
				t.Fatalf("%s missing periodic config", rel)
			}
		case "app/jobs/jobs.go":
			if !strings.Contains(body, "periodic = append(periodic, DailyDigestPeriodic())") {
				t.Fatalf("%s missing periodic registration:\n%s", rel, body)
			}
		case "app/application.go":
			if !strings.Contains(body, "PeriodicJobs: periodic") {
				t.Fatalf("%s does not pass periodic jobs to River:\n%s", rel, body)
			}
		}
	}
	if err := goCmd(app, "build", "./..."); err != nil {
		t.Fatalf("build: %v", err)
	}
	if dirty := gofmtDirty(app); dirty != "" {
		t.Fatalf("generated Go is not gofmt-clean:\n%s", dirty)
	}
	if err := goCmd(app, "test", "./app/jobs"); err != nil {
		t.Fatalf("app/jobs tests: %v", err)
	}
	out.Reset()
	if err := destroyIn(app, []string{"job", "daily_digest"}, &out); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(app, "app/jobs/daily_digest.go")); err == nil {
		t.Fatal("daily_digest.go survived destroy")
	}
	if strings.Contains(readFile(t, app, "app/jobs/jobs.go"), "DailyDigest") {
		t.Fatal("periodic registration survived destroy")
	}
	if err := goCmd(app, "build", "./..."); err != nil {
		t.Fatalf("build after destroy: %v", err)
	}
	if err := goCmd(app, "test", "./app/jobs"); err != nil {
		t.Fatalf("app/jobs tests after destroy: %v", err)
	}
}

func gofmtDirty(dir string) string {
	cmd := exec.Command("gofmt", "-l", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err.Error() + "\n" + string(out)
	}
	return string(out)
}

func goCmd(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &goCmdError{err: err, out: out}
	}
	return nil
}

type goCmdError struct {
	err error
	out []byte
}

func (e *goCmdError) Error() string {
	return e.err.Error() + "\n" + string(e.out)
}
