package generate

import (
	"fmt"
	"go/format"
	"strings"
	"time"
)

// Model writes what `bogie generate model post title:string body:text`
// produces: the create_<table> migration, a starter db/queries/<table>.sql,
// the domain type, the hand-written half of the store that converts sqlc's
// row type to it, its spec and its test factory. Running sqlc afterwards is the caller's job; the files here
// are written to match what it will generate.
func Model(module, name string, attrs []Attr, scheme IDScheme, now time.Time) ([]File, error) {
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

	migration, err := Migration("create_"+table, attrs, scheme, now)
	if err != nil {
		return nil, err
	}
	return []File{
		migration,
		{Path: "db/queries/" + table + ".sql", Content: queries(name, table, attrs)},
		{Path: "app/domain/" + name + ".go", Content: domainType(name, attrs, scheme)},
		{Path: "app/models/" + table + ".go", Content: store(module, name, table, attrs, scheme)},
		{Path: "app/models/" + table + "_test.go", Content: storeTest(module, name, table, attrs, scheme)},
		{Path: "test/factories/" + table + ".go", Content: factory(module, name, attrs, scheme)},
	}, nil
}

// storeTest is the model's spec, against the real test database as every
// store spec is, in package models_test where the suite's store is already
// built: the not-found cases, which hold for any set of attributes, and a
// round trip through the model's factory when the model can be saved on its
// own. A model with a references column cannot: its parent has to exist
// first, and bogie cannot know the parent's factory does.
func storeTest(module, name, table string, attrs []Attr, scheme IDScheme) string {
	typ, many := Camel(name), Camel(table)
	hasRefs := hasReferences(attrs)
	missingID := scheme.sampleID()
	if scheme == IDUUID {
		missingID = fmt.Sprintf("%q", missingID)
	}
	// Every uuid reference gets a well-formed value, so the store reaches the
	// query and answers not-found rather than refusing a malformed id.
	refFields := ""
	for _, a := range attrs {
		if a.Type == "references" && scheme == IDUUID {
			refFields += fmt.Sprintf("\t\t\t\tmissing.%s = %q\n", a.goName(), scheme.sampleID())
		}
	}

	imports := []string{"\t. \"github.com/onsi/ginkgo/v2\"", "\t. \"github.com/onsi/gomega\""}
	var b strings.Builder
	var specs strings.Builder
	// The model's name as prose, for the spec sentences: each one names the
	// model rather than saying "it", so it reads whole under its Describe.
	word := strings.ReplaceAll(name, "_", " ")

	// Each store method is a Describe; a Context sets up the situation and
	// an It states one behavior. What is tested is the Describe's name.
	fmt.Fprintf(&specs, "var _ = Describe(%q, func() {\n", many)

	// An id the store cannot find is not found, whatever else the model holds.
	notFound := func(call string) string {
		return fmt.Sprintf(`		DescribeTable("is not found for",
			func(id string) {
%s
			},
			Entry("a malformed id", "nope"),
			Entry("an empty id", ""),
			Entry("a well-formed id that names nothing", %q),
		)
`, call, scheme.sampleID())
	}
	getNotFound := notFound(fmt.Sprintf("\t\t\t\t_, err := store.Get%[1]s(GinkgoT().Context(), id)\n\t\t\t\tExpect(err).To(MatchError(domain.ErrNotFound))", typ))
	deleteNotFound := notFound(fmt.Sprintf("\t\t\t\tExpect(store.Delete%[1]s(GinkgoT().Context(), id)).To(MatchError(domain.ErrNotFound))", typ))

	updateMissing := fmt.Sprintf(`		Context("when the %[1]s is not there", func() {
			var missing domain.%[2]s

			BeforeEach(func() {
				missing = factories.New%[2]s()
				missing.ID = %[3]s
%[4]s			})

			It("is not found", func() {
				_, err := store.Update%[2]s(GinkgoT().Context(), missing)
				Expect(err).To(MatchError(domain.ErrNotFound))
			})
		})
`, word, typ, missingID, refFields)

	if hasRefs {
		fmt.Fprintf(&specs, `	Describe("Get%[1]s", func() {
%[2]s	})

	Describe("Delete%[1]s", func() {
%[3]s	})

	Describe("Update%[1]s", func() {
%[4]s	})

	// No round trip here: a %[5]s belongs to a parent that has to be saved
	// first. Make the parent with its factory and add the specs, with the
	// reference set in the factory's "with" function.
`, typ, getNotFound, deleteNotFound, updateMissing, word)
	} else {
		idExpr := "record.ID"
		if scheme != IDUUID {
			idExpr = "strconv.FormatInt(record.ID, 10)"
			imports = append([]string{"\t\"strconv\""}, imports...)
		}
		var change, check string
		saves := "saves the " + word
		for _, a := range attrs {
			if _, lit := edited(a, scheme); lit != "" {
				change = fmt.Sprintf("\t\t\t\trecord.%s = %s\n", a.goName(), lit)
				check = fmt.Sprintf("\t\t\t\tExpect(updated.%s).To(BeEquivalentTo(%s))\n", a.goName(), lit)
				saves = "stores the change"
				break
			}
		}
		fmt.Fprintf(&specs, `	Describe("Get%[2]s", func() {
%[9]s
		Context("with a saved %[1]s", func() {
			var record domain.%[2]s

			BeforeEach(func() {
				record = factories.%[2]s(GinkgoT(), store)
			})

			It("finds the %[1]s by id", func() {
				Expect(store.Get%[2]s(GinkgoT().Context(), %[3]s)).To(Equal(record))
			})
		})
	})

	Describe("List%[4]s", func() {
		Context("with a saved %[1]s", func() {
			var record domain.%[2]s

			BeforeEach(func() {
				record = factories.%[2]s(GinkgoT(), store)
			})

			It("includes the saved %[1]s", func() {
				all, err := store.List%[4]s(GinkgoT().Context(), 10)
				Expect(err).NotTo(HaveOccurred())
				Expect(all).To(HaveLen(1))
				Expect(all[0].ID).To(Equal(record.ID))
			})
		})
	})

	Describe("Update%[2]s", func() {
		Context("with a saved %[1]s", func() {
			var record domain.%[2]s

			BeforeEach(func() {
				record = factories.%[2]s(GinkgoT(), store)
			})

			It(%[7]q, func() {
%[5]s				updated, err := store.Update%[2]s(GinkgoT().Context(), record)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.ID).To(Equal(record.ID))
%[6]s			})
		})

%[8]s	})

	Describe("Delete%[2]s", func() {
%[10]s
		Context("with a saved %[1]s", func() {
			var record domain.%[2]s

			BeforeEach(func() {
				record = factories.%[2]s(GinkgoT(), store)
			})

			It("removes the %[1]s", func() {
				id := %[3]s
				Expect(store.Delete%[2]s(GinkgoT().Context(), id)).To(Succeed())
				_, err := store.Get%[2]s(GinkgoT().Context(), id)
				Expect(err).To(MatchError(domain.ErrNotFound))
			})
		})
	})
`, word, typ, idExpr, many, change, check, saves, updateMissing, getNotFound, deleteNotFound)
	}
	specs.WriteString("})\n")

	fmt.Fprintf(&b, "package models_test\n\nimport (\n")
	for _, imp := range imports[:len(imports)-2] {
		b.WriteString(imp + "\n")
	}
	if len(imports) > 2 {
		b.WriteString("\n")
	}
	b.WriteString(imports[len(imports)-2] + "\n" + imports[len(imports)-1] + "\n\n")
	fmt.Fprintf(&b, "\t%q\n\t%q\n)\n\n", module+"/app/domain", module+"/test/factories")
	b.WriteString(specs.String())
	return formatted(b.String())
}

func hasReferences(attrs []Attr) bool {
	for _, a := range attrs {
		if a.Type == "references" {
			return true
		}
	}
	return false
}

// formatted gofmts generated source, which aligns what the generators leave
// loose. Source that does not parse is returned as it is, so the caller's
// test fails on the real text.
func formatted(src string) string {
	out, err := format.Source([]byte(src))
	if err != nil {
		return src
	}
	return string(out)
}

// factory is test/factories/<table>.go: a builder and a saver for the model
// with valid defaults, so a spec says only what it cares about.
//
// A references column is the one thing it cannot default: the parent has to
// exist, and bogie cannot know the parent has a factory (it may be a model
// made by hand). The column stays at its zero value, the doc comment says so,
// and the spec passes the parent's id in "with".
func factory(module, name string, attrs []Attr, scheme IDScheme) string {
	typ := Camel(name)
	imports := map[string]bool{"context": true}
	var fields strings.Builder
	var refs []string
	for _, a := range attrs {
		var v string
		switch a.Type {
		case "string", "text":
			v = fmt.Sprintf("Sequence(%q)", strings.ReplaceAll(a.Name, "_", " ")+" ")
		case "integer", "bigint":
			v = "1"
		case "boolean":
			v = "true"
		case "datetime":
			v = "time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)"
			imports["time"] = true
		case "uuid":
			v = "uuid.NewString()"
			imports["github.com/google/uuid"] = true
		case "jsonb":
			v = `[]byte("{}")`
		case "references":
			refs = append(refs, a.goName())
			continue
		}
		fmt.Fprintf(&fields, "\t\t%s: %s,\n", a.goName(), v)
	}
	var std, third []string
	for _, imp := range sortedKeys(imports) {
		if strings.Contains(imp, ".") {
			third = append(third, imp)
		} else {
			std = append(std, imp)
		}
	}
	var b strings.Builder
	b.WriteString("package factories\n\nimport (\n")
	for _, imp := range std {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString("\n")
	for _, imp := range third {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	if len(third) > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\t%q\n\t%q\n)\n\n", module+"/app/domain", module+"/app/models")

	note := ""
	if len(refs) > 0 {
		note = fmt.Sprintf("//\n// Left at zero: %s. The parent has to exist first, so set it from the\n// parent's own factory:\n//\n//\tfactories.%s(t, s, func(v *domain.%s) { v.%s = parent.ID })\n", strings.Join(refs, ", "), typ, typ, refs[0])
	}
	fmt.Fprintf(&b, `// New%[1]s is a valid %[2]s that is not saved: every attribute has a default,
// and "with" overrides what the spec cares about.
//
// Written once by `+"`bogie g model`"+` and yours to edit: add an attribute's
// default here when you add the column. Running the generator again leaves a
// changed file alone unless --force.
%[4]sfunc New%[1]s(with ...func(*domain.%[1]s)) domain.%[1]s {
	record := domain.%[1]s{
%[3]s	}
	for _, f := range with {
		f(&record)
	}
	return record
}

// %[1]s saves a New%[1]s through the real store and fails the spec if it
// cannot, so a spec reads as its arrangement and nothing else.
func %[1]s(t T, s *models.Store, with ...func(*domain.%[1]s)) domain.%[1]s {
	t.Helper()
	created, err := s.Create%[1]s(context.Background(), New%[1]s(with...))
	if err != nil {
		t.Fatal(err)
	}
	return created
}
`, typ, name, fields.String(), note)
	return formatted(b.String())
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
	fmt.Fprintf(&b, "-- Written by `bogie g model`: overwritten whole on the next run. A\n")
	fmt.Fprintf(&b, "-- hand-written query goes in %s_extra.sql, not here.\n\n", table)
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

func domainType(name string, attrs []Attr, scheme IDScheme) string {
	typ := Camel(name)
	imports := map[string]bool{"time": true}
	for _, a := range attrs {
		for _, imp := range kindFor(a, scheme).imports {
			imports[imp] = true
		}
	}
	var b strings.Builder
	b.WriteString("package domain\n\nimport (\n")
	for _, imp := range sortedKeys(imports) {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString(")\n\n")
	fmt.Fprintf(&b, "// %s is a %s. Imports nothing third-party: ids are %ss here and\n", typ, name, scheme.idDomain())
	fmt.Fprintf(&b, "// become %s at the store boundary.\n", scheme)
	fmt.Fprintf(&b, "type %s struct {\n", typ)
	width := len("CreatedAt")
	for _, a := range attrs {
		if n := len(a.goName()); n > width {
			width = n
		}
	}
	fmt.Fprintf(&b, "\t%-*s %s\n", width, "ID", scheme.idDomain())
	for _, a := range attrs {
		fmt.Fprintf(&b, "\t%-*s %s\n", width, a.goName(), a.domainType(scheme))
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

func store(module, name, table string, attrs []Attr, scheme IDScheme) string {
	typ := Camel(name)
	recv := paramName(name, "s")
	uniq := uniqueAttrs(attrs)
	var b strings.Builder
	b.WriteString("package models\n\n")
	// pgconn is always needed now: every Delete classifies a foreign-key
	// violation. gofmt sorts imports, so pgconn goes before pgtype.
	b.WriteString("import (\n\t\"context\"\n\t\"errors\"\n\t\"fmt\"\n\n\t\"github.com/jackc/pgx/v5\"\n\t\"github.com/jackc/pgx/v5/pgconn\"\n")
	if needsPgtype(attrs) {
		b.WriteString("\t\"github.com/jackc/pgx/v5/pgtype\"\n")
	}
	fmt.Fprintf(&b, "\n\t%q\n\t%q\n)\n\n", module+"/app/domain", module+"/db/sqlc")

	fmt.Fprintf(&b, "// %s, as the domain sees them. Each method converts domain.%s to and from\n", Camel(table), typ)
	b.WriteString("// the row type sqlc generated, and maps pgx's \"no rows\" to domain.ErrNotFound\n")
	b.WriteString("// so callers never import pgx to ask a question.\n")
	fmt.Fprintf(&b, "//\n// Written by `bogie g model`: overwritten whole on the next run. A\n// hand-written addition goes in %s_extra.go, not here.\n\n", table)

	// params for Create and Update
	createParams := func(src string) string {
		if len(attrs) == 0 {
			return ""
		}
		parts := make([]string, len(attrs))
		for i, a := range attrs {
			// uuid columns were parsed into a local by referenceChecks.
			if kindFor(a, scheme).sqlcType == "pgtype.UUID" {
				parts[i] = fmt.Sprintf("%s: %s", a.goName(), lowerCamel(a.Name))
				continue
			}
			parts[i] = fmt.Sprintf("%s: %s", a.goName(), a.toRow(scheme, src+"."+a.goName()))
		}
		return strings.Join(parts, ", ")
	}

	// Create
	fmt.Fprintf(&b, "// Create%s validates a %s, inserts it, and returns it with its id and\n", typ, name)
	b.WriteString("// timestamps. Validation runs on save, as Rails's does, so every caller\n")
	b.WriteString("// gets it and none can forget it.\n")
	fmt.Fprintf(&b, "func (s *Store) Create%s(ctx context.Context, %s domain.%s) (domain.%s, error) {\n", typ, recv, typ, typ)
	fmt.Fprintf(&b, "\tif err := %s.Validate(); err != nil {\n\t\treturn domain.%s{}, err\n\t}\n", recv, typ)
	b.WriteString(referenceChecks(recv, typ, attrs, scheme))
	switch len(attrs) {
	case 0:
		fmt.Fprintf(&b, "\trow, err := s.q.Create%s(ctx)\n", typ)
	case 1:
		// sqlc makes a Params struct only for two or more parameters; one
		// is passed as itself.
		fmt.Fprintf(&b, "\trow, err := s.q.Create%s(ctx, %s)\n", typ, singleParam(recv, attrs[0], scheme))
	default:
		fmt.Fprintf(&b, "\trow, err := s.q.Create%s(ctx, sqlc.Create%sParams{%s})\n", typ, typ, createParams(recv))
	}
	if len(uniq) > 0 {
		fmt.Fprintf(&b, "\tif err != nil {\n\t\tvar pgErr *pgconn.PgError\n\t\tif errors.As(err, &pgErr) && pgErr.Code == \"23505\" {\n\t\t\treturn domain.%s{}, fmt.Errorf(\"%%w: %s\", domain.ErrInvalid)\n\t\t}\n\t\treturn domain.%s{}, fmt.Errorf(\"models: create %s: %%w\", err)\n\t}\n", typ, uniqueViolationMessage(uniq), typ, name)
	} else {
		fmt.Fprintf(&b, "\tif err != nil {\n\t\treturn domain.%s{}, fmt.Errorf(\"models: create %s: %%w\", err)\n\t}\n", typ, name)
	}
	fmt.Fprintf(&b, "\treturn %sFromRow(row), nil\n}\n\n", name)

	// Get
	fmt.Fprintf(&b, "// Get%s finds one %s. An id that is not a %s is simply not found.\n", typ, name, scheme.notFoundWord())
	fmt.Fprintf(&b, "func (s *Store) Get%s(ctx context.Context, id string) (domain.%s, error) {\n", typ, typ)
	fmt.Fprintf(&b, "\tpk, err := %s(id)\n\tif err != nil {\n\t\treturn domain.%s{}, domain.ErrNotFound\n\t}\n", scheme.parseFunc(), typ)
	fmt.Fprintf(&b, "\trow, err := s.q.Get%s(ctx, pk)\n\tswitch {\n\tcase errors.Is(err, pgx.ErrNoRows):\n\t\treturn domain.%s{}, domain.ErrNotFound\n", typ, typ)
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
	if scheme == IDUUID {
		// The domain's id is a string here too, so it gets the same
		// malformed-id-is-not-found treatment a URL-sourced id gets.
		fmt.Fprintf(&b, "\tpk, err := parseUUID(%s.ID)\n\tif err != nil {\n\t\treturn domain.%s{}, domain.ErrNotFound\n\t}\n", recv, typ)
	} else {
		// Nothing to parse: the domain's id is already this scheme's type.
		fmt.Fprintf(&b, "\tpk := %s.ID\n", recv)
	}
	b.WriteString(referenceChecks(recv, typ, attrs, scheme))
	if len(attrs) == 0 {
		fmt.Fprintf(&b, "\trow, err := s.q.Update%s(ctx, pk)\n", typ)
	} else {
		fmt.Fprintf(&b, "\trow, err := s.q.Update%s(ctx, sqlc.Update%sParams{ID: pk, %s})\n", typ, typ, createParams(recv))
	}
	if len(uniq) > 0 {
		b.WriteString("\tvar pgErr *pgconn.PgError\n")
	}
	fmt.Fprintf(&b, "\tswitch {\n\tcase errors.Is(err, pgx.ErrNoRows):\n\t\treturn domain.%s{}, domain.ErrNotFound\n", typ)
	if len(uniq) > 0 {
		fmt.Fprintf(&b, "\tcase errors.As(err, &pgErr) && pgErr.Code == \"23505\":\n\t\treturn domain.%s{}, fmt.Errorf(\"%%w: %s\", domain.ErrInvalid)\n", typ, uniqueViolationMessage(uniq))
	}
	fmt.Fprintf(&b, "\tcase err != nil:\n\t\treturn domain.%s{}, fmt.Errorf(\"models: update %s %%v: %%w\", %s.ID, err)\n\t}\n", typ, name, recv)
	fmt.Fprintf(&b, "\treturn %sFromRow(row), nil\n}\n\n", name)

	// Delete
	fmt.Fprintf(&b, "// Delete%s removes a %s, and says so if there was none.\n", typ, name)
	fmt.Fprintf(&b, "func (s *Store) Delete%s(ctx context.Context, id string) error {\n", typ)
	fmt.Fprintf(&b, "\tpk, err := %s(id)\n\tif err != nil {\n\t\treturn domain.ErrNotFound\n\t}\n", scheme.parseFunc())
	fmt.Fprintf(&b, "\tn, err := s.q.Delete%s(ctx, pk)\n\tif err != nil {\n\t\tvar pgErr *pgconn.PgError\n\t\tif errors.As(err, &pgErr) && pgErr.Code == \"23503\" {\n\t\t\treturn fmt.Errorf(\"%%w: %s is still in use\", domain.ErrInvalid)\n\t\t}\n\t\treturn fmt.Errorf(\"models: delete %s %%s: %%w\", id, err)\n\t}\n", typ, name, name)
	b.WriteString("\tif n == 0 {\n\t\treturn domain.ErrNotFound\n\t}\n\treturn nil\n}\n\n")

	// fromRow
	fmt.Fprintf(&b, "func %sFromRow(r sqlc.%s) domain.%s {\n\treturn domain.%s{\n", name, typ, typ, typ)
	width := len("CreatedAt:")
	for _, a := range attrs {
		if n := len(a.goName()) + 1; n > width {
			width = n
		}
	}
	fmt.Fprintf(&b, "\t\t%-*s %s,\n", width, "ID:", scheme.idFromRow("r.ID"))
	for _, a := range attrs {
		fmt.Fprintf(&b, "\t\t%-*s %s,\n", width, a.goName()+":", a.fromRow(scheme, "r."+a.goName()))
	}
	fmt.Fprintf(&b, "\t\t%-*s r.CreatedAt.Time,\n", width, "CreatedAt:")
	fmt.Fprintf(&b, "\t\t%-*s r.UpdatedAt.Time,\n", width, "UpdatedAt:")
	b.WriteString("\t}\n}\n")
	return b.String()
}

// singleParam is the one argument sqlc's Create takes for a one-column model.
func singleParam(recv string, a Attr, scheme IDScheme) string {
	if kindFor(a, scheme).sqlcType == "pgtype.UUID" {
		return lowerCamel(a.Name)
	}
	return a.toRow(scheme, recv+"."+a.goName())
}

// referenceChecks parses every uuid-typed attribute before the query, so a
// malformed id is a validation error, not a database error. Under the
// bigint scheme a references attribute is a plain int64 like any other
// integer column, so this writes nothing.
func referenceChecks(recv, typ string, attrs []Attr, scheme IDScheme) string {
	var b strings.Builder
	for _, a := range attrs {
		if kindFor(a, scheme).sqlcType != "pgtype.UUID" {
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

// uniqueAttrs is attrs that must be unique, in order: a model with none of
// these never needs to tell a duplicate-value error apart from any other
// database error.
func uniqueAttrs(attrs []Attr) []Attr {
	var out []Attr
	for _, a := range attrs {
		if a.Unique {
			out = append(out, a)
		}
	}
	return out
}

// uniqueViolationMessage names the columns a unique-constraint violation on
// uniq is about, for the ErrInvalid a caller gets instead of a raw
// Postgres error: "email is already taken", or "email or username is
// already taken" for more than one.
func uniqueViolationMessage(uniq []Attr) string {
	names := make([]string, len(uniq))
	for i, a := range uniq {
		names[i] = a.Name
	}
	return strings.Join(names, " or ") + " is already taken"
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
