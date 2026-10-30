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
controller the three lines that registered it. For a config group, its file
and its lines in config/config.go; the group's lines in the development
credentials stay, harmless. A namespaced controller's
package goes too once it holds no controller, with the three lines that
registered the namespace. A scaffold_controller leaves the model and its store,
and a root one its views, which other controllers may use; a namespaced one
removes the views it wrote (app/views/admin_categories.go). Nothing else is
touched; a file you added to a package by hand stays.

  bogie d scaffold post
  bogie d controller comments
  bogie d controller admin/reports
  bogie d scaffold_controller admin/categories
  bogie d service publish_post
  bogie d job send_welcome
  bogie d seed demo_posts
  bogie d authentication token
  bogie d config smtp
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
	// As in generate: only the generators that write an id or a references
	// column need the app's recorded scheme.
	var settings Settings
	var scheme generate.IDScheme
	if generator == "migration" || generator == "model" || generator == "scaffold" || generator == "authentication" {
		settings, err = readSettings(root)
		if err != nil {
			return fmt.Errorf("destroy %s: %w", generator, err)
		}
		scheme = settings.IDScheme
	}
	report := func(a scaffold.Action) { say("%12s  %s\n", a.Op, a.Path) }

	// The same generator, run on paper, says which files are its own.
	var files []generate.File
	var wires []generate.Wire
	runSqlc := false
	switch generator {
	case "controller", "scaffold_controller":
		// The same two files whatever actions generate was given. A scaffold
		// controller is a controller named for its model's table, and leaves
		// the model and its store. A root one leaves its views too, which
		// other controllers use; a namespaced one wrote views of its own.
		var ns, ctl string
		ns, ctl, err = generate.ParseControllerName(positional[0])
		if err == nil && generator == "scaffold_controller" {
			ctl = generate.Plural(generate.Singular(ctl))
		}
		if err == nil {
			files, _, err = generate.Controller(module, ns, ctl, nil)
			wires = generate.WirePrefixes(ns, ctl)
			if generator == "scaffold_controller" && ns != "" {
				files = append(files, generate.File{Path: generate.ViewsPath(ns, generate.Singular(ctl))})
			}
		}
		if err == nil && ns != "" {
			files, wires = dropEmptyNamespace(root, ns, files, wires)
		}
	case "service":
		files, err = generate.Service(module, name)
	case "job":
		files, _, err = generate.Job(module, name)
		wires = generate.JobWirePrefix(name)
	case "seed":
		files, _, err = generate.Seed(module, name)
		wires = generate.SeedWirePrefix(name)
	case "config":
		// The app's own groups are never removed, and must not be mistaken
		// for a flat key of the same name either.
		if generate.IsCoreConfigGroup(name) {
			err = generate.ParseConfigGroup(name)
			break
		}
		// A group has its own file; a flat key is only lines in config.go.
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(generate.ConfigPath(name)))); statErr != nil {
			if src, readErr := os.ReadFile(filepath.Join(root, "config", "config.go")); readErr == nil && generate.ConfigGroupHas(src, name) {
				wires = generate.ConfigFlatWirePrefixes(name)
				break
			}
		}
		if err = generate.ParseConfigGroup(name); err == nil {
			files = []generate.File{{Path: generate.ConfigPath(name)}}
			wires = generate.ConfigGroupWirePrefixes(name)
		}
	case "authentication":
		files, err = generate.AuthenticationFiles(module, settings.Name, name, scheme)
		wires = generate.AuthenticationWirePrefixes(settings.Name, name)
		// The secret's config group goes with the shape that read it.
		if setting, ok := generate.AuthenticationSetting(name); ok {
			files = append(files, generate.File{Path: generate.ConfigPath(setting.Group)})
			wires = append(wires, generate.ConfigGroupWirePrefixes(setting.Group)...)
		}
		if name == "api_key" {
			files = append(files, generate.File{Path: "db/sqlc/api_keys.sql.go"})
			runSqlc = true
		}
	case "model", "scaffold":
		if generator == "model" {
			files, err = generate.Model(module, name, nil, scheme, time.Time{})
		} else {
			// admin/posts removes the model admin_post and its controller
			// in the admin namespace.
			var ns, res, model string
			ns, res, model, err = generate.ParseScaffoldName(positional[0])
			if err != nil {
				break
			}
			name = model
			if res == "" {
				res = generate.Plural(model)
			}
			files, err = generate.ScaffoldFiles(module, ns, res, name, scheme)
			wires = generate.WirePrefixes(ns, res)
			if err == nil && ns != "" {
				files, wires = dropEmptyNamespace(root, ns, files, wires)
			}
		}
		// sqlc writes a file per query file and never removes one whose
		// source is gone, so the generated half goes too.
		files = append(files,
			generate.File{Path: "db/sqlc/" + generate.Plural(name) + ".sql.go"})
		runSqlc = true
	case "migration":
		var f generate.File
		f, err = generate.Migration(name, nil, scheme, time.Time{})
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
		// The layout's own directories (db/migrate, app/models, ...) stay.
		if generator == "controller" || generator == "scaffold_controller" || generator == "scaffold" || generator == "service" {
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

// dropEmptyNamespace adds a namespace's own files and the root lines that
// registered it to what is being removed, when the files already listed are
// the last controllers in its package. The namespace lives for its
// controllers: anything else in the package keeps it alive.
func dropEmptyNamespace(root, ns string, files []generate.File, wires []generate.Wire) ([]generate.File, []generate.Wire) {
	dir := filepath.Join(root, "app", "controllers", ns+"_controllers")
	removing := map[string]bool{}
	for _, f := range files {
		if filepath.ToSlash(filepath.Dir(f.Path)) == "app/controllers/"+ns+"_controllers" {
			removing[filepath.Base(f.Path)] = true
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	empty := len(matches) > 0
	for _, m := range matches {
		base := filepath.Base(m)
		if !removing[base] && base != "server.go" && base != "routes.go" && base != filepath.Base(generate.NamespaceSuitePath(ns)) {
			empty = false
		}
	}
	if !empty {
		return files, wires
	}
	rel := "app/controllers/" + ns + "_controllers/"
	files = append(files, generate.File{Path: rel + "server.go"}, generate.File{Path: rel + "routes.go"}, generate.File{Path: generate.NamespaceSuitePath(ns)})
	return files, append(wires, generate.NamespaceWirePrefixes(ns)...)
}
