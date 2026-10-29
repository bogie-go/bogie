package generate

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAttrsNull(t *testing.T) {
	got := attrs(t, "archived_at:datetime:null", "slug:string:uniq:null", "title:string")
	if !got[0].Null || got[0].Type != "datetime" {
		t.Errorf("archived_at: %+v", got[0])
	}
	if !got[1].Null || !got[1].Unique {
		t.Errorf("slug: %+v", got[1])
	}
	if got[2].Null {
		t.Errorf("title is NOT NULL unless it says otherwise: %+v", got[2])
	}
	if _, err := Migration("create_things", attrs(t, "owner:uuid:null"), IDBigint, at); err == nil {
		t.Error("a nullable uuid was accepted")
	}
	if _, err := Migration("create_things", attrs(t, "owner:references:null"), IDUUID, at); err == nil {
		t.Error("a nullable uuid reference was accepted")
	}
}

func TestModelWithNullableAttributes(t *testing.T) {
	files, err := Model("example.com/blog", "article", attrs(t, "title:string", "subtitle:string:null", "views:integer:null", "published_at:datetime:null", "edited_at:datetime"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_nullable", files)
}

func TestColumns(t *testing.T) {
	migrations := []string{
		`-- +goose Up
CREATE TABLE users (
    id         bigserial   PRIMARY KEY,
    email      varchar     NOT NULL DEFAULT '' UNIQUE,
    nickname   varchar,
    archived_at timestamptz,
    CONSTRAINT users_nick CHECK (nickname <> 'x')
);
CREATE TABLE other (id bigserial PRIMARY KEY, nickname text);
-- +goose Down
DROP TABLE users;`,
		`-- +goose Up
ALTER TABLE users ADD COLUMN seen_at timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS age integer NOT NULL DEFAULT 0, ADD COLUMN bio text;
ALTER TABLE users ALTER COLUMN archived_at SET NOT NULL;
ALTER TABLE users ALTER COLUMN age DROP NOT NULL;
ALTER TABLE users RENAME COLUMN bio TO about;
ALTER TABLE users DROP COLUMN nickname;
CREATE UNIQUE INDEX users_seen_at_idx ON users (seen_at);
CREATE UNIQUE INDEX users_pair ON users (age, about);
-- +goose Down
ALTER TABLE users DROP COLUMN seen_at;`,
	}
	got := Columns(migrations, "users")
	want := map[string]Column{
		"id":          {},
		"email":       {Unique: true},
		"archived_at": {},
		"seen_at":     {Null: true, Unique: true},
		"age":         {Null: true},
		"about":       {Null: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Columns =\n%v\nwant\n%v", got, want)
	}
}

// The sync must produce what g model would have, for an app nobody edited:
// taking a model made for one set of attributes to another gives the same
// model struct, queries, store and factory as generating for the other set.
func TestSyncMatchesGeneratingTheNewColumns(t *testing.T) {
	cases := []struct {
		name     string
		from, to []string
	}{
		{"add a string", []string{"title:string"}, []string{"title:string", "role:string"}},
		{"add a nullable datetime", []string{"title:string"}, []string{"title:string", "archived_at:datetime:null"}},
		{"add a nullable string and a number", []string{"title:string"}, []string{"title:string", "subtitle:string:null", "views:integer"}},
		{"add a jsonb", []string{"title:string"}, []string{"title:string", "meta:jsonb"}},
		{"remove a jsonb", []string{"title:string", "meta:jsonb"}, []string{"title:string"}},
		{"remove from the middle", []string{"a:string", "b:integer", "c:boolean"}, []string{"a:string", "c:boolean"}},
		{"add and remove", []string{"a:string", "b:integer"}, []string{"a:string", "c:datetime", "d:text"}},
		{"add a reference", []string{"title:string"}, []string{"title:string", "owner:references"}},
		{"add a unique column", []string{"title:string"}, []string{"title:string", "email:string:uniq"}},
		{"remove the last column", []string{"title:string"}, nil},
		{"add to a model with none", nil, []string{"title:string"}},
	}
	for _, scheme := range []IDScheme{IDBigint, IDUUID} {
		for _, c := range cases {
			t.Run(string(scheme)+"/"+c.name, func(t *testing.T) {
				from, to := attrs(t, c.from...), attrs(t, c.to...)
				old, err := Model("example.com/blog", "article", from, scheme, at)
				if err != nil {
					t.Fatal(err)
				}
				want, err := Model("example.com/blog", "article", to, scheme, at)
				if err != nil {
					t.Fatal(err)
				}

				plan, err := PlanSync(
					mustFields(t, fileContent(old, "app/models/article.go"), "Article"),
					rowFields(to, scheme), schemaOf(to), scheme)
				if err != nil {
					t.Fatal(err)
				}
				patched, err := PatchModel([]byte(fileContent(old, "app/models/article.go")), "article", plan, scheme)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := string(patched), formatted(fileContent(want, "app/models/article.go")); got != want {
					t.Errorf("model struct:\n%s\nwant:\n%s", got, want)
				}

				queries, store, factory := SyncFiles("example.com/blog", "article", plan.New, scheme)
				for _, f := range []struct{ path, got string }{
					{"db/queries/articles.sql", queries},
					{"db/adapter/articles.go", store},
					{"test/factories/articles.go", factory},
				} {
					if want := fileContent(want, f.path); f.got != formatIfGo(f.path, want) {
						t.Errorf("%s:\n%s\nwant:\n%s", f.path, f.got, want)
					}
				}
			})
		}
	}
}

func TestPlanSync(t *testing.T) {
	model := []StructField{
		{"ID", "int64"}, {"Email", "string"}, {"Name", "string"},
		{"Items", "[]Item"}, {"CreatedAt", "time.Time"}, {"UpdatedAt", "time.Time"},
	}
	row := []StructField{
		{"ID", "int64"}, {"Email", "string"}, {"Role", "string"},
		{"ArchivedAt", "pgtype.Timestamptz"}, {"Age", "*int32"},
		{"CreatedAt", "pgtype.Timestamptz"}, {"UpdatedAt", "pgtype.Timestamptz"},
	}
	plan, err := PlanSync(model, row, map[string]Column{"archived_at": {Null: true}}, IDBigint)
	if err != nil {
		t.Fatal(err)
	}
	var added, removed []string
	for _, a := range plan.Added {
		added = append(added, a.Name+":"+a.Type+map[bool]string{true: ":null", false: ""}[a.Null])
	}
	for _, a := range plan.Removed {
		removed = append(removed, a.Name)
	}
	if got := strings.Join(added, " "); got != "role:string archived_at:datetime:null age:integer:null" {
		t.Errorf("added = %s", got)
	}
	// Items is not a column bogie writes: it is not removed.
	if got := strings.Join(removed, " "); got != "name" {
		t.Errorf("removed = %s", got)
	}
	if plan.InSync() {
		t.Error("a model with a missing column was in sync")
	}

	same, err := PlanSync(row, row, nil, IDBigint)
	if err != nil {
		t.Fatal(err)
	}
	if !same.InSync() {
		t.Errorf("a model equal to its table was not in sync: %+v", same)
	}

	if _, err := PlanSync(model, append(row, StructField{"Price", "pgtype.Numeric"}), nil, IDBigint); err == nil {
		t.Error("a column of a type bogie does not write was accepted")
	}
	if _, err := PlanSync(model, append(row, StructField{"OwnerID", "pgtype.UUID"}), map[string]Column{"owner_id": {Null: true}}, IDUUID); err == nil {
		t.Error("a nullable uuid was accepted")
	}
}

func TestPlanSyncNotesATypeThatChanged(t *testing.T) {
	model := []StructField{{"ID", "int64"}, {"Age", "string"}}
	row := []StructField{{"ID", "int64"}, {"Age", "int32"}}
	plan, err := PlanSync(model, row, nil, IDBigint)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.InSync() || len(plan.Notes) != 1 || !strings.Contains(plan.Notes[0], "age") {
		t.Errorf("plan = %+v", plan)
	}
}

func TestPatchModelKeepsWhatIsYours(t *testing.T) {
	src := `package models

import (
	"fmt"
	"time"
)

// User is a user.
type User struct {
	ID    int64
	Email string
	// Name is what the user asked to be called.
	Name string
	// Created is when.
	CreatedAt time.Time
	UpdatedAt time.Time
}

const RoleStaff = "staff"

// Validate is mine.
func (u User) Validate() error {
	if u.Email == "" {
		return fmt.Errorf("%w: email", ErrInvalid)
	}
	return nil
}
`
	plan := SyncPlan{
		Added:   attrs(t, "role:string", "meta:jsonb"),
		Removed: attrs(t, "name:string"),
	}
	got, err := PatchModel([]byte(src), "user", plan, IDBigint)
	if err != nil {
		t.Fatal(err)
	}
	want := `package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// User is a user.
type User struct {
	ID    int64
	Email string
	Role  string
	Meta  json.RawMessage
	// Created is when.
	CreatedAt time.Time
	UpdatedAt time.Time
}

const RoleStaff = "staff"

// Validate is mine.
func (u User) Validate() error {
	if u.Email == "" {
		return fmt.Errorf("%w: email", ErrInvalid)
	}
	return nil
}
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// Taking the json field out again takes the import with it.
	back, err := PatchModel(got, "user", SyncPlan{Removed: attrs(t, "meta:jsonb")}, IDBigint)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(back), "encoding/json") || strings.Contains(string(back), "Meta") {
		t.Errorf("json is still there:\n%s", back)
	}

	if _, err := PatchModel([]byte(src), "user", SyncPlan{Removed: attrs(t, "nope:string")}, IDBigint); err == nil {
		t.Error("removing a field that is not there succeeded")
	}
	if _, err := PatchModel([]byte(src), "post", SyncPlan{}, IDBigint); err == nil {
		t.Error("a file with no such struct was accepted")
	}
}

// fileContent is the content of the generated file at path.
func fileContent(files []File, path string) string {
	for _, f := range files {
		if f.Path == path {
			return f.Content
		}
	}
	return ""
}

func formatIfGo(path, src string) string {
	if strings.HasSuffix(path, ".go") {
		return formatted(src)
	}
	return src
}

func mustFields(t *testing.T, src, typ string) []StructField {
	t.Helper()
	f, err := StructFields([]byte(src), typ)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// rowFields is the struct sqlc would generate for these attributes.
func rowFields(attrs []Attr, scheme IDScheme) []StructField {
	fields := []StructField{{"ID", map[IDScheme]string{IDBigint: "int64", IDUUID: "pgtype.UUID"}[scheme]}}
	for _, a := range attrs {
		fields = append(fields, StructField{a.goName(), kindFor(a, scheme).sqlcType})
	}
	return append(fields, StructField{"CreatedAt", "pgtype.Timestamptz"}, StructField{"UpdatedAt", "pgtype.Timestamptz"})
}

// schemaOf is what the migrations would say of these attributes.
func schemaOf(attrs []Attr) map[string]Column {
	out := map[string]Column{}
	for _, a := range attrs {
		out[a.Name] = Column{Null: a.Null, Unique: a.Unique}
	}
	return out
}

// A nullable column is a pointer, so the specs that set a plain value on the
// first string column must pick one that is not.
func TestModelSpecsSkipNullableColumns(t *testing.T) {
	files, err := Model("example.com/blog", "article", attrs(t, "subtitle:string:null", "title:string"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileContent(files, "db/adapter/articles_test.go"); strings.Contains(got, "record.Subtitle") || !strings.Contains(got, "record.Title") {
		t.Errorf("the store spec edits the wrong column:\n%s", got)
	}
	if got := fileContent(files, "app/models/article_test.go"); strings.Contains(got, "Subtitle") {
		t.Errorf("the model spec example uses a nullable column:\n%s", got)
	}
}

func TestScaffoldRefusesNullableColumns(t *testing.T) {
	if _, _, err := Scaffold("example.com/blog", "", "", "article", attrs(t, "title:string", "subtitle:string:null"), IDBigint, at, nil); err == nil {
		t.Error("a scaffold with a nullable column was written")
	}
}
