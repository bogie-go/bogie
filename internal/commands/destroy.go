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
controller the three lines that registered it. A namespaced controller's
package goes too once it holds no controller, with the three lines that
registered the namespace. Nothing else is touched; a file you added to a
package by hand stays.

  bogie d scaffold post
  bogie d controller comments
  bogie d controller admin/reports
  bogie d service publish_post
  bogie d job send_welcome
  bogie d model comment
  bogie d migration add_slug_to_posts
`

// Destroy handles `bogie destroy` and `bogie d`, in the app around the
// working directory.
func Destroy(args []string, out io.Writer) error {
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	return destroyIn(root, args, out)
}

// destroyIn is Destroy with the app root given, so a test can hand it a
// fixture.
func destroyIn(root string, args []string, out io.Writer) error {
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
		// The same two files whatever actions generate was given.
		var ns, ctl string
		ns, ctl, err = generate.ParseControllerName(positional[0])
		if err == nil {
			files, _, err = generate.Controller(module, ns, ctl, nil)
			wires = generate.WirePrefixes(ns, ctl)
		}
		// The namespace lives for its controllers: when this was the last
		// one, its server.go and routes.go go too, and the root lines that
		// registered it. Anything else in the package keeps it alive.
		if err == nil && ns != "" {
			dir := filepath.Join(root, "app", "controllers", ns+"_controller")
			removing := map[string]bool{}
			for _, f := range files {
				removing[filepath.Base(f.Path)] = true
			}
			matches, _ := filepath.Glob(filepath.Join(dir, "*"))
			empty := len(matches) > 0
			for _, m := range matches {
				base := filepath.Base(m)
				if !removing[base] && base != "server.go" && base != "routes.go" {
					empty = false
				}
			}
			if empty {
				rel := "app/controllers/" + ns + "_controller/"
				files = append(files, generate.File{Path: rel + "server.go"}, generate.File{Path: rel + "routes.go"})
				wires = append(wires, generate.NamespaceWirePrefixes(ns)...)
			}
		}
	case "service":
		files, err = generate.Service(module, name)
	case "job":
		files, _, err = generate.Job(name)
		wires = generate.JobWirePrefix(name)
	case "model", "scaffold":
		if generator == "model" {
			files, err = generate.Model(module, name, nil, time.Time{})
		} else {
			files, err = generate.ScaffoldFiles(module, name)
			wires = generate.WirePrefixes("", generate.Plural(name))
		}
		// sqlc writes a file per query file and never removes one whose
		// source is gone, so the generated half goes too; and a test the
		// model may have been given by hand.
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

	// A line in a file this run removed (a namespace's server.go, once its
	// last controller goes) went with the file.
	removed := map[string]bool{}
	for _, f := range files {
		removed[f.Path] = true
	}
	var remaining []generate.Wire
	for _, w := range wires {
		if !removed[w.File] {
			remaining = append(remaining, w)
		}
	}
	if err := unwire(root, remaining, *pretend, report); err != nil {
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
