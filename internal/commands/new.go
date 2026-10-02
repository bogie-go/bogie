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

// Version is set by the release build and is "dev" otherwise.
var Version = "dev"

// LayoutVersion is written to the generated bogie.toml so a later
// `bogie upgrade` knows which templates produced the app.
const LayoutVersion = "0.1.0"

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
	// Everything the command says goes through say; a failed write to the
	// terminal has nowhere to be reported, so the result is dropped.
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	fs.Usage = func() {
		say("Usage: bogie new NAME [--module=PATH] [--force] [--pretend] [--skip-tidy]\n")
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
		LayoutVersion: LayoutVersion,
	}
	report := func(a scaffold.Action) { say("%12s  %s\n", a.Op, name+"/"+a.Path) }
	if _, err := scaffold.Render(templates.App, "app", name, vars, scaffold.Options{
		Force: *force, Pretend: *pretend, Report: report,
	}); err != nil {
		return fmt.Errorf("new: %w", err)
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
		say("\nNext:\n  cd %s\n  bogie server\n  curl localhost:8080/healthz\n", name)
	}
	return nil
}
