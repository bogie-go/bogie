package generate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// File is one file a generator produces, with its path relative to the app.
type File struct {
	Path    string
	Content string
}

var (
	createPattern = regexp.MustCompile(`^create_([a-z][a-z0-9_]*)$`)
	addPattern    = regexp.MustCompile(`^add_[a-z0-9_]+_to_([a-z][a-z0-9_]*)$`)
	removePattern = regexp.MustCompile(`^remove_[a-z0-9_]+_from_([a-z][a-z0-9_]*)$`)
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Stamp is the goose version for a migration generated at t: the same
// 14-digit UTC form Rails uses, so files sort by creation.
func Stamp(t time.Time) string { return t.UTC().Format("20060102150405") }

// Migration writes db/migrate/<stamp>_<name>.sql. As in Rails, the NAME says
// what the migration does: create_<table>, add_<x>_to_<table> and
// remove_<x>_from_<table> get their SQL written; anything else gets an empty
// Up and Down to fill in.
func Migration(name string, attrs []Attr, now time.Time) (File, error) {
	if !namePattern.MatchString(name) {
		return File{}, fmt.Errorf("%q is not a migration name: lowercase letters, digits and underscores", name)
	}
	path := fmt.Sprintf("db/migrate/%s_%s.sql", Stamp(now), name)

	var up, down string
	switch {
	case createPattern.MatchString(name):
		table := createPattern.FindStringSubmatch(name)[1]
		up, down = createTable(table, attrs), "DROP TABLE "+table+";"
	case addPattern.MatchString(name) && len(attrs) > 0:
		table := addPattern.FindStringSubmatch(name)[1]
		up, down = addColumns(table, attrs), dropColumns(table, attrs)
	case removePattern.MatchString(name) && len(attrs) > 0:
		table := removePattern.FindStringSubmatch(name)[1]
		up, down = dropColumns(table, attrs), addColumns(table, attrs)
	default:
		up = "-- TODO: the change. Every statement here must be undone below."
		down = "-- TODO: undo the change above, exactly."
	}

	content := fmt.Sprintf("-- +goose Up\n%s\n\n-- +goose Down\n%s\n", up, down)
	return File{Path: path, Content: content}, nil
}

func createTable(table string, attrs []Attr) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", table)
	fmt.Fprintf(&b, "    %-10s %-11s %s,\n", "id", "uuid", "PRIMARY KEY DEFAULT gen_random_uuid()")
	for _, a := range attrs {
		fmt.Fprintf(&b, "%s,\n", a.column())
	}
	fmt.Fprintf(&b, "    %-10s %-11s %s,\n", "created_at", "timestamptz", "NOT NULL DEFAULT now()")
	fmt.Fprintf(&b, "    %-10s %-11s %s\n", "updated_at", "timestamptz", "NOT NULL DEFAULT now()")
	b.WriteString(");")
	for _, a := range attrs {
		if s := a.indexStatement(table); s != "" {
			b.WriteString("\n" + s)
		}
	}
	return b.String()
}

func addColumns(table string, attrs []Attr) string {
	var lines []string
	for _, a := range attrs {
		lines = append(lines, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", table, strings.Join(strings.Fields(a.column()), " ")))
		if s := a.indexStatement(table); s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

func dropColumns(table string, attrs []Attr) string {
	var lines []string
	for _, a := range attrs {
		lines = append(lines, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", table, a.Name))
	}
	return strings.Join(lines, "\n")
}
