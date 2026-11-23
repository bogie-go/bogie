package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// workerCommand matches the worker entry in main.go's commands map, whatever
// gofmt's column alignment made of the spacing.
var workerCommand = regexp.MustCompile(`"worker":\s*runWorker,`)

// jobsWiringGap is one piece of River wiring an app with app/jobs/jobs.go is
// missing, and the exact fix for it.
type jobsWiringGap struct {
	name string // the doctor check's name
	fix  string
}

// jobsWiringGaps checks the three pieces that make app/jobs/jobs.go live:
// the worker command in main.go, the app/jobs import in app/application.go,
// and jobs = true in bogie.toml. A file that is not on disk is skipped (the
// marker checks and the layout say so already), which also keeps a minimal
// fixture with only app/jobs working. It only means anything when
// app/jobs/jobs.go exists; the caller checks that.
func jobsWiringGaps(root, module string) []jobsWiringGap {
	var gaps []jobsWiringGap
	if src, err := os.ReadFile(filepath.Join(root, "main.go")); err == nil && !workerCommand.Match(src) {
		gaps = append(gaps, jobsWiringGap{"jobs: worker command in main.go",
			`missing; add "worker": runWorker, above // bogie:commands in main.go, or the worker command falls through to serve`})
	}
	jobsImport := module + "/app/jobs"
	if src, err := os.ReadFile(filepath.Join(root, "app", "application.go")); err == nil && !containsQuoted(src, jobsImport) {
		gaps = append(gaps, jobsWiringGap{"jobs: import in app/application.go",
			fmt.Sprintf("missing; add the import %q to app/application.go", jobsImport)})
	}
	if s, err := readSettings(root); err == nil && !s.Jobs {
		gaps = append(gaps, jobsWiringGap{"jobs: bogie.toml",
			"jobs = false although app/jobs exists; set jobs = true in bogie.toml"})
	}
	return gaps
}

func containsQuoted(src []byte, path string) bool {
	return regexp.MustCompile(`(?m)^\s*(?:\w+\s+)?` + regexp.QuoteMeta(fmt.Sprintf("%q", path)) + `\s*$`).Match(src)
}
