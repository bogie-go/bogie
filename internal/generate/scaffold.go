package generate

import (
	"fmt"
	"go/format"
	"strings"
	"time"
)

// Scaffold writes what `bogie generate scaffold post title:string body:text`
// produces, as `rails g scaffold` does: the model (everything Model writes),
// the wire shapes in app/views, and a controller with real handlers, a test
// with a fake store, and the three lines that register it with the store
// passed in. No service: the controller talks to the store, and the store
// validates on save, as a Rails controller talks to the model (docs/DESIGN.md
// §5). The model is named in the singular and the rest in the plural, as
// Rails names them.
func Scaffold(module, name string, attrs []Attr, now time.Time) ([]File, []Wire, error) {
	files, err := Model(module, name, attrs, now)
	if err != nil {
		return nil, nil, err
	}
	table := Plural(name)
	s := scaffold{module: module, name: name, table: table, attrs: attrs}
	// Struct fields and tags are aligned by gofmt rather than by hand, since
	// the attributes decide the widths.
	for _, f := range []struct{ path, src string }{
		{"app/views/" + table + ".go", s.views()},
		{"app/controllers/" + table + "_controller.go", s.controller()},
		{"app/controllers/" + table + "_controller_test.go", s.controllerTest()},
	} {
		out, err := format.Source([]byte(f.src))
		if err != nil {
			return nil, nil, fmt.Errorf("scaffold %s: generated %s does not parse: %w", name, f.path, err)
		}
		files = append(files, File{Path: f.path, Content: string(out)})
	}
	c := controller{module: module, name: table}
	wires := c.wires()
	wires[1].Line = fmt.Sprintf("server.%s = controllers.New%s(store, log)", Camel(table), c.typeName())
	return files, wires, nil
}

// ScaffoldFiles are the paths Scaffold writes for name, for destroy, which
// cannot know the attributes it was given. Model's files are included.
func ScaffoldFiles(module, name string) ([]File, error) {
	files, _, err := Scaffold(module, name, nil, time.Time{})
	return files, err
}

type scaffold struct {
	module, name, table string
	attrs               []Attr
}

func (s scaffold) typ() string  { return Camel(s.name) }  // Post
func (s scaffold) many() string { return Camel(s.table) } // Posts
func (s scaffold) ctl() string  { return s.many() + "Controller" }
func (s scaffold) storeIface() string {
	return lowerCamel(s.table) + "Store"
}

// views is app/views/<table>.go: the request body, one response, and the
// listing. Nothing here imports the domain; the controller converts.
func (s scaffold) views() string {
	imports := map[string]bool{"time": true}
	for _, a := range s.attrs {
		for _, imp := range kinds[a.Type].imports {
			imports[imp] = true
		}
	}
	var b strings.Builder
	b.WriteString("package views\n\nimport (\n")
	for _, imp := range sortedKeys(imports) {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString(")\n\n")

	width := len("UpdatedAt")
	for _, a := range s.attrs {
		if n := len(a.goName()); n > width {
			width = n
		}
	}
	field := func(a Attr) string {
		return fmt.Sprintf("\t%-*s %s `json:%q`\n", width, a.goName(), a.domainType(), a.Name)
	}

	fmt.Fprintf(&b, "// %sRequest is the body of POST /%s and PATCH /%s/:id.\n", s.typ(), s.table, s.table)
	fmt.Fprintf(&b, "type %sRequest struct {\n", s.typ())
	for _, a := range s.attrs {
		b.WriteString(field(a))
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// %sResponse is one %s on the wire.\n", s.typ(), s.name)
	fmt.Fprintf(&b, "type %sResponse struct {\n", s.typ())
	fmt.Fprintf(&b, "\t%-*s string `json:\"id\"`\n", width, "ID")
	for _, a := range s.attrs {
		b.WriteString(field(a))
	}
	fmt.Fprintf(&b, "\t%-*s time.Time `json:\"created_at\"`\n", width, "CreatedAt")
	fmt.Fprintf(&b, "\t%-*s time.Time `json:\"updated_at\"`\n", width, "UpdatedAt")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// %sResponse is a listing.\n", s.many())
	fmt.Fprintf(&b, "type %sResponse struct {\n\t%s []%sResponse `json:%q`\n}\n", s.many(), s.many(), s.typ(), s.table)
	return b.String()
}

// controller is app/controllers/<table>_controller.go: the store interface
// it consumes, the type, its routes, the five resourceful actions, the
// error mapping, and the domain-to-wire conversions.
func (s scaffold) controller() string {
	typ, many, ctl, iface := s.typ(), s.many(), s.ctl(), s.storeIface()
	recv := s.name[:1]
	var b strings.Builder
	fmt.Fprintf(&b, `package controllers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	%[1]q
	%[2]q
)

// %[3]s is the slice of the store this controller uses, declared next to
// its caller (rule 2) and holding only the methods the actions call.
// *models.Store satisfies it, and app/application.go passes it in; the
// test's fake is a few lines.
type %[3]s interface {
	Create%[4]s(ctx context.Context, %[5]s domain.%[4]s) (domain.%[4]s, error)
	Get%[4]s(ctx context.Context, id string) (domain.%[4]s, error)
	List%[6]s(ctx context.Context, limit int32) ([]domain.%[4]s, error)
	Update%[4]s(ctx context.Context, %[5]s domain.%[4]s) (domain.%[4]s, error)
	Delete%[4]s(ctx context.Context, id string) error
}

// %[7]sListLimit is how many a listing returns, newest first. Paging is
// yours to add when a client needs more.
const %[7]sListLimit = 100

// %[8]s is the HTTP surface of %[9]s: a JSON resource over the store. A
// handler converts HTTP into domain values, calls one store method, and
// converts the result back. Validation is the domain's and runs on save,
// which is why a bad value is a 422 from the store and not a binding rule
// here. Nothing below app/controllers sees *gin.Context (rule 5).
type %[8]s struct {
	Store %[3]s
	Log   *slog.Logger
}

// New%[8]s builds the controller.
func New%[8]s(store %[3]s, log *slog.Logger) *%[8]s {
	return &%[8]s{Store: store, Log: log}
}

// SetupRoutes mounts the resource under its own prefix. Middleware the whole
// controller needs goes here; which group it sits in, and so how it is
// exposed, is decided by routes.go, which calls this.
func (ctl *%[8]s) SetupRoutes(router *gin.RouterGroup) {
	%[9]s := router.Group("/%[9]s")
	%[9]s.GET("", ctl.Index)
	%[9]s.POST("", ctl.Create)
	%[9]s.GET("/:id", ctl.Show)
	%[9]s.PATCH("/:id", ctl.Update)
	%[9]s.DELETE("/:id", ctl.Destroy)
}

// Index handles GET /%[9]s.
func (ctl *%[8]s) Index(c *gin.Context) {
	%[9]s, err := ctl.Store.List%[6]s(c.Request.Context(), %[7]sListLimit)
	if err != nil {
		ctl.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, %[7]sView(%[9]s))
}

// Show handles GET /%[9]s/:id.
func (ctl *%[8]s) Show(c *gin.Context) {
	%[5]s, err := ctl.Store.Get%[4]s(c.Request.Context(), c.Param("id"))
	if err != nil {
		ctl.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, %[10]sView(%[5]s))
}

`, s.module+"/app/domain", s.module+"/app/views", iface, typ, recv, many, lowerCamel(s.table), ctl, s.table, lowerCamel(s.name))

	// Create and Update bind the request body, when there is one to bind.
	fmt.Fprintf(&b, "// Create handles POST /%s.\n", s.table)
	fmt.Fprintf(&b, "func (ctl *%s) Create(c *gin.Context) {\n", ctl)
	b.WriteString(s.bind())
	fmt.Fprintf(&b, "\t%s, err := ctl.Store.Create%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest(""))
	fmt.Fprintf(&b, "\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n\tc.JSON(http.StatusCreated, %sView(%s))\n}\n\n", lowerCamel(s.name), recv)

	fmt.Fprintf(&b, "// Update handles PATCH /%s/:id.", s.table)
	if len(s.attrs) == 0 {
		fmt.Fprintf(&b, "\nfunc (ctl *%s) Update(c *gin.Context) {\n", ctl)
		fmt.Fprintf(&b, "\t%s, err := ctl.Store.Update%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest("c.Param(\"id\")"))
	} else {
		// As in Rails's find-then-update: load the record, decode the body
		// over its current values so a key left out keeps its value, then
		// save, so validation sees the whole record.
		fmt.Fprintf(&b, " Only the fields in the body change, as\n")
		fmt.Fprintf(&b, "// in Rails: the %s is loaded, the body is decoded over its current values,\n", s.name)
		b.WriteString("// so a key left out keeps its value, and the result is saved and validated\n// whole.\n")
		fmt.Fprintf(&b, "func (ctl *%s) Update(c *gin.Context) {\n", ctl)
		fmt.Fprintf(&b, "\t%s, err := ctl.Store.Get%s(c.Request.Context(), c.Param(\"id\"))\n", recv, typ)
		b.WriteString("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n")
		fmt.Fprintf(&b, "\treq := %s\n", s.toRequest(recv))
		b.WriteString("\tif err := c.ShouldBindJSON(&req); err != nil {\n\t\tc.JSON(http.StatusBadRequest, views.Error(err))\n\t\treturn\n\t}\n")
		fmt.Fprintf(&b, "\t%s, err = ctl.Store.Update%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest("c.Param(\"id\")"))
	}
	fmt.Fprintf(&b, "\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n\tc.JSON(http.StatusOK, %sView(%s))\n}\n\n", lowerCamel(s.name), recv)

	fmt.Fprintf(&b, `// Destroy handles DELETE /%[1]s/:id.
func (ctl *%[2]s) Destroy(c *gin.Context) {
	if err := ctl.Store.Delete%[3]s(c.Request.Context(), c.Param("id")); err != nil {
		ctl.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// fail maps a domain error to a status, the one place that mapping lives:
// not found is 404, a validation failure is 422 as in Rails, and anything
// else is logged and answered 500 without leaking its text.
func (ctl *%[2]s) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		c.JSON(http.StatusNotFound, views.ErrorMessage("no such %[4]s"))
	case errors.Is(err, domain.ErrInvalid):
		c.JSON(http.StatusUnprocessableEntity, views.Error(err))
	default:
		ctl.Log.ErrorContext(c.Request.Context(), "%[1]s: request failed",
			"path", c.FullPath(), "err", err, "request_id", c.GetString("request_id"))
		c.JSON(http.StatusInternalServerError, views.ErrorMessage("internal error"))
	}
}

// The controller converts between the domain and the wire. views never
// imports domain, so this is where %[3]sResponse is built.

`, s.table, ctl, typ, s.name)

	fmt.Fprintf(&b, "func %sView(%s domain.%s) views.%sResponse {\n\treturn views.%sResponse{\n", lowerCamel(s.name), recv, typ, typ, typ)
	width := len("CreatedAt:")
	for _, a := range s.attrs {
		if n := len(a.goName()) + 1; n > width {
			width = n
		}
	}
	fmt.Fprintf(&b, "\t\t%-*s %s.ID,\n", width, "ID:", recv)
	for _, a := range s.attrs {
		fmt.Fprintf(&b, "\t\t%-*s %s.%s,\n", width, a.goName()+":", recv, a.goName())
	}
	fmt.Fprintf(&b, "\t\t%-*s %s.CreatedAt,\n\t\t%-*s %s.UpdatedAt,\n\t}\n}\n\n", width, "CreatedAt:", recv, width, "UpdatedAt:", recv)

	fmt.Fprintf(&b, "func %sView(%s []domain.%s) views.%sResponse {\n", lowerCamel(s.table), s.table, typ, many)
	fmt.Fprintf(&b, "\tout := views.%sResponse{%s: make([]views.%sResponse, 0, len(%s))}\n", many, many, typ, s.table)
	fmt.Fprintf(&b, "\tfor _, %s := range %s {\n\t\tout.%s = append(out.%s, %sView(%s))\n\t}\n\treturn out\n}\n", recv, s.table, many, many, lowerCamel(s.name), recv)
	return b.String()
}

// bind reads the request body into views.<Type>Request, or nothing when
// the model has no attributes to bind.
func (s scaffold) bind() string {
	if len(s.attrs) == 0 {
		return ""
	}
	return fmt.Sprintf("\tvar req views.%sRequest\n\tif err := c.ShouldBindJSON(&req); err != nil {\n\t\tc.JSON(http.StatusBadRequest, views.Error(err))\n\t\treturn\n\t}\n", s.typ())
}

// fromRequest is the domain value built from the bound request, with the
// id when there is one.
func (s scaffold) fromRequest(id string) string {
	var parts []string
	if id != "" {
		parts = append(parts, "ID: "+id)
	}
	for _, a := range s.attrs {
		parts = append(parts, fmt.Sprintf("%s: req.%s", a.goName(), a.goName()))
	}
	return fmt.Sprintf("domain.%s{%s}", s.typ(), strings.Join(parts, ", "))
}

// toRequest is the request body prefilled with a stored value's fields, for
// PATCH to decode over.
func (s scaffold) toRequest(recv string) string {
	parts := make([]string, len(s.attrs))
	for i, a := range s.attrs {
		parts[i] = fmt.Sprintf("%s: %s.%s", a.goName(), recv, a.goName())
	}
	return fmt.Sprintf("views.%sRequest{%s}", s.typ(), strings.Join(parts, ", "))
}

// edited is a second JSON value and Go literal per attribute type, unlike
// sample's, for the PATCH test to change one field with. Types sample has
// no literal for have none here either.
func edited(a Attr) (jsonValue, goLiteral string) {
	switch a.Type {
	case "string", "text":
		return `"Edited"`, `"Edited"`
	case "integer", "bigint":
		return "8", "8"
	case "boolean":
		return "false", "false"
	case "datetime", "jsonb":
		return "", ""
	default: // uuid, references
		return `"00000000-0000-4000-8000-000000000001"`, `"00000000-0000-4000-8000-000000000001"`
	}
}

// sample is a JSON value and the Go literal it decodes to, per attribute
// type, for the controller test's request body.
func sample(a Attr) (jsonValue, goLiteral string) {
	switch a.Type {
	case "string", "text":
		return `"Hello"`, `"Hello"`
	case "integer", "bigint":
		return "7", "7"
	case "boolean":
		return "true", "true"
	case "datetime":
		return `"2026-10-02T09:30:00Z"`, ""
	case "jsonb":
		return `{"k":1}`, ""
	default: // uuid, references
		return `"00000000-0000-4000-8000-000000000000"`, `"00000000-0000-4000-8000-000000000000"`
	}
}

// controllerTest is the controller's test: a fake of the store interface
// records what the controller asked for and answers with what the test
// chose, so no database is involved.
func (s scaffold) controllerTest() string {
	typ, many, ctl := s.typ(), s.many(), s.ctl()
	fake := "fake" + many + "Store"
	helper := lowerCamel(s.table) + "Request"
	var b strings.Builder
	fmt.Fprintf(&b, `package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	%[1]q
)

// %[2]s is a fake of the store interface declared beside the controller: it
// records what the controller asked for and answers with what the test chose.
type %[2]s struct {
	one  domain.%[3]s
	many []domain.%[3]s
	err  error

	gotID string
	got   domain.%[3]s
}

func (f *%[2]s) Create%[3]s(_ context.Context, %[4]s domain.%[3]s) (domain.%[3]s, error) {
	f.got = %[4]s
	return f.one, f.err
}

func (f *%[2]s) Get%[3]s(_ context.Context, id string) (domain.%[3]s, error) {
	f.gotID = id
	return f.one, f.err
}

func (f *%[2]s) List%[5]s(context.Context, int32) ([]domain.%[3]s, error) { return f.many, f.err }

func (f *%[2]s) Update%[3]s(_ context.Context, %[4]s domain.%[3]s) (domain.%[3]s, error) {
	f.gotID, f.got = %[4]s.ID, %[4]s
	return f.one, f.err
}

func (f *%[2]s) Delete%[3]s(_ context.Context, id string) error {
	f.gotID = id
	return f.err
}

// %[6]s mounts the controller alone on a bare router, so these tests see
// the controller and nothing else: no middleware, no other routes.
func %[6]s(t *testing.T, store *%[2]s, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	New%[7]s(store, slog.New(slog.NewTextHandler(io.Discard, nil))).SetupRoutes(&router.RouterGroup)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func Test%[5]sIndexLists(t *testing.T) {
	store := &%[2]s{many: []domain.%[3]s{{ID: "a"}, {ID: "b"}}}
	rec := %[6]s(t, store, http.MethodGet, "/%[8]s", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %%d: %%s", rec.Code, rec.Body)
	}
	var body struct {
		%[5]s []struct{ ID string } `+"`json:%[9]q`"+`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.%[5]s) != 2 || body.%[5]s[1].ID != "b" {
		t.Errorf("body = %%+v", body)
	}
}

func Test%[5]sShowUnknownIs404(t *testing.T) {
	rec := %[6]s(t, &%[2]s{err: domain.ErrNotFound}, http.MethodGet, "/%[8]s/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %%d, want 404", rec.Code)
	}
}

`, s.module+"/app/domain", fake, typ, s.name[:1], many, helper, ctl, s.table, s.table)

	// Create: the body goes through to the store, and the answer is 201.
	var jsonParts []string
	var assertion string
	for _, a := range s.attrs {
		jv, lit := sample(a)
		jsonParts = append(jsonParts, fmt.Sprintf("%q:%s", a.Name, jv))
		if assertion == "" && lit != "" {
			assertion = fmt.Sprintf("\tif store.got.%s != %s {\n\t\tt.Errorf(\"store got %s = %%v\", store.got.%s)\n\t}\n", a.goName(), lit, a.goName(), a.goName())
		}
	}
	body := "{" + strings.Join(jsonParts, ",") + "}"
	fmt.Fprintf(&b, "func Test%sCreatePassesTheBodyThroughAndAnswers201(t *testing.T) {\n", many)
	fmt.Fprintf(&b, "\tstore := &%s{one: domain.%s{ID: \"a\"}}\n", fake, typ)
	fmt.Fprintf(&b, "\trec := %s(t, store, http.MethodPost, \"/%s\", `%s`)\n\n", helper, s.table, body)
	b.WriteString("\tif rec.Code != http.StatusCreated {\n\t\tt.Fatalf(\"status = %d: %s\", rec.Code, rec.Body)\n\t}\n")
	b.WriteString(assertion)
	b.WriteString("\tif !strings.Contains(rec.Body.String(), `\"id\":\"a\"`) {\n\t\tt.Errorf(\"body = %s\", rec.Body)\n\t}\n}\n\n")

	fmt.Fprintf(&b, `// Validation is the domain's and runs in the store. The controller's only
// job is to say 422.
func Test%[1]sCreateInvalidIs422(t *testing.T) {
	store := &%[2]s{err: errors.Join(domain.ErrInvalid, errors.New("not valid"))}
	rec := %[3]s(t, store, http.MethodPost, "/%[4]s", `+"`%[5]s`"+`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %%d, want 422: %%s", rec.Code, rec.Body)
	}
}

`, many, fake, helper, s.table, body)

	if len(s.attrs) > 0 {
		fmt.Fprintf(&b, `func Test%[1]sCreateMalformedJSONIs400(t *testing.T) {
	rec := %[2]s(t, &%[3]s{}, http.MethodPost, "/%[4]s", `+"`{\"nope\":`"+`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %%d, want 400", rec.Code)
	}
}

`, many, helper, fake, s.table)
	}

	b.WriteString(s.patchTest(fake, helper))

	fmt.Fprintf(&b, `func Test%[1]sDestroyAnswers204(t *testing.T) {
	store := &%[2]s{}
	rec := %[3]s(t, store, http.MethodDelete, "/%[4]s/a", "")
	if rec.Code != http.StatusNoContent || store.gotID != "a" {
		t.Errorf("status = %%d, id = %%q", rec.Code, store.gotID)
	}
}

// An unexpected error is a 500 that says nothing about its cause.
func Test%[1]sUnexpectedErrorIs500WithoutDetail(t *testing.T) {
	rec := %[3]s(t, &%[2]s{err: errors.New("connection refused to db-1")}, http.MethodGet, "/%[4]s", "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %%d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "db-1") {
		t.Errorf("body leaks the internal error: %%s", rec.Body)
	}
}
`, many, fake, helper, s.table)
	return b.String()
}

// patchTest proves PATCH changes only what the body names: the stored value
// has every attribute set, the body sends one of them, and the store is
// asked to save that one changed and the rest as they were. Models without
// an attribute the test can write a literal for get no such test.
func (s scaffold) patchTest(fake, helper string) string {
	var lits []Attr
	for _, a := range s.attrs {
		if _, lit := sample(a); lit != "" {
			lits = append(lits, a)
		}
	}
	if len(lits) == 0 {
		return ""
	}
	typ, many := s.typ(), s.many()
	stored := []string{`ID: "a"`}
	for _, a := range lits {
		_, lit := sample(a)
		stored = append(stored, fmt.Sprintf("%s: %s", a.goName(), lit))
	}
	sent := lits[0]
	jv, lit := edited(sent)
	var b strings.Builder
	fmt.Fprintf(&b, "// PATCH is partial, as in Rails: a field the body leaves out keeps its\n// stored value rather than being blanked.\n")
	fmt.Fprintf(&b, "func Test%sUpdateChangesOnlyTheFieldsSent(t *testing.T) {\n", many)
	fmt.Fprintf(&b, "\tstore := &%s{one: domain.%s{%s}}\n", fake, typ, strings.Join(stored, ", "))
	fmt.Fprintf(&b, "\trec := %s(t, store, http.MethodPatch, \"/%s/a\", `{%q:%s}`)\n\n", helper, s.table, sent.Name, jv)
	b.WriteString("\tif rec.Code != http.StatusOK {\n\t\tt.Fatalf(\"status = %d: %s\", rec.Code, rec.Body)\n\t}\n")
	fmt.Fprintf(&b, "\tif store.got.%s != %s {\n\t\tt.Errorf(\"store got %s = %%v, want %%v\", store.got.%s, %s)\n\t}\n", sent.goName(), lit, sent.goName(), sent.goName(), lit)
	for _, a := range lits[1:] {
		_, keep := sample(a)
		fmt.Fprintf(&b, "\tif store.got.%s != %s {\n\t\tt.Errorf(\"store got %s = %%v, want it kept\", store.got.%s)\n\t}\n", a.goName(), keep, a.goName(), a.goName())
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, `func Test%[1]sUpdateUnknownIs404(t *testing.T) {
	rec := %[2]s(t, &%[3]s{err: domain.ErrNotFound}, http.MethodPatch, "/%[4]s/nope", `+"`{}`"+`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %%d, want 404", rec.Code)
	}
}

`, many, helper, fake, s.table)
	return b.String()
}
