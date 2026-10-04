// Package commands holds one function per bogie subcommand.
package commands

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bogie-go/bogie/internal/scaffold"
	"github.com/bogie-go/bogie/templates"
)

// The tool's version is what bogie.toml records, so a later `bogie
// app:update` knows exactly which templates produced the app: see Version in
// version.go.

// A name becomes a Go module path segment, a package name, an env prefix and a
// binary name, so it is held to the strictest of those.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// New handles `bogie new NAME`.
func New(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(out)
	module := fs.String("module", "", "Go module path of the new app (default: NAME)")
	force := fs.Bool("force", false, "overwrite files that differ")
	pretend := fs.Bool("pretend", false, "report what would be written and write nothing")
	skipTidy := fs.Bool("skip-tidy", false, "do not run go mod tidy in the new app")
	jobs := fs.Bool("jobs", false, "add background jobs: River on the same Postgres, a worker role, app/jobs and g job")
	// Everything the command says goes through say; a failed write to the
	// terminal has nowhere to be reported, so the result is dropped.
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	fs.Usage = func() {
		say("Usage: bogie new NAME [--module=PATH] [--jobs] [--force] [--pretend] [--skip-tidy]\n")
		fs.PrintDefaults()
	}
	// Flags may come after NAME, as in `bogie new blog --module=...`, which
	// the flag package does not do by itself: it stops at the first positional
	// argument. So parse, take the positional, and parse what follows it.
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) != 1 {
		fs.Usage()
		return fmt.Errorf("new: exactly one NAME is required")
	}
	name := positional[0]
	if !namePattern.MatchString(name) {
		return fmt.Errorf("new: %q is not a valid name: lowercase letters, digits and underscores, starting with a letter", name)
	}
	if *module == "" {
		*module = name
	}

	vars := scaffold.Vars{
		Name:          name,
		Module:        *module,
		EnvPrefix:     strings.ToUpper(name),
		LayoutVersion: Version(),
		Jobs:          *jobs,
		Mascot:        templates.MascotDataURI(),
	}
	report := func(a scaffold.Action) { say("%12s  %s\n", a.Op, name+"/"+a.Path) }
	if _, err := scaffold.Render(templates.App, "app", name, vars, scaffold.Options{
		Force: *force, Pretend: *pretend, Report: report,
	}); err != nil {
		return fmt.Errorf("new: %w", err)
	}
	if err := writeCredentials(name, vars, *pretend, report); err != nil {
		return err
	}

	if !*pretend && !*skipTidy {
		say("%12s  go mod tidy\n", "run")
		cmd := exec.Command("go", "mod", "tidy")
		cmd.Dir = name
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("new: go mod tidy in %s: %w", name, err)
		}
	}

	if !*pretend {
		// The same steps, in the same order, as the README's Quickstart.
		say("\nNext:\n"+
			"  cd %s\n"+
			"  make up                                         # postgres on :5440, via docker compose\n"+
			"  bogie g scaffold post title:string body:text    # a first resource, wired in\n"+
			"  bogie db:prepare                                # create, migrate, seed\n"+
			"  bogie s                                         # serves on :8080 (bogie server)\n"+
			"\nThen, from another terminal:\n"+
			"  curl localhost:8080/healthz\n", name)
	}
	return nil
}
