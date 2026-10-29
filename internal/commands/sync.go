package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bogie-go/bogie/internal/generate"
)

const modelSyncUsage = `Usage: bogie model:sync [TABLE]

Brings the Go side of each model in line with its table. The migrations are
the schema: sqlc compiles them, and model:sync reads the columns from what
sqlc makes of them, so a migration written by hand is as good as a generated
one. For each column the model lacks it adds the field, and for each field
whose column is gone it removes the field. The queries, the store and the
factory are rewritten from the new columns and merged with your copy, the way
app:update merges, so edits you made by hand stay; where a hand edit and the
change touch the same lines the file gets conflict markers to resolve.

Never touched: Validate and the rest of the model, the *_extra files, the
views, the controllers and the specs. The report says so for each.

Needs a clean git tree, so that git can undo it.

  bogie model:sync            every model that bogie wrote
  bogie model:sync users      one
`

// ModelSync handles `bogie model:sync`.
func ModelSync(args []string, out io.Writer) error {
	root, err := appRoot(".")
	if err != nil {
		return err
	}
	return modelSyncIn(root, args, out)
}

// managedHeader is in every store file `bogie g model` writes. A model
// without it was written by hand, and the sync leaves it alone.
const managedHeader = "Written by `bogie g model`"

func modelSyncIn(root string, args []string, out io.Writer) error {
	say := func(op, format string, a ...any) {
		_, _ = fmt.Fprintf(out, "%12s  %s\n", op, fmt.Sprintf(format, a...))
	}
	var target string
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			_, _ = fmt.Fprint(out, modelSyncUsage)
			return nil
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("model:sync: no option %s; it takes a table name or nothing", a)
		case target != "":
			return fmt.Errorf("model:sync: one table at a time, or none for every model")
		default:
			target = a
		}
	}

	settings, err := readSettings(root)
	if err != nil {
		return fmt.Errorf("model:sync: %w", err)
	}
	if dirty, err := gitDirty(root); err == nil && dirty {
		return errors.New("model:sync: the git tree has uncommitted changes; commit or stash them first, so the sync can be undone with git")
	}
	names, err := syncModels(root, target)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		say("ok", "no model written by bogie to sync")
		return nil
	}

	rowsSrc, err := sqlcRows(root)
	if err != nil {
		return fmt.Errorf("model:sync: %w", err)
	}
	migrations := migrationSources(root)

	var removed []removedField
	changed, failed := 0, 0
	for _, name := range names {
		table := generate.Plural(name)
		typ := generate.Camel(name)
		modelPath := "app/models/" + name + ".go"
		modelSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(modelPath)))
		if err != nil {
			return fmt.Errorf("model:sync: %w", err)
		}
		modelFields, err := generate.StructFields(modelSrc, typ)
		if err != nil {
			say("skip", "%s: %v", name, err)
			failed++
			continue
		}
		rowFields, err := generate.StructFields(rowsSrc, typ)
		if err != nil {
			say("skip", "%s: no table %s in the schema", name, table)
			failed++
			continue
		}
		plan, err := generate.PlanSync(modelFields, rowFields, generate.Columns(migrations, table), settings.IDScheme)
		if err != nil {
			say("skip", "%s: %v", name, err)
			failed++
			continue
		}
		for _, n := range plan.Notes {
			say("note", "%s: %s", name, n)
		}
		if plan.InSync() {
			continue
		}
		changed++
		for _, a := range plan.Added {
			say("add", "%s.%s (%s)", table, a.Name, columnWord(a))
		}
		for _, a := range plan.Removed {
			say("remove", "%s.%s", table, a.Name)
			removed = append(removed, removedField{table: table, typ: typ, field: a.Name, goName: generate.Camel(a.Name)})
		}

		patched, err := generate.PatchModel(modelSrc, name, plan, settings.IDScheme)
		if err != nil {
			say("skip", "%s: %v", modelPath, err)
			failed++
			continue
		}
		write := func(path string, content []byte) error {
			return os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), content, 0o644)
		}
		if err := write(modelPath, patched); err != nil {
			return fmt.Errorf("model:sync: %w", err)
		}
		say("update", "%s", modelPath)

		oldQ, oldS, oldF := generate.SyncFiles(settings.Module, name, plan.Old, settings.IDScheme)
		newQ, newS, newF := generate.SyncFiles(settings.Module, name, plan.New, settings.IDScheme)
		for _, f := range []struct{ path, base, theirs string }{
			{"db/queries/" + table + ".sql", oldQ, newQ},
			{"db/adapter/" + table + ".go", oldS, newS},
			{"test/factories/" + table + ".go", oldF, newF},
		} {
			op, err := syncFile(root, f.path, f.base, f.theirs)
			if err != nil {
				return fmt.Errorf("model:sync: %w", err)
			}
			if op == "conflict" {
				failed++
			}
			if op != "" {
				say(op, "%s", f.path)
			}
		}
		if generate.NeedsNullableHelpers(plan.Added) && !adapterHas(root, "func timestamptzToTimePtr") {
			if err := write(generate.NullableHelpersPath, []byte(generate.NullableHelpers)); err != nil {
				return fmt.Errorf("model:sync: %w", err)
			}
			say("create", "%s", generate.NullableHelpersPath)
		}
		for _, left := range []struct{ path, why string }{
			{"app/views/" + table + ".go", "the JSON the API sends and takes is yours; add the field there if it should carry it"},
			{"app/controllers/" + table + "_controller.go", "what a request may set is yours"},
			{"app/models/" + name + "_test.go", "its specs do not list columns"},
			{"db/adapter/" + table + "_test.go", "its specs do not list columns"},
		} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(left.path))); err == nil {
				say("left", "%s (%s)", left.path, left.why)
			}
		}
	}

	if changed == 0 && failed == 0 {
		say("ok", "every model matches its table (%d)", len(names))
		return nil
	}

	for _, r := range removed {
		for _, hit := range findUses(root, r.typ, r.goName) {
			say("still", "%s mentions %s, the field removed with %s.%s", hit, r.goName, r.table, r.field)
		}
	}
	if changed > 0 && hasQueries(root) {
		say("run", "sqlc generate")
		cmd := exec.Command("go", "tool", "sqlc", "generate")
		cmd.Dir = filepath.Join(root, "db")
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("model:sync: sqlc: %w (a query of yours, in an _extra.sql file, may name a column that is gone)", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("model:sync: %d to look at (see skip and conflict above)", failed)
	}
	if changed > 0 {
		say("next", "go build ./... and bogie test")
	}
	return nil
}

type removedField struct{ table, typ, field, goName string }

// columnWord names an added column for the report.
func columnWord(a generate.Attr) string {
	if a.Null {
		return a.Type + ", may be NULL"
	}
	return a.Type
}

// syncFile merges the generated file for the new columns into the app's own
// copy, using the file generated for the old columns as the common ancestor.
// An untouched copy is simply replaced. It returns the report word, or "" when
// the file does not change.
func syncFile(root, path, base, theirs string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(path))
	mine, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return "left", nil
	}
	if err != nil {
		return "", err
	}
	if bytes.Equal(mine, []byte(base)) {
		if base == theirs {
			return "", nil
		}
		return "update", os.WriteFile(full, []byte(theirs), 0o644)
	}
	merged, conflicts, err := mergeFile(mine, []byte(base), []byte(theirs), "before", "model:sync")
	if err != nil {
		return "", err
	}
	if bytes.Equal(merged, mine) {
		return "", nil
	}
	if err := os.WriteFile(full, merged, 0o644); err != nil {
		return "", err
	}
	if conflicts > 0 {
		return "conflict", nil
	}
	return "merge", nil
}

// syncModels is the models the sync covers: each app/models/NAME.go whose
// table has the queries and the store `bogie g model` writes. With a target,
// only that one, named by table or by model.
func syncModels(root, target string) ([]string, error) {
	managed := func(name string) bool {
		table := generate.Plural(name)
		if _, err := os.Stat(filepath.Join(root, "db", "queries", table+".sql")); err != nil {
			return false
		}
		adapter, err := os.ReadFile(filepath.Join(root, "db", "adapter", table+".go"))
		return err == nil && strings.Contains(string(adapter), managedHeader)
	}
	if target != "" {
		for _, name := range []string{target, generate.Singular(target)} {
			if _, err := os.Stat(filepath.Join(root, "app", "models", name+".go")); err == nil {
				if !managed(name) {
					return nil, fmt.Errorf("model:sync: %s was not written by `bogie g model` (no queries and store with its header); it is yours to keep in line", name)
				}
				return []string{name}, nil
			}
		}
		return nil, fmt.Errorf("model:sync: no model %q in app/models", target)
	}
	files, _ := filepath.Glob(filepath.Join(root, "app", "models", "*.go"))
	sort.Strings(files)
	var names []string
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".go")
		if strings.HasSuffix(base, "_test") || !managed(base) {
			continue
		}
		names = append(names, base)
	}
	return names, nil
}

// sqlcRows is the db/sqlc/models.go that sqlc makes from the migrations alone.
// It is generated into a scratch directory with a query of no meaning, so a
// query that names a column that is gone cannot stop the sync from learning
// that it is gone.
func sqlcRows(root string) ([]byte, error) {
	config, err := os.ReadFile(filepath.Join(root, "db", "sqlc.yaml"))
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "bogie-sync-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for path, content := range map[string][]byte{
		"sqlc.yaml":         config,
		"queries/probe.sql": []byte("-- name: Probe :one\nSELECT 1;\n"),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, path), content, 0o644); err != nil {
			return nil, err
		}
	}
	migrations, _ := filepath.Glob(filepath.Join(root, "db", "migrate", "*.sql"))
	if err := os.MkdirAll(filepath.Join(dir, "migrate"), 0o755); err != nil {
		return nil, err
	}
	for _, m := range migrations {
		b, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "migrate", filepath.Base(m)), b, 0o644); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command("go", "tool", "sqlc", "generate", "-f", filepath.Join(dir, "sqlc.yaml"))
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sqlc could not compile the migrations: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return os.ReadFile(filepath.Join(dir, "sqlc", "models.go"))
}

// migrationSources is the text of each migration, in the order goose runs them.
func migrationSources(root string) []string {
	files, _ := filepath.Glob(filepath.Join(root, "db", "migrate", "*.sql"))
	sort.Strings(files)
	var out []string
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			out = append(out, string(b))
		}
	}
	return out
}

// adapterHas reports whether any file in db/adapter contains text.
func adapterHas(root, text string) bool {
	files, _ := filepath.Glob(filepath.Join(root, "db", "adapter", "*.go"))
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), text) {
			return true
		}
	}
	return false
}

// findUses lists path:line for each Go line that reads or sets a field by
// name, in the files that mention the model's type (typ), outside the code
// sqlc generates. Without types it cannot tell one model's Name from
// another's, so it narrows by file; the compiler has the last word. A removed
// field that code still names is a compile error to fix by hand.
func findUses(root, typ, field string) []string {
	re := regexp.MustCompile(`\.` + field + `\b|\b` + field + `:`)
	mentions := regexp.MustCompile(`\b` + typ + `\b`)
	var hits []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "sqlc", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !mentions.Match(b) {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				hits = append(hits, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), i+1))
			}
		}
		return nil
	})
	return hits
}
