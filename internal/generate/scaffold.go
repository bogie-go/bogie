package generate

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"time"
)

// ResourceActions are the five actions of a resource, in the order a scaffold
// writes them.
var ResourceActions = []string{"index", "show", "create", "update", "destroy"}

// ParseOnly reads the value of --only: actions separated by commas or spaces,
// returned once each, in the order of ResourceActions. new and edit render
// forms and an API has none, so they are refused as `g controller` refuses
// them.
func ParseOnly(spec string) ([]string, error) {
	want := map[string]bool{}
	for _, a := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' }) {
		if a == "new" || a == "edit" {
			return nil, fmt.Errorf("--only %s: %s renders a form; this is an API-only service, so there is no %s action", a, a, a)
		}
		if _, ok := resourceful[a]; !ok {
			return nil, fmt.Errorf("--only %s: not an action; one of %s", a, strings.Join(ResourceActions, ", "))
		}
		want[a] = true
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("--only needs at least one action of %s", strings.Join(ResourceActions, ", "))
	}
	var out []string
	for _, a := range ResourceActions {
		if want[a] {
			out = append(out, a)
		}
	}
	return out, nil
}

// Scaffold writes what `bogie generate scaffold post title:string body:text`
// produces: the model (everything Model writes),
// the wire shapes in app/views, and a controller with real handlers, a request
// test, and the three lines that register it with the store
// passed in. No service: the controller talks to the store, and the store
// validates on save, and a controller talks to the model (docs/DESIGN.md
// §5). The model is named in the singular and the rest in the plural.
//
// ns and res place the controller: the namespace to mount it under ("" for the
// root) and its resource name, plural ("" for the table's own). A namespaced
// scaffold is a model of its own, named with the namespace (admin_product, table
// admin_products) as Rails prefixes it, whose controller keeps the short name
// (ProductsController, at /admin/products).
//
// only limits the HTTP half to those actions (nil is all five): their routes,
// handlers, views and request specs. The model half is written in full either
// way.
func Scaffold(module, ns, res, name string, attrs []Attr, scheme IDScheme, now time.Time, only []string) ([]File, []Wire, error) {
	files, err := Model(module, name, attrs, scheme, now)
	if err != nil {
		return nil, nil, err
	}
	httpFiles, wires, err := ScaffoldController(module, ns, res, name, attrs, scheme, only)
	if err != nil {
		return nil, nil, err
	}
	return append(files, httpFiles...), wires, nil
}

// ParseScaffoldName splits what `g scaffold` accepts, post or admin/posts, into
// the namespace ("" for none), the controller's resource name (the plural of
// the last segment) and the model's name. A namespace is part of the model's
// name, as Rails prefixes it: admin/posts is the model admin_post with the
// table admin_posts, and its controller is PostsController in the admin
// namespace.
func ParseScaffoldName(spec string) (ns, res, model string, err error) {
	ns, name, err := ParseControllerName(spec)
	if err != nil {
		return "", "", "", err
	}
	if ns == "" {
		return "", "", name, nil
	}
	return ns, Plural(Singular(name)), ns + "_" + Singular(name), nil
}

// ScaffoldController writes the HTTP half of a scaffold for a model that
// already exists: the views, a controller over the model's store and its
// request spec, and the three lines that register it. ns is the namespace
// (admin) to mount it under, or "" for the root. The model is never
// namespaced, only its controller is, so the model and its table are named by
// name alone.
func ScaffoldController(module, ns, res, name string, attrs []Attr, scheme IDScheme, only []string) ([]File, []Wire, error) {
	s, err := newScaffold(module, ns, res, name, attrs, scheme, only)
	if err != nil {
		return nil, nil, err
	}
	// Struct fields and tags are aligned by gofmt rather than by hand, since
	// the attributes decide the widths.
	sources := []struct{ path, src string }{
		{s.viewsPath(), s.views()},
		{s.dir() + s.res + "_controller.go", s.controller()},
		{s.dir() + s.res + "_controller_test.go", s.controllerTest()},
	}
	var files []File
	for _, f := range sources {
		if f.src == "" {
			continue
		}
		out, err := format.Source([]byte(f.src))
		if err != nil {
			return nil, nil, fmt.Errorf("scaffold %s: generated %s does not parse: %w", name, f.path, err)
		}
		files = append(files, File{Path: f.path, Content: string(out)})
	}
	c := controller{module: module, ns: ns, name: s.res}
	wires := c.wires()
	if ns == "" {
		wires[1].Line = fmt.Sprintf("server.%s = controllers.New%s(store, log)", Camel(s.res), c.typeName())
	} else {
		wires[1].Line = fmt.Sprintf("server.%s.%s = %s.New%s(store, log)", Camel(ns), Camel(s.res), c.pkg(), c.typeName())
	}
	return files, wires, nil
}

// newScaffold names what a scaffold of name writes: the actions among only
// (nil is all five), in the namespace ns ("" for the root).
func newScaffold(module, ns, res, name string, attrs []Attr, scheme IDScheme, only []string) (scaffold, error) {
	if ns != "" {
		if err := checkNamespace(ns); err != nil {
			return scaffold{}, err
		}
	}
	actions := map[string]bool{}
	for _, a := range only {
		if _, ok := resourceful[a]; !ok {
			return scaffold{}, fmt.Errorf("%q is not an action; one of %s", a, strings.Join(ResourceActions, ", "))
		}
		actions[a] = true
	}
	if len(only) == 0 {
		for _, a := range ResourceActions {
			actions[a] = true
		}
	}
	if res == "" {
		res = Plural(name)
	}
	return scaffold{module: module, ns: ns, res: res, name: name, table: Plural(name), attrs: attrs, scheme: scheme, actions: actions}, nil
}

// ViewsPath is the file of a model's wire shapes, which a namespaced
// controller shares with the root one rather than writing a second set.
func ViewsPath(name string) string { return "app/views/" + Plural(name) + ".go" }

// ScaffoldFiles are the paths Scaffold writes for name, for destroy, which
// cannot know the attributes it was given. Model's files are included.
func ScaffoldFiles(module, ns, res, name string, scheme IDScheme) ([]File, error) {
	files, _, err := Scaffold(module, ns, res, name, nil, scheme, time.Time{}, nil)
	return files, err
}

// ModelAttrs reads a model's attributes back from its source in app/models,
// for the scaffold of a model that already exists: every field but the id and
// the two timestamps, with the type g model would have been given to make it.
// A Go string is a string (a uuid column and a text column read the same),
// and an id-typed field named ...ID is a references column.
func ModelAttrs(src []byte, name string, scheme IDScheme) ([]Attr, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return nil, err
	}
	var st *ast.StructType
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range g.Specs {
			if t, ok := sp.(*ast.TypeSpec); ok && t.Name.Name == Camel(name) {
				st, _ = t.Type.(*ast.StructType)
			}
		}
	}
	if st == nil {
		return nil, fmt.Errorf("no struct %s in the model's file", Camel(name))
	}
	var out []Attr
	for _, f := range st.Fields.List {
		typ := types.ExprString(f.Type)
		for _, n := range f.Names {
			switch n.Name {
			case "ID", "CreatedAt", "UpdatedAt":
				continue
			}
			col := fieldColumn(n.Name)
			a := Attr{Name: col}
			switch typ {
			case "string":
				a.Type = "string"
				if scheme == IDUUID && strings.HasSuffix(col, "_id") {
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
				a.Type = "jsonb"
			default:
				return nil, fmt.Errorf("field %s has type %s; a scaffold controller covers the types g model writes (string, int32, int64, bool, time.Time, json.RawMessage)", n.Name, typ)
			}
			out = append(out, a)
		}
	}
	return out, nil
}

// fieldColumn is the column a Go field name came from: ImageUrl is image_url
// and CategoryID is category_id.
func fieldColumn(field string) string {
	if base, ok := strings.CutSuffix(field, "ID"); ok && base != "" {
		return Snake(base) + "_id"
	}
	return Snake(field)
}

type scaffold struct {
	module, ns, res, name, table string
	attrs                        []Attr
	scheme                       IDScheme
	actions                      map[string]bool // the actions written; never empty
}

// has reports whether the action is among those written.
func (s scaffold) has(actions ...string) bool {
	for _, a := range actions {
		if s.actions[a] {
			return true
		}
	}
	return false
}

// pkg and dir are where the controller lives: the root's controllers
// package, or the namespace's own.
func (s scaffold) pkg() string { return controller{ns: s.ns}.pkg() }
func (s scaffold) dir() string { return controller{ns: s.ns}.dir() }

// url is the path the resource answers at: /posts, or /admin/posts.
func (s scaffold) url() string { return controller{ns: s.ns, name: s.res}.prefix() }

// viewsPath is the file of the wire shapes.
func (s scaffold) viewsPath() string { return ViewsPath(s.name) }

func (s scaffold) typ() string  { return Camel(s.name) }  // Post
func (s scaffold) many() string { return Camel(s.table) } // Posts
func (s scaffold) ctl() string  { return Camel(s.res) + "Controller" }
func (s scaffold) storeIface() string {
	return lowerCamel(s.table) + "Store"
}

// viewType is one type of the views file, with the imports it needs.
type viewType struct {
	name, src string
	imports   []string
}

// viewTypes are the wire shapes the actions asked for need: the request body
// for create and update, one response for anything that returns a record, and
// the listing for index. Nothing here imports the domain; the controller
// converts. Destroy alone answers with no body, so it needs none.
func (s scaffold) viewTypes() []viewType {
	attrImports := map[string]bool{}
	for _, a := range s.attrs {
		for _, imp := range kindFor(a, s.scheme).imports {
			attrImports[imp] = true
		}
	}
	withAttrImports := func(extra ...string) []string {
		all := map[string]bool{}
		for imp := range attrImports {
			all[imp] = true
		}
		for _, imp := range extra {
			all[imp] = true
		}
		return sortedKeys(all)
	}

	width := len("UpdatedAt")
	for _, a := range s.attrs {
		if n := len(a.goName()); n > width {
			width = n
		}
	}
	field := func(a Attr) string {
		return fmt.Sprintf("\t%-*s %s `json:%q`\n", width, a.goName(), a.domainType(s.scheme), a.Name)
	}

	var out []viewType
	if s.has("create", "update") {
		var of string
		switch {
		case s.has("create") && s.has("update"):
			of = fmt.Sprintf("POST /%s and PATCH /%s/:id", s.res, s.res)
		case s.has("create"):
			of = "POST /" + s.res
		default:
			of = fmt.Sprintf("PATCH /%s/:id", s.res)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "// %sRequest is the body of %s.\n", s.typ(), of)
		fmt.Fprintf(&b, "type %sRequest struct {\n", s.typ())
		for _, a := range s.attrs {
			b.WriteString(field(a))
		}
		b.WriteString("}\n")
		out = append(out, viewType{s.typ() + "Request", b.String(), withAttrImports()})
	}
	if s.has("index", "show", "create", "update") {
		var b strings.Builder
		fmt.Fprintf(&b, "// %sResponse is one %s on the wire.\n", s.typ(), s.name)
		fmt.Fprintf(&b, "type %sResponse struct {\n", s.typ())
		fmt.Fprintf(&b, "\t%-*s %s `json:\"id\"`\n", width, "ID", s.scheme.idDomain())
		for _, a := range s.attrs {
			b.WriteString(field(a))
		}
		fmt.Fprintf(&b, "\t%-*s time.Time `json:\"created_at\"`\n", width, "CreatedAt")
		fmt.Fprintf(&b, "\t%-*s time.Time `json:\"updated_at\"`\n", width, "UpdatedAt")
		b.WriteString("}\n")
		out = append(out, viewType{s.typ() + "Response", b.String(), withAttrImports("time")})
	}
	if s.has("index") {
		src := fmt.Sprintf("// %sResponse is a listing.\ntype %sResponse struct {\n\t%s []%sResponse `json:%q`\n}\n", s.many(), s.many(), s.many(), s.typ(), s.table)
		out = append(out, viewType{s.many() + "Response", src, nil})
	}
	return out
}

// views is app/views/<table>.go, or "" when no action needs a wire shape.
func (s scaffold) views() string {
	types := s.viewTypes()
	if len(types) == 0 {
		return ""
	}
	imports := map[string]bool{}
	var srcs []string
	for _, t := range types {
		for _, imp := range t.imports {
			imports[imp] = true
		}
		srcs = append(srcs, t.src)
	}
	var b strings.Builder
	b.WriteString("package views\n\n")
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, imp := range sortedKeys(imports) {
			fmt.Fprintf(&b, "\t%q\n", imp)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(strings.Join(srcs, "\n"))
	return b.String()
}

// MergeViews adds to a model's existing views file the wire shapes the
// actions need that it does not declare yet, and returns the new text and
// the names it added. A namespaced controller shares the root controller's
// types, so the file is the model's and grows as controllers over it do; a
// type already there is left exactly as it is, hand-edited or not.
func MergeViews(existing []byte, module, name string, attrs []Attr, scheme IDScheme, only []string) ([]byte, []string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", existing, 0)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]bool{}
	for _, d := range file.Decls {
		if g, ok := d.(*ast.GenDecl); ok {
			for _, sp := range g.Specs {
				if t, ok := sp.(*ast.TypeSpec); ok {
					have[t.Name.Name] = true
				}
			}
		}
	}
	s, err := newScaffold(module, "", "", name, attrs, scheme, only)
	if err != nil {
		return nil, nil, err
	}
	out := string(existing)
	var added []string
	for _, t := range s.viewTypes() {
		if have[t.name] {
			continue
		}
		for _, imp := range t.imports {
			if !strings.Contains(out, fmt.Sprintf("%q", imp)) {
				out = addImport(out, imp)
			}
		}
		out = strings.TrimRight(out, "\n") + "\n\n" + t.src
		added = append(added, t.name)
	}
	if len(added) == 0 {
		return existing, nil, nil
	}
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return nil, nil, fmt.Errorf("the merged views file does not parse: %w", err)
	}
	return formatted, added, nil
}

// addImport puts an import in a file that lacks it: into its import block, or
// as a new one after the package clause. gofmt sorts it into place.
func addImport(src, path string) string {
	spec := fmt.Sprintf("\t%q\n", path)
	if i := strings.Index(src, "import (\n"); i >= 0 {
		i += len("import (\n")
		return src[:i] + spec + src[i:]
	}
	if i := strings.Index(src, "\nimport "); i >= 0 {
		end := i + 1 + strings.Index(src[i+1:], "\n")
		single := strings.TrimPrefix(src[i+1:end], "import ")
		return src[:i+1] + "import (\n\t" + single + "\n" + spec + ")" + src[end:]
	}
	end := strings.Index(src, "\n") + 1
	return src[:end] + "\nimport (\n" + spec + ")\n" + src[end:]
}

// controller is app/controllers/<table>_controller.go: the store interface
// it consumes, the type, its routes, the resourceful actions, the
// error mapping, and the domain-to-wire conversions. Only the actions asked
// for are written, and with them only the store methods, routes and
// conversions they use.
func (s scaffold) controller() string {
	typ, many, ctl, iface := s.typ(), s.many(), s.ctl(), s.storeIface()
	recv := paramName(s.name, "c")
	one := s.has("show", "create", "update") // a record on the wire
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w(`package %[3]s

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	%[1]q
	%[2]q
)

// %[4]s is the slice of the store this controller uses, declared next to
// its caller and holding only the methods the actions call.
// *adapter.Store satisfies it, and app/application.go passes it in.
type %[4]s interface {
`, s.module+"/app/models", s.module+"/app/views", s.pkg(), iface)
	if s.has("create") {
		w("\tCreate%[1]s(ctx context.Context, %[2]s models.%[1]s) (models.%[1]s, error)\n", typ, recv)
	}
	if s.has("show", "update") {
		w("\tGet%s(ctx context.Context, id string) (models.%s, error)\n", typ, typ)
	}
	if s.has("index") {
		w("\tList%s(ctx context.Context, limit int32) ([]models.%s, error)\n", many, typ)
	}
	if s.has("update") {
		w("\tUpdate%[1]s(ctx context.Context, %[2]s models.%[1]s) (models.%[1]s, error)\n", typ, recv)
	}
	if s.has("destroy") {
		w("\tDelete%s(ctx context.Context, id string) error\n", typ)
	}
	w("}\n\n")
	if s.has("index") {
		w(`// %[1]sListLimit is how many a listing returns, newest first. Paging is
// yours to add when a client needs more.
const %[1]sListLimit = 100

`, lowerCamel(s.table))
	}
	w(`// %[1]s is the HTTP surface of %[2]s: a JSON resource over the store. A
// handler converts HTTP into domain values, calls one store method, and
// converts the result back. Validation is the domain's and runs on save,
// which is why a bad value is a 422 from the store and not a binding rule
// here. Nothing below app/controllers sees *gin.Context.
type %[1]s struct {
	Store %[3]s
	Log   *slog.Logger
}

// New%[1]s builds the controller.
func New%[1]s(store %[3]s, log *slog.Logger) *%[1]s {
	return &%[1]s{Store: store, Log: log}
}

// SetupRoutes mounts the resource under its own prefix. Middleware the whole
// controller needs goes here; which group it sits in, and so how it is
// exposed, is decided by routes.go, which calls this.
func (ctl *%[1]s) SetupRoutes(router *gin.RouterGroup) {
	%[4]s := router.Group("/%[2]s")
`, ctl, s.res, iface, lowerCamel(s.res))
	for _, a := range []string{"index", "create", "show", "update", "destroy"} {
		if !s.has(a) {
			continue
		}
		r := resourceful[a]
		w("\t%s.%s(%q, ctl.%s)\n", lowerCamel(s.res), r.method, r.path, Camel(a))
	}
	w("}\n\n")

	// Handlers, in the order the actions are listed.
	if s.has("index") {
		w(`// Index handles GET %[3]s.
func (ctl *%[4]s) Index(c *gin.Context) {
	%[1]s, err := ctl.Store.List%[2]s(c.Request.Context(), %[5]sListLimit)
	if err != nil {
		ctl.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, %[5]sView(%[1]s))
}

`, lowerCamel(s.table), many, s.url(), ctl, lowerCamel(s.table))
	}
	if s.has("show") {
		w(`// Show handles GET %[1]s/:id.
func (ctl *%[2]s) Show(c *gin.Context) {
	%[3]s, err := ctl.Store.Get%[4]s(c.Request.Context(), c.Param("id"))
	if err != nil {
		ctl.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, %[5]sView(%[3]s))
}

`, s.url(), ctl, recv, typ, lowerCamel(s.name))
	}

	// Create and Update bind the request body, when there is one to bind.
	if s.has("create") {
		w("// Create handles POST %s.\n", s.url())
		w("func (ctl *%s) Create(c *gin.Context) {\n", ctl)
		b.WriteString(s.bind())
		w("\t%s, err := ctl.Store.Create%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest(""))
		w("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n\tc.JSON(http.StatusCreated, %sView(%s))\n}\n\n", lowerCamel(s.name), recv)
	}

	if s.has("update") {
		w("// Update handles PATCH %s/:id.", s.url())
		if len(s.attrs) == 0 {
			// Nothing to bind, but the id still has to be resolved to the
			// domain's id type (not every scheme's id is a string), so this
			// loads the record the same way the attributed case does and saves
			// it back unchanged but for updated_at.
			w("\nfunc (ctl *%s) Update(c *gin.Context) {\n", ctl)
			w("\t%s, err := ctl.Store.Get%s(c.Request.Context(), c.Param(\"id\"))\n", recv, typ)
			b.WriteString("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n")
			w("\t%s, err = ctl.Store.Update%s(c.Request.Context(), %s)\n", recv, typ, recv)
		} else {
			// Find, then update: load the record, decode the body
			// over its current values so a key left out keeps its value, then
			// save, so validation sees the whole record. The id comes from the
			// loaded record, not the URL, so it is already the domain's id type.
			w(" Only the fields in the body change: the %s is loaded,\n", s.name)
			b.WriteString("// the body is decoded over its current values,\n")
			b.WriteString("// so a key left out keeps its value, and the result is saved and validated\n// whole.\n")
			w("func (ctl *%s) Update(c *gin.Context) {\n", ctl)
			w("\t%s, err := ctl.Store.Get%s(c.Request.Context(), c.Param(\"id\"))\n", recv, typ)
			b.WriteString("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n")
			w("\treq := %s\n", s.toRequest(recv))
			b.WriteString("\tif err := c.ShouldBindJSON(&req); err != nil {\n\t\tc.JSON(http.StatusBadRequest, views.Error(err))\n\t\treturn\n\t}\n")
			w("\t%s, err = ctl.Store.Update%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest(recv+".ID"))
		}
		w("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n\tc.JSON(http.StatusOK, %sView(%s))\n}\n\n", lowerCamel(s.name), recv)
	}

	if s.has("destroy") {
		w(`// Destroy handles DELETE %[1]s/:id.
func (ctl *%[2]s) Destroy(c *gin.Context) {
	if err := ctl.Store.Delete%[3]s(c.Request.Context(), c.Param("id")); err != nil {
		ctl.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

`, s.url(), ctl, typ)
	}

	w(`// fail maps a domain error to a status, the one place that mapping lives:
// not found is 404, a validation failure is 422, and anything
// else is logged and answered 500 without leaking its text.
func (ctl *%[2]s) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, models.ErrNotFound):
		c.JSON(http.StatusNotFound, views.ErrorMessage("no such %[4]s"))
	case errors.Is(err, models.ErrInvalid):
		c.JSON(http.StatusUnprocessableEntity, views.Error(err))
	default:
		ctl.Log.ErrorContext(c.Request.Context(), "%[1]s: request failed",
			"path", c.FullPath(), "err", err, "request_id", c.GetString("request_id"))
		c.JSON(http.StatusInternalServerError, views.ErrorMessage("internal error"))
	}
}
`, s.table, ctl, typ, s.name)

	if !one && !s.has("index") {
		return b.String()
	}
	w(`
// The controller converts between the domain and the wire. views never
// imports domain, so this is where %[1]sResponse is built.

`, typ)
	// One record on the wire: index builds a listing out of them.
	w("func %sView(%s models.%s) views.%sResponse {\n\treturn views.%sResponse{\n", lowerCamel(s.name), recv, typ, typ, typ)
	width := len("CreatedAt:")
	for _, a := range s.attrs {
		if n := len(a.goName()) + 1; n > width {
			width = n
		}
	}
	w("\t\t%-*s %s.ID,\n", width, "ID:", recv)
	for _, a := range s.attrs {
		w("\t\t%-*s %s.%s,\n", width, a.goName()+":", recv, a.goName())
	}
	w("\t\t%-*s %s.CreatedAt,\n\t\t%-*s %s.UpdatedAt,\n\t}\n}\n", width, "CreatedAt:", recv, width, "UpdatedAt:", recv)

	if s.has("index") {
		w("\nfunc %sView(%s []models.%s) views.%sResponse {\n", lowerCamel(s.table), lowerCamel(s.table), typ, many)
		w("\tout := views.%sResponse{%s: make([]views.%sResponse, 0, len(%s))}\n", many, many, typ, lowerCamel(s.table))
		w("\tfor _, %s := range %s {\n\t\tout.%s = append(out.%s, %sView(%s))\n\t}\n\treturn out\n}\n", recv, lowerCamel(s.table), many, many, lowerCamel(s.name), recv)
	}
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
	return fmt.Sprintf("models.%s{%s}", s.typ(), strings.Join(parts, ", "))
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
func edited(a Attr, scheme IDScheme) (jsonValue, goLiteral string) {
	switch a.Type {
	case "string", "text":
		return `"Edited"`, `"Edited"`
	case "integer", "bigint":
		return "8", "8"
	case "boolean":
		return "false", "false"
	case "datetime", "jsonb":
		return "", ""
	case "uuid":
		return `"00000000-0000-4000-8000-000000000001"`, `"00000000-0000-4000-8000-000000000001"`
	default: // references: whatever this scheme's foreign keys are made of
		if scheme == IDUUID {
			return `"00000000-0000-4000-8000-000000000001"`, `"00000000-0000-4000-8000-000000000001"`
		}
		return "2", "2"
	}
}

// controllerTest is the controller's request spec, beside the controller in
// package controllers_test: the real router, controller and store on the
// test database, records made with the model's factory. The suite provides
// app. A model with a references column cannot be saved without its parent,
// which bogie cannot know a factory for, so its specs stop at those that need
// no saved record, and say so.
func (s scaffold) controllerTest() string {
	typ, many, table := s.typ(), s.many(), s.table
	hasRefs := hasReferences(s.attrs)
	missing := s.scheme.sampleID()
	factory := "factories." + typ + "(GinkgoT(), app.Store)"
	url := s.url()
	pathOf := fmt.Sprintf(`"%s/" + fmt.Sprint(record.ID)`, url)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	// section starts the Describe of one action, a blank line after the last.
	section := func(action string) bool {
		if !s.has(action) {
			return false
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		return true
	}

	// GET /things
	if section("index") {
		w("var _ = Describe(\"GET %s\", func() {\n", url)
		w("\tContext(\"with no %s\", func() {\n\t\tIt(\"is an empty list\", func() {\n\t\t\tapp.Get(\"%s\").Do().Status(http.StatusOK).Contains(\"\\\"%s\\\":[]\")\n\t\t})\n\t})\n", table, url, table)
		if !hasRefs {
			w(`
	Context("with %[1]s", func() {
		var first, second models.%[2]s

		BeforeEach(func() {
			first = %[4]s
			second = %[4]s
		})

		It("lists them, newest first", func() {
			var list views.%[3]sResponse
			app.Get("%[5]s").Do().Status(http.StatusOK).JSON(&list)

			Expect(list.%[3]s).To(HaveLen(2))
			Expect(list.%[3]s[0].ID).To(Equal(second.ID))
			Expect(list.%[3]s[1].ID).To(Equal(first.ID))
		})
	})
`, table, typ, many, factory, url)
		}
		w("})\n")
	}

	// GET /things/:id
	if section("show") {
		w("var _ = Describe(\"GET %s/:id\", func() {\n", url)
		if !hasRefs {
			w(`	Context("when the %[1]s exists", func() {
		var record models.%[2]s

		BeforeEach(func() {
			record = %[3]s
		})

		It("shows it", func() {
			var shown views.%[2]sResponse
			app.Get(%[4]s).Do().Status(http.StatusOK).JSON(&shown)

			Expect(shown.ID).To(Equal(record.ID))
		})
	})

`, s.name, typ, factory, pathOf)
		}
		w(`	Context("when the %[1]s does not exist", func() {
		It("is not found", func() {
			app.Get("%[2]s/%[3]s").Do().Status(http.StatusNotFound)
		})
	})
})
`, s.name, url, missing)
	}

	// POST /things
	if section("create") {
		w("var _ = Describe(\"POST %s\", func() {\n", url)
		if len(s.attrs) == 0 {
			w(`	Context("with no attributes to send", func() {
		It("creates one", func() {
			app.Post("%s", nil).Do().Status(http.StatusCreated)
		})
	})
`, url)
		} else {
			if !hasRefs {
				w("\tvar body views.%sRequest\n\n", typ)
				w(`	Context("with a valid body", func() {
		BeforeEach(func() {
			body = %[1]sRequest(factories.New%[2]s())
		})

		It("creates it", func() {
			var created views.%[2]sResponse
			app.Post("%[3]s", body).Do().Status(http.StatusCreated).JSON(&created)

			Expect(created.ID).NotTo(BeZero())
			_, err := app.Store.Get%[2]s(GinkgoT().Context(), fmt.Sprint(created.ID))
			Expect(err).NotTo(HaveOccurred())
		})
	})
`, lowerCamel(s.name), typ, url)
				if uniq := uniqueAttrs(s.attrs); len(uniq) > 0 {
					names := make([]string, len(uniq))
					for i, a := range uniq {
						names[i] = a.Name
					}
					w(`
	Context("when %[1]s with the same %[2]s exists", func() {
		BeforeEach(func() {
			body = %[3]sRequest(%[4]s)
		})

		It("is unprocessable", func() {
			app.Post("%[5]s", body).Do().Status(http.StatusUnprocessableEntity)
		})
	})
`, withArticle(s.name), strings.Join(names, " and "), lowerCamel(s.name), factory, url)
				}
				w("\n")
			}
			w(`	Context("with a malformed body", func() {
		It("is a bad request", func() {
			app.Post("%s", "{\"nope\":").Do().Status(http.StatusBadRequest)
		})
	})
`, url)
		}
		w("})\n")
	}

	// PATCH /things/:id
	var sent *Attr
	var sentLit string
	for i, a := range s.attrs {
		if _, lit := edited(a, s.scheme); lit != "" && a.Type != "references" && a.Type != "uuid" {
			sent, sentLit = &s.attrs[i], lit
			break
		}
	}
	if section("update") {
		w("var _ = Describe(\"PATCH %s/:id\", func() {\n", url)
		if !hasRefs {
			w(`	Context("when the %[1]s exists", func() {
		var record models.%[2]s

		BeforeEach(func() {
			record = %[3]s
		})

`, s.name, typ, factory)
			if sent != nil {
				// PATCH is partial: a field the body leaves out keeps
				// its stored value rather than being blanked.
				w("\t\tIt(\"changes only the fields sent\", func() {\n\t\t\tvar updated views.%sResponse\n", typ)
				w("\t\t\tapp.Patch(%s, map[string]any{%q: %s}).Do().Status(http.StatusOK).JSON(&updated)\n\n", pathOf, sent.Name, sentLit)
				w("\t\t\tExpect(updated.%s).To(BeEquivalentTo(%s))\n", sent.goName(), sentLit)
				for _, a := range s.attrs {
					if a.Name == sent.Name {
						continue
					}
					switch a.Type {
					case "datetime", "jsonb":
					default:
						w("\t\t\tExpect(updated.%[1]s).To(Equal(record.%[1]s))\n", a.goName())
					}
				}
				w("\t\t})\n")
			} else {
				w(`		It("keeps it as it is when nothing is sent", func() {
			var updated views.%[1]sResponse
			app.Patch(%[2]s, map[string]any{}).Do().Status(http.StatusOK).JSON(&updated)

			Expect(updated.ID).To(Equal(record.ID))
		})
`, typ, pathOf)
			}
			if len(s.attrs) > 0 {
				w(`
		Context("with a malformed body", func() {
			It("is a bad request", func() {
				app.Patch(%s, "{\"nope\":").Do().Status(http.StatusBadRequest)
			})
		})
`, pathOf)
			}
			w("\t})\n\n")
		}
		w(`	Context("when the %[1]s does not exist", func() {
		It("is not found", func() {
			app.Patch("%[2]s/%[3]s", map[string]any{}).Do().Status(http.StatusNotFound)
		})
	})
})
`, s.name, url, missing)
	}

	// DELETE /things/:id
	if section("destroy") {
		w("var _ = Describe(\"DELETE %s/:id\", func() {\n", url)
		if !hasRefs {
			w(`	Context("when the %[1]s exists", func() {
		var record models.%[2]s

		BeforeEach(func() {
			record = %[3]s
		})

		It("deletes it", func() {
			app.Delete(%[4]s).Do().Status(http.StatusNoContent)

			app.Get(%[4]s).Do().Status(http.StatusNotFound)
		})
	})

`, s.name, typ, factory, pathOf)
		}
		w(`	Context("when the %[1]s does not exist", func() {
		It("is not found", func() {
			app.Delete("%[2]s/%[3]s").Do().Status(http.StatusNotFound)
		})
	})
})
`, s.name, url, missing)
	}

	if hasRefs {
		w(`
// The specs that save %[1]s are left out: it belongs to a parent that has to
// be saved first. Make the parent with its factory, set the reference in the
// "with" function of factories.%[2]s, and add them.
`, withArticle(s.name), typ)
	} else if len(s.attrs) > 0 && s.has("create") {
		w("\n// %[1]sRequest is the body that creates a record like this one.\nfunc %[1]sRequest(record models.%[2]s) views.%[2]sRequest {\n\treturn views.%[2]sRequest{", lowerCamel(s.name), typ)
		for i, a := range s.attrs {
			if i > 0 {
				w(", ")
			}
			w("%[1]s: record.%[1]s", a.goName())
		}
		w("}\n}\n")
	}

	// What the specs use decides what is imported.
	var code strings.Builder
	for _, line := range strings.Split(b.String(), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			code.WriteString(line + "\n")
		}
	}
	uses := func(sym string) bool { return strings.Contains(code.String(), sym) }
	var head strings.Builder
	fmt.Fprintf(&head, "package %s_test\n\nimport (\n", s.pkg())
	if uses("fmt.") {
		head.WriteString("\t\"fmt\"\n")
	}
	head.WriteString("\t\"net/http\"\n\n\t. \"github.com/onsi/ginkgo/v2\"\n")
	if uses("Expect(") {
		head.WriteString("\t. \"github.com/onsi/gomega\"\n")
	}
	var own []string
	for _, imp := range []struct{ sym, path string }{{"models.", "/app/models"}, {"views.", "/app/views"}, {"factories.", "/test/factories"}} {
		if uses(imp.sym) {
			own = append(own, fmt.Sprintf("\t%q\n", s.module+imp.path))
		}
	}
	if len(own) > 0 {
		head.WriteString("\n" + strings.Join(own, ""))
	}
	head.WriteString(")\n\n")
	return head.String() + b.String()
}
