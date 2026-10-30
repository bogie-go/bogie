package generate

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"strings"
)

// StructField is one field of a Go struct: its name and its type as written.
type StructField struct {
	Name string
	Type string
}

// owned is a field every model has and bogie writes: the id and the two
// timestamps. They are never a column to add or remove.
func (f StructField) owned() bool {
	return f.Name == "ID" || f.Name == "CreatedAt" || f.Name == "UpdatedAt"
}

// StructFields lists the fields of the struct typeName declared in src, in
// order. A line that declares several names gives one field for each.
func StructFields(src []byte, typeName string) ([]StructField, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return nil, err
	}
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range g.Specs {
			t, ok := sp.(*ast.TypeSpec)
			if !ok || t.Name.Name != typeName {
				continue
			}
			st, ok := t.Type.(*ast.StructType)
			if !ok {
				continue
			}
			var out []StructField
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					out = append(out, StructField{Name: n.Name, Type: types.ExprString(f.Type)})
				}
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("no struct %s in the model's file", typeName)
}

// fieldAttr is the attribute a model field was made from: the type g model
// would have been given to write it. A field whose type is not one g model
// writes, or whose name cannot be a column name, is not a column bogie
// manages, and ok is false.
func fieldAttr(f StructField, scheme IDScheme) (a Attr, ok bool) {
	col := fieldColumn(f.Name)
	typ, null := strings.CutPrefix(f.Type, "*")
	a = Attr{Name: col, Null: null}
	switch typ {
	case "string":
		a.Type = "string"
		if scheme == IDUUID && strings.HasSuffix(col, "_id") && !null {
			a.Type = "references"
		}
	case "int32":
		a.Type = "integer"
	case "int64":
		a.Type = "bigint"
		if scheme == IDBigint && strings.HasSuffix(col, "_id") {
			a.Type = "references"
		}
	case "bool":
		a.Type = "boolean"
	case "time.Time":
		a.Type = "datetime"
	case "json.RawMessage":
		if null {
			return Attr{}, false
		}
		a.Type = "jsonb"
	default:
		return Attr{}, false
	}
	return a, a.goName() == f.Name
}

// rowAttr is the attribute for a column, read from the field sqlc generated
// for it. sqlc gives a nullable string, number or bool a pointer, but a
// datetime is the same pgtype whether or not it may be NULL, so the
// migrations are asked: schema says which columns may be.
func rowAttr(f StructField, schema map[string]Column, scheme IDScheme) (Attr, error) {
	col := fieldColumn(f.Name)
	a := Attr{Name: col}
	typ, null := strings.CutPrefix(f.Type, "*")
	a.Null = null
	switch typ {
	case "string":
		a.Type = "string"
	case "int32":
		a.Type = "integer"
	case "int64":
		a.Type = "bigint"
		if scheme == IDBigint && strings.HasSuffix(col, "_id") {
			a.Type = "references"
		}
	case "bool":
		a.Type = "boolean"
	case "pgtype.Timestamptz":
		a.Type = "datetime"
		a.Null = schema[col].Null
	case "pgtype.UUID":
		a.Type = "uuid"
		if scheme == IDUUID && strings.HasSuffix(col, "_id") {
			a.Type = "references"
		}
		if schema[col].Null {
			return Attr{}, fmt.Errorf("column %s is a nullable uuid, which bogie does not write; add it to the model by hand", col)
		}
	case "[]byte":
		a.Type = "jsonb"
	default:
		return Attr{}, fmt.Errorf("column %s has the type %s, which bogie does not write; add it to the model by hand", col, f.Type)
	}
	if a.goName() != f.Name {
		return Attr{}, fmt.Errorf("column %s: cannot tell which column %s is", col, f.Name)
	}
	a.Unique = schema[col].Unique
	return a, nil
}

// SyncPlan is what differs between a model and its table.
type SyncPlan struct {
	Old, New       []Attr   // the model's columns before the sync, and after it
	Added, Removed []Attr   // columns the table has and the model lacks, and the reverse
	Notes          []string // differences left alone, for the report
}

// InSync reports whether the model already has every column of its table and
// no field without one.
func (p SyncPlan) InSync() bool { return len(p.Added) == 0 && len(p.Removed) == 0 }

// PlanSync compares a model's fields with the fields of the row type sqlc
// generated for its table. A model field bogie did not write (a slice, a
// struct, a type it has no column for) is not a column and is left alone. So
// is one that bogie could have written but that never had a column, a count
// or a joined name the reader added: it is removed only when the migrations
// dropped a column of that name. Order is kept: the model's own, then the new
// columns as the table has them.
func PlanSync(model, row []StructField, schema map[string]Column, scheme IDScheme) (SyncPlan, error) {
	var p SyncPlan
	inRow := map[string]StructField{}
	for _, f := range row {
		if !f.owned() {
			inRow[f.Name] = f
		}
	}
	inModel := map[string]bool{}
	for _, f := range model {
		inModel[f.Name] = true
		if f.owned() {
			continue
		}
		a, ok := fieldAttr(f, scheme)
		if !ok {
			continue
		}
		rf, there := inRow[f.Name]
		if a.Type == "string" && (rf.Type == "pgtype.UUID" || schema[a.Name].UUID) {
			a.Type = "uuid"
		}
		if !there && !schema[a.Name].Gone {
			// Not a column, and never was: a count or a joined name the
			// reader added. It is theirs.
			continue
		}
		// The model cannot say it, but the store handles a unique violation.
		a.Unique = schema[a.Name].Unique
		p.Old = append(p.Old, a)
		if !there {
			p.Removed = append(p.Removed, a)
			continue
		}
		p.New = append(p.New, a)
		if ra, err := rowAttr(rf, schema, scheme); err == nil && ra.domainType(scheme) != f.Type {
			p.Notes = append(p.Notes, fmt.Sprintf("%s: the column is %s now and the model has %s; left as it is", ra.Name, ra.domainType(scheme), f.Type))
		}
	}
	for _, f := range row {
		if f.owned() || inModel[f.Name] {
			continue
		}
		a, err := rowAttr(f, schema, scheme)
		if err != nil {
			return SyncPlan{}, err
		}
		p.Added = append(p.Added, a)
		p.New = append(p.New, a)
	}
	return p, nil
}

// SyncFiles are the files that follow from a model's columns, written as
// `bogie g model` would write them for attrs: the queries, the store and the
// factory. The sync merges them into the app's own copies, from what was
// written for the old columns, so what has been edited by hand survives.
func SyncFiles(module, name string, attrs []Attr, scheme IDScheme) (queriesSQL, storeGo, factoryGo string) {
	table := Plural(name)
	return queries(name, table, attrs), formatted(store(module, name, table, attrs, scheme)), factory(module, name, attrs, scheme)
}

// PatchModel adds the planned columns to the model's struct and removes the
// dropped ones, and touches nothing else in the file: Validate, constants and
// comments stay. A removed field takes the comment lines directly above it.
func PatchModel(src []byte, name string, p SyncPlan, scheme IDScheme) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	typ := Camel(name)
	start := -1
	for i, l := range lines {
		if l == "type "+typ+" struct {" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("no `type %s struct {` in the model's file", typ)
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("the struct %s is not closed", typ)
	}

	body := append([]string(nil), lines[start+1:end]...)
	for _, a := range p.Removed {
		at := -1
		for i, l := range body {
			fields := strings.Fields(l)
			if len(fields) > 0 && strings.TrimSuffix(fields[0], ",") == a.goName() {
				if strings.HasSuffix(fields[0], ",") {
					return nil, fmt.Errorf("%s shares a line with other fields; remove it by hand", a.goName())
				}
				at = i
				break
			}
		}
		if at < 0 {
			return nil, fmt.Errorf("no field %s in the struct", a.goName())
		}
		from := at
		for from > 0 && strings.HasPrefix(strings.TrimSpace(body[from-1]), "//") {
			from--
		}
		body = append(body[:from], body[at+1:]...)
	}

	// New fields go above CreatedAt, and above the comment lines that belong
	// to it.
	at := len(body)
	for i, l := range body {
		if fields := strings.Fields(l); len(fields) > 0 && fields[0] == "CreatedAt" {
			at = i
			for at > 0 && strings.HasPrefix(strings.TrimSpace(body[at-1]), "//") {
				at--
			}
			break
		}
	}
	var added []string
	for _, a := range p.Added {
		added = append(added, "\t"+a.goName()+" "+a.domainType(scheme))
	}
	body = append(body[:at], append(added, body[at:]...)...)

	out := append(append(append([]string(nil), lines[:start+1]...), body...), lines[end:]...)
	text := strings.Join(out, "\n")
	text, err := fixImport(text, "encoding/json", "json.")
	if err != nil {
		return nil, err
	}
	formattedSrc, err := format.Source([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("the patched model does not parse: %w", err)
	}
	return formattedSrc, nil
}

// fixImport makes the import block hold path exactly when the file uses it
// (some line has the prefix use, such as "json."). A file with no import
// block, or a block it cannot find, is an error only when an import is
// missing.
func fixImport(src, path, use string) (string, error) {
	line := "\t\"" + path + "\""
	has := strings.Contains(src, line+"\n")
	// The import lines themselves never contain the prefix: "encoding/json"
	// has no "json." in it.
	uses := strings.Contains(src, use)
	switch {
	case uses && !has:
		i := strings.Index(src, "import (\n")
		if i < 0 {
			return "", fmt.Errorf("the model's file has no import block to add %q to; add the import by hand", path)
		}
		i += len("import (\n")
		return src[:i] + line + "\n" + src[i:], nil
	case !uses && has:
		return strings.Replace(src, line+"\n", "", 1), nil
	}
	return src, nil
}

var (
	createTableRe = regexp.MustCompile(`^create table (?:if not exists )?(?:\w+\.)?"?(\w+)"? ?\((.*)\)$`)
	alterTableRe  = regexp.MustCompile(`^alter table (?:only )?(?:if exists )?(?:\w+\.)?"?(\w+)"? (.*)$`)
	uniqueIndexRe = regexp.MustCompile(`^create unique index (?:if not exists )?\S+ on (?:only )?(?:\w+\.)?"?(\w+)"?(?: using \w+)? ?\((.*)\)`)
	uniqueWordRe  = regexp.MustCompile(`\bunique\b`)
	spaceRe       = regexp.MustCompile(`\s+`)
)

// Column is what sqlc's row type does not show of a column.
type Column struct {
	Null   bool // it may be NULL: a datetime is the same pgtype either way
	Unique bool // a UNIQUE column, which the store answers as "already taken"
	// Gone is a column a migration dropped, or renamed away. A model field
	// with no column is only removed when its column is known to be gone: a
	// field the reader added for a computed value (a count, a joined name) has
	// no column and never did.
	Gone bool
	// UUID is a uuid column. A Go string is a string and a uuid alike, so the
	// model cannot say; sqlc can for a column that is there, and the
	// migration for one that is gone.
	UUID bool
}

// Columns reads the Up part of each migration, in order, and says what it
// makes of each column of table, and which it dropped. It follows the statements a migration is
// made of: CREATE TABLE and ALTER TABLE with ADD, DROP and RENAME COLUMN and
// ALTER COLUMN SET or DROP NOT NULL, and CREATE UNIQUE INDEX on one column; a
// column it never saw is not in the map.
func Columns(migrations []string, table string) map[string]Column {
	cols := map[string]Column{}
	for _, m := range migrations {
		for _, stmt := range upStatements(m) {
			if c := createTableRe.FindStringSubmatch(stmt); c != nil {
				if c[1] == table {
					for _, def := range splitTop(c[2]) {
						addColumn(cols, def)
					}
				}
				continue
			}
			if u := uniqueIndexRe.FindStringSubmatch(stmt); u != nil {
				if c, ok := cols[strings.Trim(u[2], `" `)]; ok && !c.Gone && u[1] == table && !strings.Contains(u[2], ",") {
					c.Unique = true
					cols[strings.Trim(u[2], `" `)] = c
				}
				continue
			}
			a := alterTableRe.FindStringSubmatch(stmt)
			if a == nil || a[1] != table {
				continue
			}
			for _, action := range splitTop(a[2]) {
				alterColumn(cols, action)
			}
		}
	}
	return cols
}

// upStatements is the SQL statements of m before its Down marker, lower-cased
// with comments removed and white space collapsed.
func upStatements(m string) []string {
	if i := strings.Index(m, "-- +goose Down"); i >= 0 {
		m = m[:i]
	}
	var kept []string
	for _, l := range strings.Split(m, "\n") {
		if i := strings.Index(l, "--"); i >= 0 {
			l = l[:i]
		}
		kept = append(kept, l)
	}
	var out []string
	for _, s := range strings.Split(strings.Join(kept, "\n"), ";") {
		s = strings.TrimSpace(spaceRe.ReplaceAllString(strings.ToLower(s), " "))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// splitTop splits s at the commas outside parentheses.
func splitTop(s string) []string {
	var out []string
	depth, from := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[from:i]))
				from = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[from:]))
}

// addColumn records one column definition, "name type [constraints]", unless
// it is a table constraint.
func addColumn(cols map[string]Column, def string) {
	name, rest, _ := strings.Cut(def, " ")
	name = strings.Trim(name, `"`)
	switch name {
	case "constraint", "primary", "foreign", "unique", "check", "like", "exclude", "":
		return
	}
	cols[name] = Column{
		Null:   !strings.Contains(rest, "not null") && !strings.Contains(rest, "primary key"),
		Unique: uniqueWordRe.MatchString(rest),
		UUID:   rest == "uuid" || strings.HasPrefix(rest, "uuid "),
	}
}

// alterColumn applies one ALTER TABLE action to cols.
func alterColumn(cols map[string]Column, action string) {
	words := strings.Fields(action)
	if len(words) < 2 {
		return
	}
	trim := func(s string) string { return strings.Trim(s, `"`) }
	switch words[0] {
	case "add":
		rest := strings.TrimPrefix(action, "add ")
		rest = strings.TrimPrefix(rest, "column ")
		rest = strings.TrimPrefix(rest, "if not exists ")
		addColumn(cols, rest)
	case "drop":
		if words[1] == "constraint" {
			return
		}
		name := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(action, "drop "), "column "), "if exists ")
		name, _, _ = strings.Cut(name, " ")
		c := cols[trim(name)]
		c.Gone = true
		cols[trim(name)] = c
	case "alter":
		rest := strings.TrimPrefix(strings.TrimPrefix(action, "alter "), "column ")
		name, change, _ := strings.Cut(rest, " ")
		name = trim(name)
		c, ok := cols[name]
		if !ok || c.Gone {
			return
		}
		switch {
		case strings.HasPrefix(change, "set not null"):
			c.Null = false
		case strings.HasPrefix(change, "drop not null"):
			c.Null = true
		}
		cols[name] = c
	case "rename":
		rest := strings.TrimPrefix(strings.TrimPrefix(action, "rename "), "column ")
		from, to, ok := strings.Cut(rest, " to ")
		if !ok {
			return
		}
		from, to = trim(from), trim(strings.TrimSpace(to))
		if v, ok := cols[from]; ok && !v.Gone {
			cols[to] = v
			v.Gone = true
			cols[from] = v
		}
	}
}
