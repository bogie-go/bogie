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
// and that every controller package is registered. One line per check, exit
// 1 if any failed.
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

	for _, m := range []struct{ file, marker string }{
		{"app/controllers/application.go", "controllers"},
		{"app/application.go", "wire"},
		{"app/controllers/routes.go", "routes"},
	} {
		src, err := read(m.file)
		if err == nil {
			err = markers.Check(src, m.file, m.marker)
		}
		check("marker bogie:"+m.marker, err)
	}

	gomod, _ := read("go.mod")
	for _, tool := range []string{"github.com/pressly/goose/v3/cmd/goose", "github.com/sqlc-dev/sqlc/cmd/sqlc", "github.com/bogie-go/credentials/cmd/credentials"} {
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

	// Every app/controllers/<x>_controller package must be constructed in
	// app/application.go and mounted in routes.go, or it exists without
	// serving anything. A mount with no construction is a nil controller
	// whose first request panics.
	routes, _ := read("app/controllers/routes.go")
	wiring, _ := read("app/application.go")
	dirs, _ := filepath.Glob(filepath.Join(root, "app", "controllers", "*_controller"))
	for _, d := range dirs {
		pkg := filepath.Base(d)
		field := camelField(strings.TrimSuffix(pkg, "_controller"))
		var err error
		switch {
		case !regexp.MustCompile(`\bserver\.` + field + `\s*=`).Match(wiring):
			err = fmt.Errorf("not constructed in app/application.go (expected server.%s = ... above // bogie:wire)", field)
		case !regexp.MustCompile(`\bs\.` + field + `\.SetupRoutes\(`).Match(routes):
			err = fmt.Errorf("not mounted in app/controllers/routes.go (expected s.%s.SetupRoutes above // bogie:routes)", field)
		}
		check("controller "+pkg+" registered", err)
	}

	if failed > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", failed)
	}
	_, _ = fmt.Fprintln(out, "\nAll checks passed.")
	return nil
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
