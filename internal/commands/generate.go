package commands

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bogie-go/bogie/internal/generate"
	"github.com/bogie-go/bogie/internal/markers"
	"github.com/bogie-go/bogie/internal/scaffold"
)

const generateUsage = `Usage: bogie generate GENERATOR NAME [attributes] [--only ACTIONS] [--pretend] [--force] [--skip-sqlc]
       bogie g ...

Generators:
  migration NAME [field[:type] ...] db/migrate/<stamp>_NAME.sql
                                    create_<table>, add_<x>_to_<table> and
                                    remove_<x>_from_<table> write their SQL
  model NAME [field[:type] ...]     the migration, db/queries/<table>.sql,
                                    app/models/<name>.go and the store's
                                    db/adapter/<table>.go; then runs sqlc
  scaffold NAME [field[:type] ...]  the model, plus app/views/<table>.go and
                                    a <Table>Controller with real handlers
                                    over the store, REGISTERED. The model is
                                    singular, the rest plural. --only
                                    index,show writes just those actions,
                                    with their routes, views and specs
                                    (the model half is written in full).
                                    admin/NAME is a model of its own, as in
                                    Rails: admin_page, table admin_pages,
                                    its controller PagesController in the
                                    admin namespace at /admin/pages
  scaffold_controller [NS/]RESOURCE the HTTP half of a scaffold for a model
                                    that already exists, read from
                                    app/models: admin/categories mounts the
                                    five actions over the category store
                                    under /admin/categories. The model is
                                    the last segment, not namespaced;
                                    --only works here too
  controller NAME [action ...]      app/controllers/<name>_controller.go, one
                                    controller in one file, REGISTERED:
                                    a field, a wire line and a mount above
                                    each marker. Name a resource in the
                                    plural: posts answers at
                                    /posts, post at /post. admin/NAME puts it
                                    in the admin namespace, the package
                                    app/controllers/admin_controllers/, made
                                    on first use and registered the same way
  service NAME                      app/services/<name>/, ctx first, no HTTP
  job NAME                          app/jobs/<name>.go, a River job, REGISTERED
                                    above bogie:jobs; the first one on an app
                                    made without --jobs adds it first
  seed NAME                         db/seeds/<name>.go, REGISTERED above
                                    bogie:seeds in db/seeds/seeds.go. Unlike a
                                    migration, db seed runs every seed every
                                    time: write it safe to run twice
  config GROUP key[:type][:flag][=value] ...
  config key[:type][:flag]=value    a group of the credentials file (or, with a
                                    value or type, a flat top-level key): a typed
                                    struct in config/<group>.go, registered in
                                    config.go, with a line per key in the
                                    development credentials. Types: string
                                    (default) int bool duration (integer and
                                    boolean work too); flags: req
                                    (production refuses to boot without it),
                                    secret (the same, never given a made-up
                                    value). smtp.address is SMTP_ADDRESS;
                                    run again to add keys
  authentication SHAPE              a middleware on a route group, its config
                                    key with a development value written to
                                    the credentials, and a WithX group helper
                                    on the root Server. SHAPE is secret (a
                                    trusted service calls this one), token
                                    (bearer JWT a user presents) or api_key
                                    (keys for machine clients, with a table
                                    and api_keys:create)

Attributes are field:type, with :index, :uniq or :null after the type (a column
is NOT NULL unless it says :null). Types:
  string text integer bigint boolean datetime uuid jsonb references
(int and bool are accepted too, as in g config, which takes integer and boolean.)
A field with no :type defaults to string. A references field
names the other model: post:references is post_id.

  bogie g scaffold post title body:text
  bogie g scaffold category name:string:uniq --only index
  bogie g scaffold_controller admin/categories
  bogie g model comment body:text post:references
  bogie g migration add_slug_to_posts slug:string:uniq
  bogie g controller comments index show create
  bogie g controller admin/reports index show
  bogie g service publish_post
  bogie g job send_welcome
  bogie g seed demo_posts
  bogie g config smtp address:req port:int=1025 username password:secret
  bogie g config support_email=help@example.com
  bogie g authentication secret
`

// Generate handles `bogie generate` and `bogie g`, in the app around the
// working directory.
func Generate(args []string, out io.Writer) error {
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	return generateIn(root, args, out)
}

// generateIn is Generate with the app root given, so a test can hand it a
// fixture.
func generateIn(root string, args []string, out io.Writer) error {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		say(generateUsage)
		if len(args) == 0 {
			return fmt.Errorf("generate: which generator?")
		}
		return nil
	}
	generator := args[0]

	fs := flag.NewFlagSet("generate "+generator, flag.ContinueOnError)
	fs.SetOutput(out)
	force := fs.Bool("force", false, "overwrite files that differ")
	pretend := fs.Bool("pretend", false, "report what would be written and write nothing")
	skipSqlc := fs.Bool("skip-sqlc", false, "do not run sqlc afterwards")
	onlySpec := fs.String("only", "", "write only these actions (index,show,create,update,destroy)")
	fs.Usage = func() { say(generateUsage) }

	// Flags anywhere, positionals in order, as for `new`.
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
		say(generateUsage)
		return fmt.Errorf("generate %s: NAME is required", generator)
	}
	// Post, BlogPost and AddSlugToPosts are what a hand types; every
	// generator works in snake_case.
	name, attrArgs := generate.Snake(positional[0]), positional[1:]

	module, err := appModule(root)
	if err != nil {
		return err
	}
	// Only migration, model, scaffold and authentication write an id or a
	// references column, so only they need the app's recorded scheme; the
	// rest (controller, service, job, seed) ask nothing of bogie.toml
	// beyond the module, as before this setting existed.
	var settings Settings
	var scheme generate.IDScheme
	if generator == "migration" || generator == "model" || generator == "scaffold" || generator == "scaffold_controller" || generator == "authentication" {
		settings, err = readSettings(root)
		if err != nil {
			return fmt.Errorf("generate %s: %w", generator, err)
		}
		scheme = settings.IDScheme
	}
	var only []string
	// The controller's views file a second scaffold_controller adds types to, when it exists.
	var viewsPath string
	var viewsMerged []byte
	var viewsAdded []string
	if *onlySpec != "" {
		if generator != "scaffold" && generator != "scaffold_controller" {
			return fmt.Errorf("generate %s: --only is for scaffold and scaffold_controller", generator)
		}
		only, err = generate.ParseOnly(*onlySpec)
		if err != nil {
			return fmt.Errorf("generate %s: %w", generator, err)
		}
	}
	var files []generate.File
	var wires []generate.Wire
	runSqlc := false
	// A config group, from `g config` or the secret an authentication shape
	// reads: its keys get lines in the development credentials afterwards.
	var configKeys, configFresh []generate.ConfigKey
	var configGroup string
	configRandom, configFlat := false, false
	switch generator {
	case "controller":
		ns, ctl, err := generate.ParseControllerName(positional[0])
		if err != nil {
			return fmt.Errorf("generate controller: %w", err)
		}
		// The namespace package is written on first use, with the three
		// root lines that register it; after that only the controller's
		// own lines are inserted.
		if ns != "" {
			if _, err := os.Stat(filepath.Join(root, "app", "controllers", ns+"_controllers", "server.go")); err != nil {
				files, wires, err = generate.Namespace(module, ns)
				if err != nil {
					return fmt.Errorf("generate controller: %w", err)
				}
			}
		}
		ctlFiles, ctlWires, err := generate.Controller(module, ns, ctl, attrArgs)
		if err != nil {
			return fmt.Errorf("generate controller: %w", err)
		}
		files, wires = append(files, ctlFiles...), append(wires, ctlWires...)
		// A resource controller is named in the plural and the name is used
		// as typed, so `g controller post show` answers at /post/:id.
		// Say so once, last, since the path is what the caller will
		// notice.
		if generate.LooksLikeASingularResource(ctl, attrArgs) {
			defer say("%12s  A resource is named in the plural: %s would answer at /%s; %s answers at /%s\n", "note", generate.Plural(ctl), generate.Plural(ctl), ctl, ctl)
		}
	case "scaffold_controller":
		if len(attrArgs) > 0 {
			return fmt.Errorf("generate scaffold_controller: the attributes are read from the model, so give none (got %s)", strings.Join(attrArgs, " "))
		}
		ns, resource, err := generate.ParseControllerName(positional[0])
		if err != nil {
			return fmt.Errorf("generate scaffold_controller: %w", err)
		}
		// Models are never namespaced: admin/categories is the category
		// model's controller, so the model is the last segment, singular.
		model := generate.Singular(resource)
		src, err := os.ReadFile(filepath.Join(root, "app", "models", model+".go"))
		if err != nil {
			return fmt.Errorf("generate scaffold_controller: no model %s (app/models/%s.go): make it first with bogie g model %s, or use bogie g scaffold", model, model, model)
		}
		if _, err := os.Stat(filepath.Join(root, "db", "adapter", generate.Plural(model)+".go")); err != nil {
			return fmt.Errorf("generate scaffold_controller: model %s has no store (db/adapter/%s.go)", model, generate.Plural(model))
		}
		modelAttrs, err := generate.ModelAttrs(src, model, scheme)
		if err != nil {
			return fmt.Errorf("generate scaffold_controller: app/models/%s.go: %w", model, err)
		}
		if ns != "" {
			if _, err := os.Stat(filepath.Join(root, "app", "controllers", ns+"_controllers", "server.go")); err != nil {
				files, wires, err = generate.Namespace(module, ns)
				if err != nil {
					return fmt.Errorf("generate scaffold_controller: %w", err)
				}
			}
		}
		ctlFiles, ctlWires, err := generate.ScaffoldController(module, ns, generate.Plural(model), model, modelAttrs, scheme, only)
		if err != nil {
			return fmt.Errorf("generate scaffold_controller: %w", err)
		}
		// A namespaced controller has wire types of its own (AdminProduct...),
		// in app/views/admin_products.go. When that file is already there, a
		// second scaffold_controller over the model, with other actions,
		// completes it: the file in use is kept, hand-edited or not, and only
		// gains the types this controller needs and it lacks.
		for _, f := range ctlFiles {
			if f.Path != generate.ViewsPath(ns, model) {
				files = append(files, f)
				continue
			}
			existing, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
			if err != nil {
				files = append(files, f)
				continue
			}
			merged, added, err := generate.MergeViews(existing, module, ns, model, modelAttrs, scheme, only)
			if err != nil {
				return fmt.Errorf("generate scaffold_controller: %s: %w", f.Path, err)
			}
			if len(added) == 0 {
				say("%12s  %s (it has the types this controller needs)\n", "exist", f.Path)
				continue
			}
			viewsPath, viewsMerged, viewsAdded = f.Path, merged, added
		}
		wires = append(wires, ctlWires...)
	case "service":
		files, err = generate.Service(module, name)
		if err != nil {
			return fmt.Errorf("generate service: %w", err)
		}
	case "job":
		jobsMissing := false
		if _, err := os.Stat(filepath.Join(root, "app", "jobs", "jobs.go")); err != nil {
			if *pretend {
				// Adding --jobs rewrites a dozen files and tidies the modules,
				// so --pretend only says it would.
				say("%12s  this app has no app/jobs yet; --jobs would be added first\n", "note")
				jobsMissing = true
			} else {
				say("%12s  this app has no app/jobs yet; adding --jobs\n", "note")
				if err := enableJobs(root, out); err != nil {
					return fmt.Errorf("generate job: %w", err)
				}
			}
		}
		files, wires, err = generate.Job(module, name)
		if err != nil {
			return fmt.Errorf("generate job: %w", err)
		}
		if jobsMissing {
			// app/jobs/jobs.go, with its markers, does not exist to be checked.
			would := wires
			defer func() {
				for _, w := range would {
					say("%12s  %s: %s\n", "insert", w.File, w.Line)
				}
			}()
			wires = nil
		}
	case "seed":
		files, wires, err = generate.Seed(module, name)
		if err != nil {
			return fmt.Errorf("generate seed: %w", err)
		}
	case "config":
		if generate.IsFlatConfigArg(positional[0]) {
			// A flat key (hello=world): a top-level entry with no group.
			configKeys, err = generate.ParseFlatConfigKeys(positional)
			if err != nil {
				return fmt.Errorf("generate config: %w", err)
			}
			wires, configFresh, err = planFlatConfig(root, configKeys)
			if err != nil {
				return fmt.Errorf("generate config: %w", err)
			}
			configFlat = true
			break
		}
		if len(positional) == 1 {
			return fmt.Errorf("generate config: %s is a group name, so give it keys: bogie g config %s address port:int (or for a flat key with a value: bogie g config %s=value)", name, name, name)
		}
		if err := generate.CheckConfigGroupName(name); err != nil {
			return fmt.Errorf("generate config: %w", err)
		}
		configKeys, err = generate.ParseConfigKeys(attrArgs)
		if err != nil {
			return fmt.Errorf("generate config: %w", err)
		}
		files, wires, configFresh, err = planConfigGroup(root, name, configKeys)
		if err != nil {
			return fmt.Errorf("generate config: %w", err)
		}
		configGroup = name
	case "authentication":
		files, wires, err = generate.Authentication(module, settings.Name, name, scheme, nextStamp(root, time.Now()))
		if err != nil {
			return fmt.Errorf("generate authentication: %w", err)
		}
		runSqlc = name == "api_key"
		// The secret lives in a config group (token.secret); add it the way
		// `g config` would.
		if setting, ok := generate.AuthenticationSetting(name); ok {
			groupFiles, groupWires, fresh, err := planConfigGroup(root, setting.Group, []generate.ConfigKey{setting.ConfigKey()})
			if err != nil {
				return fmt.Errorf("generate authentication: %w", err)
			}
			files, wires = append(files, groupFiles...), append(wires, groupWires...)
			configGroup, configFresh, configRandom = setting.Group, fresh, true
		}
	case "migration", "model", "scaffold":
		runSqlc = true
	default:
		say(generateUsage)
		return fmt.Errorf("generate: no generator named %q", generator)
	}

	attrs, err := generate.ParseAttrs(attrArgs)
	if runSqlc && generator != "authentication" && err != nil {
		return fmt.Errorf("generate %s: %w", generator, err)
	}

	now := nextStamp(root, time.Now())

	switch generator {
	case "migration":
		f, err := generate.Migration(name, attrs, scheme, now)
		if err != nil {
			return fmt.Errorf("generate migration: %w", err)
		}
		files = []generate.File{f}
	case "model":
		files, err = generate.Model(module, name, attrs, scheme, now)
		if err != nil {
			return fmt.Errorf("generate model: %w", err)
		}
	case "scaffold":
		// admin/products is a model of its own, as in Rails: admin_product,
		// table admin_products, with its controller in the admin namespace
		// under the short name. The existing model's controller is
		// scaffold_controller.
		ns, res, model, err := generate.ParseScaffoldName(positional[0])
		if err != nil {
			return fmt.Errorf("generate scaffold: %w", err)
		}
		if ns != "" {
			name = model
			if _, err := os.Stat(filepath.Join(root, "app", "controllers", ns+"_controllers", "server.go")); err != nil {
				files, wires, err = generate.Namespace(module, ns)
				if err != nil {
					return fmt.Errorf("generate scaffold: %w", err)
				}
			}
			defer say("%12s  model %s is %s.go, table %s; its controller is %s/%s, at /%s/%s\n", "note", model, model, generate.Plural(model), ns+"_controllers", res, ns, res)
		}
		scaffoldFiles, scaffoldWires, err := generate.Scaffold(module, ns, res, name, attrs, scheme, now, only)
		if err != nil {
			return fmt.Errorf("generate scaffold: %w", err)
		}
		files, wires = append(files, scaffoldFiles...), append(wires, scaffoldWires...)
	}

	toWrite := make([]scaffold.File, len(files))
	for i, f := range files {
		// A migration is named by what it does, not by its stamp: a second
		// create_comments is the same migration ("Another
		// migration is already named create_comments"). Point the write at
		// the existing file, so it reports identical or conflict, and --force
		// replaces it in place rather than adding a duplicate.
		if existing := existingMigration(root, f.Path); existing != "" {
			f.Path = existing
		}
		toWrite[i] = scaffold.File{Path: f.Path, Content: []byte(f.Content)}
	}
	report := func(a scaffold.Action) { say("%12s  %s\n", a.Op, a.Path) }

	// Every marker is checked before any file is written: a controller that
	// cannot be registered is not half-written. A marker in a file this run
	// writes (a new namespace's server.go) is checked in that content.
	pending := map[string][]byte{}
	for _, f := range toWrite {
		pending[f.Path] = f.Content
	}
	for _, w := range wires {
		src, ok := pending[w.File]
		if !ok {
			src, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(w.File)))
			if err != nil {
				return err
			}
		}
		if err := markers.Check(src, w.File, w.Marker); err != nil {
			return fmt.Errorf("generate %s: %w", generator, err)
		}
	}

	if _, err := scaffold.Write(root, toWrite, scaffold.Options{Force: *force, Pretend: *pretend, Report: report}); err != nil {
		return fmt.Errorf("generate %s: %w", generator, err)
	}
	if viewsPath != "" {
		report(scaffold.Action{Op: "update", Path: viewsPath + ": " + strings.Join(viewsAdded, ", ")})
		if !*pretend {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(viewsPath)), viewsMerged, 0o644); err != nil {
				return fmt.Errorf("generate %s: %w", generator, err)
			}
		}
	}
	if err := wire(root, wires, pending, *pretend, report); err != nil {
		return fmt.Errorf("generate %s: %w", generator, err)
	}

	// A config group's keys get lines in the development credentials file: a
	// value if one was given, a commented placeholder otherwise, and for the
	// secret an authentication shape reads, a random value, so the app boots
	// as it did. Staging and production are given theirs by hand, and refuse
	// to boot without. New dependencies are tidied in.
	if configGroup != "" || configFlat {
		lines, err := developmentKeys(configFresh, configRandom)
		if err != nil {
			return fmt.Errorf("generate %s: %w", generator, err)
		}
		if err := addDevelopmentKeys(root, configGroup, lines, *pretend, report); err != nil {
			return fmt.Errorf("generate %s: %w", generator, err)
		}
	}
	if generator == "config" {
		read := "s.Config." + generate.Camel(configKeys[0].Name)
		if !configFlat {
			read = "s.Config." + generate.Camel(configGroup) + "." + generate.Camel(configKeys[0].Name)
		}
		say("%12s  read it as %s; set it in production with make credentials ENV=production, or %s\n", "next",
			read, configKeys[0].Env(configGroup))
	}
	if generator == "authentication" {
		if !*pretend && name != "secret" {
			say("%12s  go mod tidy\n", "run")
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = root
			cmd.Stdout = out
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("generate authentication: go mod tidy: %w", err)
			}
		}
		say("%12s  mount the routes it guards in app/controllers/routes.go: s.Posts.SetupRoutes(s.%s(&r.RouterGroup))\n", "next", generate.AuthenticationGroup(name))
	}

	// The migrations are the schema sqlc compiles against, so a new migration
	// changes the generated models immediately, before it is ever applied.
	if runSqlc && !*pretend && !*skipSqlc && hasQueries(root) {
		say("%12s  sqlc generate\n", "run")
		cmd := exec.Command("go", "tool", "sqlc", "generate")
		cmd.Dir = root + "/db"
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("generate %s: sqlc: %w", generator, err)
		}
	}
	return nil
}

// existingMigration finds a migration in the app with the same name as path
// (db/migrate/<stamp>_<name>.sql) under any stamp, and returns its path
// relative to root, or "".
func existingMigration(root, path string) string {
	dir, file := filepath.Split(path)
	if dir != "db/migrate/" {
		return ""
	}
	name := file
	if i := strings.Index(file, "_"); i == 14 { // a 14-digit stamp, then _
		name = file[i+1:]
	}
	matches, _ := filepath.Glob(filepath.Join(root, "db", "migrate", "*_"+name))
	for _, m := range matches {
		rel, err := filepath.Rel(root, m)
		if err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return ""
}

// hasQueries reports whether db/queries holds any query file. sqlc refuses to
// run with none, and an app that has removed its last model has none.
func hasQueries(root string) bool {
	matches, _ := filepath.Glob(filepath.Join(root, "db", "queries", "*.sql"))
	return len(matches) > 0
}

// nextStamp returns now, moved forward a second at a time until no migration
// in the app carries that version. goose refuses duplicate versions, and two
// generators run in the same second would otherwise produce them.
func nextStamp(root string, now time.Time) time.Time {
	for {
		matches, _ := filepath.Glob(filepath.Join(root, "db", "migrate", generate.Stamp(now)+"_*.sql"))
		if len(matches) == 0 {
			return now
		}
		now = now.Add(time.Second)
	}
}

// appModule reads the module path from the app's go.mod.
func appModule(root string) (string, error) {
	b, err := os.ReadFile(root + "/go.mod")
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("generate: no module line in %s/go.mod", root)
}
