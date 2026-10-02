package generate

import "fmt"

// Job writes app/jobs/<name>.go, a River job: an Args type with a Kind, a
// Worker with Work, and a test; and the line that registers the worker above
// the bogie:jobs marker.
func Job(name string) ([]File, []Wire, error) {
	if !namePattern.MatchString(name) {
		return nil, nil, fmt.Errorf("%q is not a job name: lowercase letters, digits and underscores", name)
	}
	typ := Camel(name)
	files := []File{
		{Path: "app/jobs/" + name + ".go", Content: jobFile(name, typ)},
		{Path: "app/jobs/" + name + "_test.go", Content: jobTest(name, typ)},
	}
	wires := []Wire{{
		File:   "app/jobs/jobs.go",
		Marker: "jobs",
		Line:   fmt.Sprintf("registered += add(workers, &%sWorker{Log: log})", typ),
	}}
	return files, wires, nil
}

// JobWirePrefix is what destroy removes.
func JobWirePrefix(name string) []Wire {
	return []Wire{{File: "app/jobs/jobs.go", Line: "registered += add(workers, &" + Camel(name) + "Worker{"}}
}

func jobFile(name, typ string) string {
	return fmt.Sprintf(`package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// %[2]sArgs is what a %[1]s job carries. It is stored as JSON in the job row,
// so keep it to ids and small values; the worker loads what it needs.
type %[2]sArgs struct {
	// ID string `+"`"+`json:"id"`+"`"+`
}

// Kind names the job in the database. Never change it once jobs are queued;
// a queued job of the old kind would have no worker.
func (%[2]sArgs) Kind() string { return %[3]q }

// %[2]sWorker does the work. Enqueue one with:
//
//	app.Jobs.Insert(ctx, jobs.%[2]sArgs{}, nil)
//
// Dependencies go here as fields, declared as the smallest interface this
// worker uses (rule 2), and are passed on the registration line in jobs.go.
type %[2]sWorker struct {
	river.WorkerDefaults[%[2]sArgs]
	Log *slog.Logger
}

// Work runs one job. Return an error to have River retry it with backoff;
// return nil when it is done. ctx is cancelled when the worker drains.
func (w *%[2]sWorker) Work(ctx context.Context, job *river.Job[%[2]sArgs]) error {
	w.Log.InfoContext(ctx, "%[1]s: not implemented yet", "job", job.ID)
	return nil
}
`, name, typ, name)
}

func jobTest(name, typ string) string {
	return fmt.Sprintf(`package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func Test%[2]sWork(t *testing.T) {
	w := &%[2]sWorker{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	job := &river.Job[%[2]sArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: %[2]sArgs{}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work: %%v", err)
	}
}
`, name, typ)
}
