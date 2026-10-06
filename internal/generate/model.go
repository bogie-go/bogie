package generate

import (
	"fmt"
	"strings"
	"time"
)

// Model writes what `bogie generate model post title:string body:text`
// produces: the create_<table> migration, a starter db/queries/<table>.sql,
// the domain type, and the hand-written half of the store that converts sqlc's
// row type to it. Running sqlc afterwards is the caller's job; the files here
// are written to match what it will generate.
func Model(module, name string, attrs []Attr, now time.Time) ([]File, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("%q is not a model name: lowercase letters, digits and underscores, singular", name)
	}
	if looksPlural(name) {
		return nil, fmt.Errorf("%q looks plural; name the model in the singular (%s)", name, Singular(name))
	}
	table := Plural(name)
	for _, a := range attrs {
		switch a.Name {
		case "id", "created_at", "updated_at":
			return nil, fmt.Errorf("%s is added to every model; leave it out", a.Name)
		}
	}

	migration, err := Migration("create_"+table, attrs, now)
	if err != nil {
		return nil, err
	}
	return []File{
		migration,
		{Path: "db/queries/" + table + ".sql", Content: queries(name, table, attrs)},
		{Path: "app/domain/" + name + ".go", Content: domainType(name, attrs)},
		{Path: "app/models/" + table + ".go", Content: store(module, name, table, attrs)},
		{Path: "app/models/" + table + "_test.go", Content: storeTest(module, name, table, attrs)},
	}, nil
}

// storeTest is the model's test, against the real test database as every
// store test is: the not-found cases, which hold for any set of attributes,
// and a place to put the round trip once the model has values worth
// inserting.
func storeTest(module, name, table string, attrs []Attr) string {
	typ := Camel(name)
	// Every uuid-typed attribute gets a well-formed value, so the store reaches
	// the query and answers not-found rather than refusing a malformed id.
	literal := "ID: id"
	for _, a := range attrs {
		if kinds[a.Type].sqlcType == "pgtype.UUID" {
			literal += fmt.Sprintf(", %s: %q", a.goName(), "00000000-0000-4000-8000-000000000000")
		}
	}
	return fmt.Sprintf(`package models

import (
	"context"
	"errors"
	"testing"

	%[3]q
)

// Whatever the id looks like, a %[2]s that is not there is ErrNotFound, and a
// caller cannot tell a malformed id from a well-formed one that names nothing.
func TestUnknown%[4]sAreNotFound(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for _, id := range []string{"nope", "", "00000000-0000-4000-8000-000000000000"} {
		if _, err := store.Get%[1]s(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Get%[1]s(%%q): err = %%v, want ErrNotFound", id, err)
		}
		if err := store.Delete%[1]s(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Delete%[1]s(%%q): err = %%v, want ErrNotFound", id, err)
		}
		if _, err := store.Update%[1]s(ctx, domain.%[1]s{%[5]s}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Update%[1]s(%%q): err = %%v, want ErrNotFound", id, err)
		}
	}
}

// TODO: a round trip. Create a %[2]s with real values, Get it, List it,
// Update it, Delete it; see posts_test.go for the shape.
`, typ, name, module+"/app/domain", Camel(table), literal)
}

func queries(name, table string, attrs []Attr) string {
	cols := make([]string, len(attrs))
	params := make([]string, len(attrs))
	sets := make([]string, len(attrs))
	for i, a := range attrs {
		cols[i] = a.Name
		params[i] = fmt.Sprintf("$%d", i+1)
		sets[i] = fmt.Sprintf("%s = $%d", a.Name, i+2)
	}
	one := Camel(name)
	many := Camel(table)
	var b strings.Builder
	fmt.Fprintf(&b, "-- name: Get%s :one\nSELECT * FROM %s WHERE id = $1;\n\n", one, table)
	fmt.Fprintf(&b, "-- name: List%s :many\nSELECT * FROM %s ORDER BY created_at DESC, id DESC LIMIT $1;\n\n", many, table)
	if len(attrs) == 0 {
		fmt.Fprintf(&b, "-- name: Create%s :one\nINSERT INTO %s DEFAULT VALUES RETURNING *;\n\n", one, table)
		fmt.Fprintf(&b, "-- name: Update%s :one\nUPDATE %s SET updated_at = now() WHERE id = $1 RETURNING *;\n\n", one, table)
	} else {
		fmt.Fprintf(&b, "-- name: Create%s :one\nINSERT INTO %s (%s) VALUES (%s) RETURNING *;\n\n", one, table, strings.Join(cols, ", "), strings.Join(params, ", "))
		fmt.Fprintf(&b, "-- name: Update%s :one\nUPDATE %s SET %s, updated_at = now() WHERE id = $1 RETURNING *;\n\n", one, table, strings.Join(sets, ", "))
	}
	fmt.Fprintf(&b, "-- name: Delete%s :execrows\nDELETE FROM %s WHERE id = $1;\n", one, table)
	return b.String()
}

func domainType(name string, attrs []Attr) string {
	typ := Camel(name)
	imports := map[string]bool{"time": true}
	for _, a := range attrs {
		for _, imp := range kinds[a.Type].imports {
			imports[imp] = true
		}
	}
	var b strings.Builder
	b.WriteString("package domain\n\nimport (\n")
	for _, imp := range sortedKeys(imports) {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// %s is a %s. Imports nothing third-party: ids are strings here and\n", typ, name)
	b.WriteString("// become uuid at the store boundary.\n")
	fmt.Fprintf(&b, "type %s struct {\n", typ)
	width := len("CreatedAt")
	for _, a := range attrs {
		if n := len(a.goName()); n > width {
			width = n
		}
	}
	fmt.Fprintf(&b, "\t%-*s string\n", width, "ID")
	for _, a := range attrs {
		fmt.Fprintf(&b, "\t%-*s %s\n", width, a.goName(), a.domainType())
	}
	fmt.Fprintf(&b, "\t%-*s time.Time\n", width, "CreatedAt")
	fmt.Fprintf(&b, "\t%-*s time.Time\n", width, "UpdatedAt")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// Validate is the model's validations, as Rails would put them. Wrap every\n")
	fmt.Fprintf(&b, "// failure in ErrInvalid so a controller can answer 422 without knowing the\n")
	fmt.Fprintf(&b, "// rule:\n//\n//\treturn fmt.Errorf(\"%%w: title is required\", ErrInvalid)\n")
	fmt.Fprintf(&b, "func (%s %s) Validate() error {\n\treturn nil\n}\n", name[:1], typ)
	return b.String()
}

func store(module, name, table string, attrs []Attr) string {
	typ := Camel(name)
	recv := paramName(name, "s")
	var b strings.Builder
	b.WriteString("package models\n\n")
	b.WriteString("import (\n\t\"context\"\n\t\"errors\"\n\t\"fmt\"\n\n\t\"github.com/jackc/pgx/v5\"\n")
	if needsPgtype(attrs) {
		b.WriteString("\t\"github.com/jackc/pgx/v5/pgtype\"\n")
	}
	fmt.Fprintf(&b, "\n\t%q\n)\n\n", module+"/app/domain")

	fmt.Fprintf(&b, "// %s, as the domain sees them. Each method converts domain.%s to and from\n", Camel(table), typ)
	b.WriteString("// the row type sqlc generated, and maps pgx's \"no rows\" to domain.ErrNotFound\n")
	b.WriteString("// so callers never import pgx to ask a question.\n\n")

	// params for Create and Update
	createParams := func(src string) string {
		if len(attrs) == 0 {
			return ""
		}
		parts := make([]string, len(attrs))
		for i, a := range attrs {
			// uuid columns were parsed into a local by referenceChecks.
			if kinds[a.Type].sqlcType == "pgtype.UUID" {
				parts[i] = fmt.Sprintf("%s: %s", a.goName(), lowerCamel(a.Name))
				continue
			}
			parts[i] = fmt.Sprintf("%s: %s", a.goName(), a.toRow(src+"."+a.goName()))
		}
		return strings.Join(parts, ", ")
	}

	// Create
	fmt.Fprintf(&b, "// Create%s validates a %s, inserts it, and returns it with its id and\n", typ, name)
	b.WriteString("// timestamps. Validation runs on save, as Rails's does, so every caller\n")
	b.WriteString("// gets it and none can forget it.\n")
	fmt.Fprintf(&b, "func (s *Store) Create%s(ctx context.Context, %s domain.%s) (domain.%s, error) {\n", typ, recv, typ, typ)
	fmt.Fprintf(&b, "\tif err := %s.Validate(); err != nil {\n\t\treturn domain.%s{}, err\n\t}\n", recv, typ)
	b.WriteString(referenceChecks(recv, typ, attrs))
	switch len(attrs) {
	case 0:
		fmt.Fprintf(&b, "\trow, err := s.q.Create%s(ctx)\n", typ)
	case 1:
		// sqlc makes a Params struct only for two or more parameters; one
		// is passed as itself.
		fmt.Fprintf(&b, "\trow, err := s.q.Create%s(ctx, %s)\n", typ, singleParam(recv, attrs[0]))
	default:
		fmt.Fprintf(&b, "\trow, err := s.q.Create%s(ctx, Create%sParams{%s})\n", typ, typ, createParams(recv))
	}
	fmt.Fprintf(&b, "\tif err != nil {\n\t\treturn domain.%s{}, fmt.Errorf(\"models: create %s: %%w\", err)\n\t}\n", typ, name)
	fmt.Fprintf(&b, "\treturn %sFromRow(row), nil\n}\n\n", name)

	// Get
	fmt.Fprintf(&b, "// Get%s finds one %s. An id that is not a uuid is simply not found.\n", typ, name)
	fmt.Fprintf(&b, "func (s *Store) Get%s(ctx context.Context, id string) (domain.%s, error) {\n", typ, typ)
	fmt.Fprintf(&b, "\tuid, err := parseUUID(id)\n\tif err != nil {\n\t\treturn domain.%s{}, domain.ErrNotFound\n\t}\n", typ)
	fmt.Fprintf(&b, "\trow, err := s.q.Get%s(ctx, uid)\n\tswitch {\n\tcase errors.Is(err, pgx.ErrNoRows):\n\t\treturn domain.%s{}, domain.ErrNotFound\n", typ, typ)
	fmt.Fprintf(&b, "\tcase err != nil:\n\t\treturn domain.%s{}, fmt.Errorf(\"models: get %s %%s: %%w\", id, err)\n\t}\n", typ, name)
	fmt.Fprintf(&b, "\treturn %sFromRow(row), nil\n}\n\n", name)

	// List
	fmt.Fprintf(&b, "// List%s returns the newest %s first, at most limit of them.\n", Camel(table), table)
	fmt.Fprintf(&b, "func (s *Store) List%s(ctx context.Context, limit int32) ([]domain.%s, error) {\n", Camel(table), typ)
	fmt.Fprintf(&b, "\trows, err := s.q.List%s(ctx, limit)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"models: list %s: %%w\", err)\n\t}\n", Camel(table), table)
	fmt.Fprintf(&b, "\tout := make([]domain.%s, 0, len(rows))\n\tfor _, r := range rows {\n\t\tout = append(out, %sFromRow(r))\n\t}\n\treturn out, nil\n}\n\n", typ, name)

	// Update
	fmt.Fprintf(&b, "// Update%s validates a %s, writes its columns, and returns the stored row.\n", typ, name)
	fmt.Fprintf(&b, "func (s *Store) Update%s(ctx context.Context, %s domain.%s) (domain.%s, error) {\n", typ, recv, typ, typ)
	fmt.Fprintf(&b, "\tif err := %s.Validate(); err != nil {\n\t\treturn domain.%s{}, err\n\t}\n", recv, typ)
	fmt.Fprintf(&b, "\tuid, err := parseUUID(%s.ID)\n\tif err != nil {\n\t\treturn domain.%s{}, domain.ErrNotFound\n\t}\n", recv, typ)
	b.WriteString(referenceChecks(recv, typ, attrs))
	if len(attrs) == 0 {
		fmt.Fprintf(&b, "\trow, err := s.q.Update%s(ctx, uid)\n", typ)
	} else {
		fmt.Fprintf(&b, "\trow, err := s.q.Update%s(ctx, Update%sParams{ID: uid, %s})\n", typ, typ, createParams(recv))
	}
	fmt.Fprintf(&b, "\tswitch {\n\tcase errors.Is(err, pgx.ErrNoRows):\n\t\treturn domain.%s{}, domain.ErrNotFound\n", typ)
	fmt.Fprintf(&b, "\tcase err != nil:\n\t\treturn domain.%s{}, fmt.Errorf(\"models: update %s %%s: %%w\", %s.ID, err)\n\t}\n", typ, name, recv)
	fmt.Fprintf(&b, "\treturn %sFromRow(row), nil\n}\n\n", name)

	// Delete
	fmt.Fprintf(&b, "// Delete%s removes a %s, and says so if there was none.\n", typ, name)
	fmt.Fprintf(&b, "func (s *Store) Delete%s(ctx context.Context, id string) error {\n", typ)
	b.WriteString("\tuid, err := parseUUID(id)\n\tif err != nil {\n\t\treturn domain.ErrNotFound\n\t}\n")
	fmt.Fprintf(&b, "\tn, err := s.q.Delete%s(ctx, uid)\n\tif err != nil {\n\t\treturn fmt.Errorf(\"models: delete %s %%s: %%w\", id, err)\n\t}\n", typ, name)
	b.WriteString("\tif n == 0 {\n\t\treturn domain.ErrNotFound\n\t}\n\treturn nil\n}\n\n")

	// fromRow
	fmt.Fprintf(&b, "func %sFromRow(r %s) domain.%s {\n\treturn domain.%s{\n", name, typ, typ, typ)
	width := len("CreatedAt:")
	for _, a := range attrs {
		if n := len(a.goName()) + 1; n > width {
			width = n
		}
	}
	fmt.Fprintf(&b, "\t\t%-*s uuidString(r.ID),\n", width, "ID:")
	for _, a := range attrs {
		fmt.Fprintf(&b, "\t\t%-*s %s,\n", width, a.goName()+":", a.fromRow("r."+a.goName()))
	}
	fmt.Fprintf(&b, "\t\t%-*s r.CreatedAt.Time,\n", width, "CreatedAt:")
	fmt.Fprintf(&b, "\t\t%-*s r.UpdatedAt.Time,\n", width, "UpdatedAt:")
	b.WriteString("\t}\n}\n")
	return b.String()
}

// singleParam is the one argument sqlc's Create takes for a one-column model.
func singleParam(recv string, a Attr) string {
	if kinds[a.Type].sqlcType == "pgtype.UUID" {
		return lowerCamel(a.Name)
	}
	return a.toRow(recv + "." + a.goName())
}

// referenceChecks parses every uuid-typed attribute before the query, so a
// malformed id is a validation error, not a database error.
func referenceChecks(recv, typ string, attrs []Attr) string {
	var b strings.Builder
	for _, a := range attrs {
		if kinds[a.Type].sqlcType != "pgtype.UUID" {
			continue
		}
		v := lowerCamel(a.Name)
		fmt.Fprintf(&b, "\t%s, err := parseUUID(%s.%s)\n\tif err != nil {\n", v, recv, a.goName())
		fmt.Fprintf(&b, "\t\treturn domain.%s{}, fmt.Errorf(\"%%w: %s is not a uuid\", domain.ErrInvalid)\n\t}\n", typ, a.Name)
	}
	return b.String()
}

// looksPlural reports whether name is the plural of some word: it changes
// when singularised and comes back when pluralised again. Words ending in
// -us, -ss or -is (status, address, analysis) are singular and exempt.
func looksPlural(name string) bool {
	for _, suffix := range []string{"us", "ss", "is"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	s := Singular(name)
	return s != name && Plural(s) == name
}

func needsPgtype(attrs []Attr) bool {
	for _, a := range attrs {
		if strings.HasPrefix(kinds[a.Type].sqlcType, "pgtype.") && a.Type == "datetime" {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}
