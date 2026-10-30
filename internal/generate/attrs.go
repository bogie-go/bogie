package generate

import (
	"fmt"
	"regexp"
	"strings"
)

// Attr is one column, as `name:type[:index|:uniq|:null]` on the command line.
type Attr struct {
	Name   string
	Type   string // the generator name: string, text, integer, ...
	Index  bool
	Unique bool
	// Null is a column that may be NULL. Every generated column is NOT NULL
	// unless it says :null; `bogie model:sync` also sets it for a nullable
	// column it finds in the schema.
	Null bool
}

// kind is what one attribute type becomes at each layer.
type kind struct {
	sql       string // the Postgres column type
	sqlDef    string // NOT NULL and a default, so sqlc emits a plain Go type
	domain    string // the Go type in app/models, stdlib only
	sqlcType  string // the Go type sqlc emits for the column
	toRow     string // expression converting a domain value %s to the row type
	fromRow   string // expression converting a row value %s to the domain type
	imports   []string
	reference bool
}

// kinds maps attribute names to Postgres and Go. A generated column is
// NOT NULL unless the attribute says :null: a nullable column makes sqlc emit
// a pointer, and a pointer in the model is a nil check in every caller, so
// null is asked for, not the default.
//
// "references" has no entry here: what a foreign key is made of depends on
// the app's IDScheme, not the attribute, so it is resolved by kindFor.
var kinds = map[string]kind{
	"string":   {sql: "varchar", sqlDef: "NOT NULL DEFAULT ''", domain: "string", sqlcType: "string", toRow: "%s", fromRow: "%s"},
	"text":     {sql: "text", sqlDef: "NOT NULL DEFAULT ''", domain: "string", sqlcType: "string", toRow: "%s", fromRow: "%s"},
	"integer":  {sql: "integer", sqlDef: "NOT NULL DEFAULT 0", domain: "int32", sqlcType: "int32", toRow: "%s", fromRow: "%s"},
	"bigint":   {sql: "bigint", sqlDef: "NOT NULL DEFAULT 0", domain: "int64", sqlcType: "int64", toRow: "%s", fromRow: "%s"},
	"boolean":  {sql: "boolean", sqlDef: "NOT NULL DEFAULT false", domain: "bool", sqlcType: "bool", toRow: "%s", fromRow: "%s"},
	"datetime": {sql: "timestamptz", sqlDef: "NOT NULL", domain: "time.Time", sqlcType: "pgtype.Timestamptz", toRow: "pgtype.Timestamptz{Time: %s, Valid: true}", fromRow: "%s.Time", imports: []string{"time"}},
	"uuid":     {sql: "uuid", sqlDef: "NOT NULL", domain: "string", sqlcType: "pgtype.UUID", toRow: "%s", fromRow: "uuidString(%s)"},
	// json.RawMessage and []byte assign to each other without a conversion,
	// so the store needs no encoding/json import.
	"jsonb": {sql: "jsonb", sqlDef: "NOT NULL DEFAULT '{}'", domain: "json.RawMessage", sqlcType: "[]byte", toRow: "%s", fromRow: "%s", imports: []string{"encoding/json"}},
	// references is a placeholder so ParseAttrs and the help text see a
	// known type name; kindFor never reads this entry.
	"references": {reference: true},
}

// IDScheme is how an app's primary keys and foreign keys are made: bigint
// (the default, an auto-incrementing bigserial) or uuid. Chosen once
// per app at `bogie new` time, recorded in bogie.toml, and threaded through
// every generator that writes an id or a references column, so an app never
// mixes the two.
type IDScheme string

const (
	IDBigint IDScheme = "bigint"
	IDUUID   IDScheme = "uuid"
)

// IsValid reports whether s is a scheme bogie knows, so a bad --id flag or a
// hand-edited bogie.toml fails with a clear message instead of generating
// the wrong thing silently.
func (s IDScheme) IsValid() bool { return s == IDBigint || s == IDUUID }

// idColumn is the "id" column's own line in CREATE TABLE: its type, and
// however this scheme generates a new one.
func (s IDScheme) idColumn() (sqlType, def string) {
	if s == IDUUID {
		return "uuid", "PRIMARY KEY DEFAULT gen_random_uuid()"
	}
	return "bigserial", "PRIMARY KEY"
}

// referencesKind is what a `post:references` attribute becomes under this
// scheme: a foreign key is always the same shape as the id it points to.
func (s IDScheme) referencesKind() kind {
	if s == IDUUID {
		return kind{sql: "uuid", sqlDef: "NOT NULL", domain: "string", sqlcType: "pgtype.UUID", toRow: "%s", fromRow: "uuidString(%s)", reference: true}
	}
	return kind{sql: "bigint", sqlDef: "NOT NULL", domain: "int64", sqlcType: "int64", toRow: "%s", fromRow: "%s", reference: true}
}

// idDomain is the model's own ID field's type in app/models.
func (s IDScheme) idDomain() string {
	if s == IDUUID {
		return "string"
	}
	return "int64"
}

// parseFunc is the store helper, from db/adapter/uuid.go or id.go, that
// turns a URL path string into this scheme's id type.
func (s IDScheme) parseFunc() string {
	if s == IDUUID {
		return "parseUUID"
	}
	return "parseID"
}

// idFromRow converts a scanned row's ID (expr) to the model's id type.
func (s IDScheme) idFromRow(expr string) string {
	if s == IDUUID {
		return "uuidString(" + expr + ")"
	}
	return expr
}

// notFoundWord names what a malformed id is not, for the Get/Update/Delete
// doc comments: "An id that is not a uuid/valid id is simply not found."
func (s IDScheme) notFoundWord() string {
	if s == IDUUID {
		return "uuid"
	}
	return "valid id"
}

// sampleID is a well-formed id that names nothing, for a generated test's
// not-found cases.
func (s IDScheme) sampleID() string {
	if s == IDUUID {
		return "00000000-0000-4000-8000-000000000000"
	}
	return "999999999"
}

// kindFor resolves an attribute to its kind under scheme: every type but
// references is scheme-independent and comes straight from kinds; a
// references attribute is whatever shape this scheme's foreign keys are.
func kindFor(a Attr, scheme IDScheme) kind {
	k := kinds[a.Type]
	if a.Type == "references" {
		k = scheme.referencesKind()
	}
	if a.Null {
		k = nullable(k)
	}
	return k
}

// attrAliases are accepted spellings that resolve to a type in kinds. `bogie g
// config` takes int and bool; scaffold, model and migration take integer and
// boolean. Each now takes both.
var attrAliases = map[string]string{"int": "integer", "bool": "boolean"}

// nullable is k for a column that may be NULL. With emit_pointers_for_null_types
// sqlc emits a pointer for a string, number or bool, and the model holds the
// same pointer. A datetime stays a pgtype in the row and becomes *time.Time in
// the model, through two helpers (see nullableHelpers). A uuid has no
// conversion here, so a nullable uuid is refused by checkNull. jsonb is
// already nil when empty.
func nullable(k kind) kind {
	k.sqlDef = strings.TrimPrefix(k.sqlDef, "NOT NULL")
	k.sqlDef = strings.TrimSpace(k.sqlDef)
	if i := strings.Index(k.sqlDef, "DEFAULT"); i >= 0 {
		// A nullable column starts as NULL, not as the empty value.
		k.sqlDef = strings.TrimSpace(k.sqlDef[:i])
	}
	switch k.sqlcType {
	case "string", "int32", "int64", "bool":
		k.sqlcType = "*" + k.sqlcType
		k.domain = "*" + k.domain
	case "pgtype.Timestamptz":
		k.domain = "*time.Time"
		k.toRow = "timePtrToTimestamptz(%s)"
		k.fromRow = "timestamptzToTimePtr(%s)"
	}
	return k
}

// checkNull refuses a nullable column whose type bogie cannot convert: a uuid,
// and so a references column under the uuid scheme.
func (a Attr) checkNull(scheme IDScheme) error {
	if a.Null && kindFor(Attr{Name: a.Name, Type: a.Type}, scheme).sqlcType == "pgtype.UUID" {
		return fmt.Errorf("%s: a uuid column cannot be :null here; write the migration by hand", a.Name)
	}
	return nil
}

var attrPattern = regexp.MustCompile(`^([a-z][a-z0-9_]*)(?::([a-z]+))?((?::(?:index|uniq|null))*)$`)

// ParseAttrs reads `name[:type][:index|:uniq|:null]` arguments. A bare name with no
// `:type` defaults to string. A `references` attr names the
// other model: `post:references` becomes the column post_id.
func ParseAttrs(args []string) ([]Attr, error) {
	var out []Attr
	for _, a := range args {
		m := attrPattern.FindStringSubmatch(a)
		if m == nil {
			return nil, fmt.Errorf("%q is not name[:type][:index|:uniq|:null]", a)
		}
		typ := m[2]
		if typ == "" {
			typ = "string"
		}
		// The names `bogie g config` uses, accepted here too, so one habit works.
		if canon, ok := attrAliases[typ]; ok {
			typ = canon
		}
		attr := Attr{Name: m[1], Type: typ}
		for _, mod := range strings.Split(strings.TrimPrefix(m[3], ":"), ":") {
			switch mod {
			case "index":
				attr.Index = true
			case "uniq":
				attr.Unique = true
			case "null":
				attr.Null = true
			}
		}
		if _, ok := kinds[attr.Type]; !ok {
			return nil, fmt.Errorf("%q: unknown type %q; one of %s", a, attr.Type, strings.Join(typeNames(), ", "))
		}
		if attr.Type == "references" {
			attr.Name += "_id"
			attr.Index = true
		}
		out = append(out, attr)
	}
	return out, nil
}

func typeNames() []string {
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// column is the attr's line in CREATE TABLE, padded to align with the fixed
// columns around it.
func (a Attr) column(scheme IDScheme) string {
	k := kindFor(a, scheme)
	def := k.sqlDef
	if a.Type == "references" {
		def += fmt.Sprintf(" REFERENCES %s (id)", Plural(strings.TrimSuffix(a.Name, "_id")))
	}
	if a.Unique {
		def += " UNIQUE"
	}
	return strings.TrimRight(fmt.Sprintf("    %-10s %-11s %s", a.Name, k.sql, def), " ")
}

// indexStatement is the CREATE INDEX for an indexed attr, or "".
func (a Attr) indexStatement(table string) string {
	if !a.Index {
		return ""
	}
	return fmt.Sprintf("CREATE INDEX %s_%s_idx ON %s (%s);", table, a.Name, table, a.Name)
}

func (a Attr) goName() string { return Camel(a.Name) }
func (a Attr) domainType(scheme IDScheme) string {
	return kindFor(a, scheme).domain
}
func (a Attr) toRow(scheme IDScheme, expr string) string {
	return fmt.Sprintf(kindFor(a, scheme).toRow, expr)
}
func (a Attr) fromRow(scheme IDScheme, expr string) string {
	return fmt.Sprintf(kindFor(a, scheme).fromRow, expr)
}
