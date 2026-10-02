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

const generateUsage = `Usage: bogie generate GENERATOR NAME [attributes] [--pretend] [--force] [--skip-sqlc]
       bogie g ...

Generators:
  migration NAME [field:type ...]   db/migrate/<stamp>_NAME.sql
                                    create_<table>, add_<x>_to_<table> and
                                    remove_<x>_from_<table> write their SQL
  model NAME [field:type ...]       the migration, db/queries/<table>.sql,
                                    app/domain/<name>.go and the store's
                                    app/models/<table>.go; then runs sqlc
  controller NAME [action ...]      app/controllers/<name>_controller/ with
                                    one file per action, REGISTERED: a field,
                                    a wire line and a mount above each marker
  service NAME                      app/services/<name>/, ctx first, no HTTP

Attributes are field:type, with :index or :uniq after the type. Types:
  string text integer bigint boolean datetime uuid jsonb references
A references field names the other model: post:references is post_id.

  bogie g model comment body:text post:references
  bogie g migration add_slug_to_posts slug:string:uniq
  bogie g controller comments index show create
  bogie g service publish_post
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
	// Post, BlogPost and AddSlugToPosts are what Rails fingers type; every
	// generator works in snake_case.
	name, attrArgs := generate.Snake(positional[0]), positional[1:]

	module, err := appModule(root)
	if err != nil {
		return err
	}
	var files []generate.File
	var wires []generate.Wire
	runSqlc := false
	switch generator {
	case "controller":
		files, wires, err = generate.Controller(module, name, attrArgs)
		if err != nil {
			return fmt.Errorf("generate controller: %w", err)
		}
	case "service":
		files, err = generate.Service(module, name)
		if err != nil {
			return fmt.Errorf("generate service: %w", err)
		}
	case "migration", "model":
		runSqlc = true
	default:
		say(generateUsage)
		return fmt.Errorf("generate: no generator named %q", generator)
	}

	attrs, err := generate.ParseAttrs(attrArgs)
	if runSqlc && err != nil {
		return fmt.Errorf("generate %s: %w", generator, err)
	}

	now := nextStamp(root, time.Now())

	switch generator {
	case "migration":
		f, err := generate.Migration(name, attrs, now)
		if err != nil {
			return fmt.Errorf("generate migration: %w", err)
		}
		files = []generate.File{f}
	case "model":
		files, err = generate.Model(module, name, attrs, now)
		if err != nil {
			return fmt.Errorf("generate model: %w", err)
		}
	}

	toWrite := make([]scaffold.File, len(files))
	for i, f := range files {
		// A migration is named by what it does, not by its stamp: a second
		// create_comments is the same migration, as Rails says ("Another
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
	// cannot be registered is not half-written.
	for _, w := range wires {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(w.File)))
		if err != nil {
			return err
		}
		if err := markers.Check(src, w.File, w.Marker); err != nil {
			return fmt.Errorf("generate %s: %w", generator, err)
		}
	}

	if _, err := scaffold.Write(root, toWrite, scaffold.Options{Force: *force, Pretend: *pretend, Report: report}); err != nil {
		return fmt.Errorf("generate %s: %w", generator, err)
	}
	if err := wire(root, wires, *pretend, report); err != nil {
		return fmt.Errorf("generate %s: %w", generator, err)
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
// generators run in the same second would otherwise produce them, as Rails's
// next_migration_number guards against too.
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
