package generate

import (
	"fmt"
	"regexp"
	"strings"
)

// Attr is one column, as `name:type[:index|:uniq]` on the command line.
type Attr struct {
	Name   string
	Type   string // the Rails name: string, text, integer, ...
	Index  bool
	Unique bool
}

// kind is what one Rails attribute type becomes at each layer.
type kind struct {
	sql       string // the Postgres column type
	sqlDef    string // NOT NULL and a default, so sqlc emits a plain Go type
	domain    string // the Go type in app/domain, stdlib only
	sqlcType  string // the Go type sqlc emits for the column
	toRow     string // expression converting a domain value %s to the row type
	fromRow   string // expression converting a row value %s to the domain type
	imports   []string
	reference bool
}

// kinds maps Rails attribute names to Postgres and Go. Every generated column
// is NOT NULL: a nullable column makes sqlc emit a pointer, and a pointer in
// the domain is a nil check in every caller. A model that truly needs null
// edits the migration by hand, which is one line.
var kinds = map[string]kind{
	"string":   {sql: "text", sqlDef: "NOT NULL DEFAULT ''", domain: "string", sqlcType: "string", toRow: "%s", fromRow: "%s"},
	"text":     {sql: "text", sqlDef: "NOT NULL DEFAULT ''", domain: "string", sqlcType: "string", toRow: "%s", fromRow: "%s"},
	"integer":  {sql: "integer", sqlDef: "NOT NULL DEFAULT 0", domain: "int32", sqlcType: "int32", toRow: "%s", fromRow: "%s"},
	"bigint":   {sql: "bigint", sqlDef: "NOT NULL DEFAULT 0", domain: "int64", sqlcType: "int64", toRow: "%s", fromRow: "%s"},
	"boolean":  {sql: "boolean", sqlDef: "NOT NULL DEFAULT false", domain: "bool", sqlcType: "bool", toRow: "%s", fromRow: "%s"},
	"datetime": {sql: "timestamptz", sqlDef: "NOT NULL", domain: "time.Time", sqlcType: "pgtype.Timestamptz", toRow: "pgtype.Timestamptz{Time: %s, Valid: true}", fromRow: "%s.Time", imports: []string{"time"}},
	"uuid":     {sql: "uuid", sqlDef: "NOT NULL", domain: "string", sqlcType: "pgtype.UUID", toRow: "%s", fromRow: "uuidString(%s)"},
	// json.RawMessage and []byte assign to each other without a conversion,
	// so the store needs no encoding/json import.
	"jsonb":      {sql: "jsonb", sqlDef: "NOT NULL DEFAULT '{}'", domain: "json.RawMessage", sqlcType: "[]byte", toRow: "%s", fromRow: "%s", imports: []string{"encoding/json"}},
	"references": {sql: "uuid", sqlDef: "NOT NULL", domain: "string", sqlcType: "pgtype.UUID", toRow: "%s", fromRow: "uuidString(%s)", reference: true},
}

var attrPattern = regexp.MustCompile(`^([a-z][a-z0-9_]*):([a-z]+)(?::(index|uniq))?$`)

// ParseAttrs reads `name:type[:index|:uniq]` arguments. A `references` attr
// names the other model: `post:references` becomes the column post_id.
func ParseAttrs(args []string) ([]Attr, error) {
	var out []Attr
	for _, a := range args {
		m := attrPattern.FindStringSubmatch(a)
		if m == nil {
			return nil, fmt.Errorf("%q is not name:type[:index|:uniq]", a)
		}
		attr := Attr{Name: m[1], Type: m[2], Index: m[3] == "index", Unique: m[3] == "uniq"}
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
func (a Attr) column() string {
	k := kinds[a.Type]
	def := k.sqlDef
	if a.Type == "references" {
		def += fmt.Sprintf(" REFERENCES %s (id)", Plural(strings.TrimSuffix(a.Name, "_id")))
	}
	if a.Unique {
		def += " UNIQUE"
	}
	return fmt.Sprintf("    %-10s %-11s %s", a.Name, k.sql, def)
}

// indexStatement is the CREATE INDEX for an indexed attr, or "".
func (a Attr) indexStatement(table string) string {
	if !a.Index {
		return ""
	}
	return fmt.Sprintf("CREATE INDEX %s_%s_idx ON %s (%s);", table, a.Name, table, a.Name)
}

func (a Attr) goName() string     { return Camel(a.Name) }
func (a Attr) domainType() string { return kinds[a.Type].domain }
func (a Attr) toRow(expr string) string {
	return fmt.Sprintf(kinds[a.Type].toRow, expr)
}
func (a Attr) fromRow(expr string) string {
	return fmt.Sprintf(kinds[a.Type].fromRow, expr)
}
