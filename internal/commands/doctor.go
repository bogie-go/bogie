package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bogie-go/bogie/internal/markers"
)

// Doctor checks that the app still has what the generators rely on: the
// markers, exactly once each; the pinned tools; the files the layout needs;
// and that every controller and namespace is registered. One line per
// check, exit 1 if any failed.
func Doctor(out io.Writer) error {
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	failed := 0
	check := func(name string, err error) {
		if err == nil {
			_, _ = fmt.Fprintf(out, "%6s  %s\n", "ok", name)
			return
		}
		failed++
		_, _ = fmt.Fprintf(out, "%6s  %s: %v\n", "FAIL", name, err)
	}
	read := func(rel string) ([]byte, error) { return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) }
	exists := func(rel string) error {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("missing")
		}
		return nil
	}

	check("bogie.toml", exists(Marker))

	markerFiles := []struct{ file, marker string }{
		{"app/controllers/application.go", "controllers"},
		{"app/application.go", "wire"},
		{"app/controllers/routes.go", "routes"},
		{"config/config.go", "config"},
		{"config/config.go", "env"},
		{"config/config.go", "validate"},
		{"main.go", "commands"},
	}
	if exists("app/jobs/jobs.go") == nil {
		markerFiles = append(markerFiles, struct{ file, marker string }{"app/jobs/jobs.go", "jobs"})
	}
	// A namespace package carries the same two markers as the root.
	for _, d := range namespaceDirs(root) {
		rel := "app/controllers/" + filepath.Base(d) + "/"
		markerFiles = append(markerFiles,
			struct{ file, marker string }{rel + "server.go", "controllers"},
			struct{ file, marker string }{rel + "routes.go", "routes"})
	}
	for _, m := range markerFiles {
		src, err := read(m.file)
		if err == nil {
			err = markers.Check(src, m.file, m.marker)
		}
		check("marker bogie:"+m.marker+" in "+m.file, err)
	}

	gomod, _ := read("go.mod")
	tools := []string{"github.com/pressly/goose/v3/cmd/goose", "github.com/sqlc-dev/sqlc/cmd/sqlc", "github.com/bogie-go/credentials/cmd/credentials"}
	// Live reload is opt-out: the tool is expected only where its config is,
	// so an app that deleted .air.toml is not nagged about a tool it removed
	// on purpose, and one that kept it is told when `go mod tidy` has not run.
	if exists(airConfig) == nil {
		tools = append(tools, airModule)
	}
	for _, tool := range tools {
		var err error
		if !strings.Contains(string(gomod), tool) {
			err = fmt.Errorf("not under the tool directive in go.mod")
		}
		check("tool "+tool[strings.LastIndex(tool, "/")+1:], err)
	}

	for _, f := range []string{"db/sqlc.yaml", "db/db.go", "bin/ci", "docker-compose.yml", "AGENTS.md"} {
		check(f, exists(f))
	}
	if err := exists("config/master.key"); err != nil {
		check("config/master.key", fmt.Errorf("missing; development reads the environment only until someone copies it over"))
	} else {
		check("config/master.key", nil)
	}

	// Every controller must be constructed in app/application.go and
	// mounted: a plain one (app/controllers/<name>_controller.go) on the root
	// Server, a namespace package (app/controllers/<ns>_controller/) on the
	// root too, and a controller inside a namespace on that namespace's
	// Server, in its routes.go. A mount with no construction is a nil
	// controller whose first request panics; a construction with no mount
	// serves nothing.
	type registration struct {
		label     string
		construct string // what app/application.go assigns, after "server."
		mountIn   string // the routes.go that must call SetupRoutes on it
		mount     string // the field SetupRoutes is called on, after "s."
	}
	var regs []registration
	plain, _ := filepath.Glob(filepath.Join(root, "app", "controllers", "*_controller.go"))
	for _, f := range plain {
		name := strings.TrimSuffix(filepath.Base(f), "_controller.go")
		field := camelField(name)
		regs = append(regs, registration{"controller " + name, field, "app/controllers/routes.go", field})
	}
	for _, d := range namespaceDirs(root) {
		ns := strings.TrimSuffix(filepath.Base(d), "_controller")
		nsField := camelField(ns)
		regs = append(regs, registration{"namespace " + ns, nsField, "app/controllers/routes.go", nsField})
		inner, _ := filepath.Glob(filepath.Join(d, "*_controller.go"))
		for _, f := range inner {
			name := strings.TrimSuffix(filepath.Base(f), "_controller.go")
			field := camelField(name)
			regs = append(regs, registration{"controller " + ns + "/" + name, nsField + "." + field, "app/controllers/" + filepath.Base(d) + "/routes.go", field})
		}
	}
	wiring, _ := read("app/application.go")
	for _, r := range regs {
		routes, _ := read(r.mountIn)
		var err error
		switch {
		case !regexp.MustCompile(`\bserver\.` + regexp.QuoteMeta(r.construct) + `\s*=`).Match(wiring):
			err = fmt.Errorf("not constructed in app/application.go (expected server.%s = ... above // bogie:wire)", r.construct)
		case !regexp.MustCompile(`\bs\.` + regexp.QuoteMeta(r.mount) + `\.SetupRoutes\(`).Match(routes):
			err = fmt.Errorf("not mounted in %s (expected s.%s.SetupRoutes above // bogie:routes)", r.mountIn, r.mount)
		}
		check(r.label+" registered", err)
	}

	if failed > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", failed)
	}
	_, _ = fmt.Fprintln(out, "\nAll checks passed.")
	return nil
}

// namespaceDirs are the namespace packages, app/controllers/<ns>_controller/.
func namespaceDirs(root string) []string {
	dirs, _ := filepath.Glob(filepath.Join(root, "app", "controllers", "*_controller"))
	return dirs
}

func camelField(snake string) string {
	parts := strings.Split(snake, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
