package commands

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bogie-go/bogie/internal/generate"
	"github.com/bogie-go/bogie/internal/scaffold"
)

const destroyUsage = `Usage: bogie destroy GENERATOR NAME [--pretend]
       bogie d ...

Removes what the generator of the same name created: its files, and for a
controller the three lines that registered it. Nothing else is touched; a
file you added to the package by hand stays.

  bogie d controller comments
  bogie d service publish_post
  bogie d job send_welcome
  bogie d model comment
  bogie d migration add_slug_to_posts
`

// Destroy handles `bogie destroy` and `bogie d`.
func Destroy(args []string, out io.Writer) error {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		say(destroyUsage)
		if len(args) == 0 {
			return fmt.Errorf("destroy: which generator?")
		}
		return nil
	}
	generator := args[0]

	fs := flag.NewFlagSet("destroy "+generator, flag.ContinueOnError)
	fs.SetOutput(out)
	pretend := fs.Bool("pretend", false, "report what would be removed and remove nothing")
	fs.Usage = func() { say(destroyUsage) }
	var positional []string
	rest := args[1:]
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
	if len(positional) == 0 {
		say(destroyUsage)
		return fmt.Errorf("destroy %s: NAME is required", generator)
	}
	name := generate.Snake(positional[0])

	root, err := appRoot(".")
	if err != nil {
		return err
	}
	module, err := appModule(root)
	if err != nil {
		return err
	}
	report := func(a scaffold.Action) { say("%12s  %s\n", a.Op, a.Path) }

	// The same generator, run on paper, says which files are its own.
	var files []generate.File
	var wires []generate.Wire
	runSqlc := false
	switch generator {
	case "controller":
		// Validates the name; the files are whatever is in the package,
		// since destroy cannot know which actions generate was given.
		_, _, err = generate.Controller(module, name, nil)
		wires = generate.WirePrefixes(name)
		dir := filepath.Join(root, "app", "controllers", name+"_controller")
		matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			files = append(files, generate.File{Path: filepath.ToSlash(rel)})
		}
	case "service":
		files, err = generate.Service(module, name)
	case "job":
		files, _, err = generate.Job(name)
		wires = generate.JobWirePrefix(name)
	case "model":
		files, err = generate.Model(module, name, nil, time.Time{})
		// sqlc writes a file per query file and never removes one whose
		// source is gone, so the generated half goes too; and a test the
		// model was given by hand, as the example has.
		files = append(files,
			generate.File{Path: "app/models/" + generate.Plural(name) + ".sql.go"},
			generate.File{Path: "app/domain/" + name + "_test.go"})
		runSqlc = true
	case "migration":
		var f generate.File
		f, err = generate.Migration(name, nil, time.Time{})
		files = []generate.File{f}
		runSqlc = true
	default:
		say(destroyUsage)
		return fmt.Errorf("destroy: no generator named %q", generator)
	}
	if err != nil {
		return fmt.Errorf("destroy %s: %w", generator, err)
	}

	for _, f := range files {
		path := f.Path
		if existing := existingMigration(root, path); existing != "" {
			path = existing
		}
		abs := filepath.Join(root, filepath.FromSlash(path))
		if _, err := os.Stat(abs); err != nil {
			report(scaffold.Action{Op: "absent", Path: path})
			continue
		}
		report(scaffold.Action{Op: "remove", Path: path})
		if *pretend {
			continue
		}
		if err := os.Remove(abs); err != nil {
			return err
		}
		// A controller or service package directory goes once it is empty.
		// The layout's own directories (db/migrate, app/domain, ...) stay.
		if generator == "controller" || generator == "service" {
			_ = os.Remove(filepath.Dir(abs))
		}
	}

	if err := unwire(root, wires, *pretend, report); err != nil {
		return fmt.Errorf("destroy %s: %w", generator, err)
	}

	if runSqlc && !*pretend {
		say("%12s  if that migration was already applied somewhere, bogie db:reset rebuilds the database from the files that remain\n", "note")
		if !hasQueries(root) {
			say("%12s  sqlc generate (no query files left; the next g model runs it)\n", "skip")
			return nil
		}
		say("%12s  sqlc generate\n", "run")
		cmd := exec.Command("go", "tool", "sqlc", "generate")
		cmd.Dir = root + "/db"
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("destroy %s: sqlc: %w", generator, err)
		}
	}
	return nil
}
