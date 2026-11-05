package generate

import (
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// Job writes app/jobs/<name>.go, a River job: an Args type with a Kind, a
// Worker with Work, and a test; and the line that registers the worker above
// the bogie:jobs marker. every, when set, is a cron schedule (robfig/cron's
// ParseStandard): the job is also registered as a river.PeriodicJob on that
// schedule, RunOnStart left off. An empty every leaves the job as a worker
// only, the same files and the same one line as before.
func Job(module, name, every string) ([]File, []Wire, error) {
	if !namePattern.MatchString(name) {
		return nil, nil, fmt.Errorf("%q is not a job name: lowercase letters, digits and underscores", name)
	}
	every, err := parseEvery(every)
	if err != nil {
		return nil, nil, err
	}
	typ := Camel(name)
	content, test := jobFile(name, typ), jobTest(module, name, typ)
	wires := []Wire{{
		File:   "app/jobs/jobs.go",
		Marker: "jobs",
		Line:   fmt.Sprintf("registered += add(workers, &%sWorker{Log: log})", typ),
	}}
	if every != "" {
		content = periodicJobFile(name, typ, every)
		test = periodicJobTest(module, name, typ)
		wires = append(wires, Wire{
			File:   "app/jobs/jobs.go",
			Marker: "jobs",
			Line:   fmt.Sprintf("periodic = append(periodic, %sPeriodic())", typ),
		})
	}
	files := []File{
		{Path: "app/jobs/" + name + ".go", Content: content},
		{Path: "app/jobs/" + name + "_test.go", Content: test},
	}
	return files, wires, nil
}

// parseEvery accepts the empty string (not a periodic job) and a schedule
// robfig/cron's ParseStandard accepts: five fields, a descriptor such as
// @daily, or a CRON_TZ= prefix. Anything else is an error, and the value
// returned is trimmed so the generated source carries the schedule that was
// validated.
func parseEvery(every string) (string, error) {
	trimmed := strings.TrimSpace(every)
	if every != "" && trimmed == "" {
		return "", fmt.Errorf("--every is a cron schedule, for example \"0 9 * * *\"")
	}
	if trimmed == "" {
		return "", nil
	}
	if _, err := cron.ParseStandard(trimmed); err != nil {
		return "", fmt.Errorf("--every %q is not a cron schedule (%v): five fields, as in \"0 9 * * *\", or @daily, or CRON_TZ=UTC 0 9 * * *", trimmed, err)
	}
	return trimmed, nil
}

// JobWirePrefix is the worker line destroy removes. The periodic line, when
// the job was generated with --every, is JobPeriodicWirePrefix.
func JobWirePrefix(name string) []Wire {
	return []Wire{{File: "app/jobs/jobs.go", Line: "registered += add(workers, &" + Camel(name) + "Worker{"}}
}

// JobPeriodicWirePrefix is the schedule line. Destroy includes it only when
// the line is present, so a job generated without --every is removed exactly
// as before.
func JobPeriodicWirePrefix(name string) Wire {
	return Wire{File: "app/jobs/jobs.go", Line: "periodic = append(periodic, " + Camel(name) + "Periodic())"}
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
// worker uses, next to the worker, and are passed on the registration line in jobs.go.
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

func jobTest(module, name, typ string) string {
	return fmt.Sprintf(`package jobs_test

import (
	"context"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"%[3]s/app/jobs"
)

var _ = Describe("%[2]sWorker", func() {
	var (
		worker *jobs.%[2]sWorker
		job    *river.Job[jobs.%[2]sArgs]
	)

	Describe("Work", func() {
		Context("with a job and no dependencies", func() {
			BeforeEach(func() {
				worker = &jobs.%[2]sWorker{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
				job = &river.Job[jobs.%[2]sArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: jobs.%[2]sArgs{}}
			})

			It("succeeds", func() {
				Expect(worker.Work(context.Background(), job)).To(Succeed())
			})
		})
	})
})
`, name, typ, module)
}

func periodicJobFile(name, typ, every string) string {
	return fmt.Sprintf(`package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"
)

// %[2]sArgs is what a %[1]s job carries. It is stored as JSON in the job row,
// so keep it to ids and small values; the worker loads what it needs.
type %[2]sArgs struct {
	// ID string `+"`"+`json:"id"`+"`"+`
}

// Kind names the job in the database. Never change it once jobs are queued;
// a queued job of the old kind would have no worker.
func (%[2]sArgs) Kind() string { return %[1]q }

// %[2]sWorker does the work. Enqueue one with:
//
//	app.Jobs.Insert(ctx, jobs.%[2]sArgs{}, nil)
//
// River also enqueues one on the schedule in %[2]sPeriodic.
//
// Dependencies go here as fields, declared as the smallest interface this
// worker uses, next to the worker, and are passed on the registration line in jobs.go.
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

// %[2]sPeriodic is the schedule registered in jobs.go. %[3]q is a
// standard cron expression (minute hour day month weekday); with no CRON_TZ=
// prefix it is the process's local time. RunOnStart is off, so a deploy or a
// new leader does not enqueue one immediately.
func %[2]sPeriodic() *river.PeriodicJob {
	schedule, err := cron.ParseStandard(%[3]q)
	if err != nil {
		// bogie g job only writes an expression ParseStandard accepts.
		panic(err)
	}
	return river.NewPeriodicJob(
		schedule,
		func() (river.JobArgs, *river.InsertOpts) {
			return %[2]sArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	)
}
`, name, typ, every)
}

func periodicJobTest(module, name, typ string) string {
	return jobTest(module, name, typ) + fmt.Sprintf(`
var _ = Describe("%[1]sPeriodic", func() {
	It("builds the periodic job", func() {
		Expect(jobs.%[1]sPeriodic()).NotTo(BeNil())
	})
})
`, typ)
}
