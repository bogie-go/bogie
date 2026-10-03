package generate

import (
	"fmt"
	"strings"
)

// Rails's resourceful actions and the routes they take. Anything else is a
// GET under its own name, as `rails g controller posts search` gives
// `get "posts/search"`. new and edit render forms and have no place in an
// API-only service, so they are refused.
var resourceful = map[string]struct{ method, path string }{
	"index":   {"GET", ""},
	"show":    {"GET", "/:id"},
	"create":  {"POST", ""},
	"update":  {"PATCH", "/:id"},
	"destroy": {"DELETE", "/:id"},
}

// Wire is one line a generator registers in a hand-written file, above a
// marker, with the import the line needs.
type Wire struct {
	File   string // relative to the app
	Marker string
	Line   string
	Import string
}

// ParseControllerName splits what `rails g controller` accepts, posts,
// admin/posts or Admin::Posts, into a namespace (empty for none) and a name,
// both snake_case. One level of namespace, as docs/DESIGN.md §5 decides.
func ParseControllerName(spec string) (ns, name string, err error) {
	parts := strings.Split(strings.ReplaceAll(spec, "::", "/"), "/")
	for i, p := range parts {
		parts[i] = Snake(p)
	}
	switch len(parts) {
	case 1:
		name = parts[0]
	case 2:
		ns, name = parts[0], parts[1]
		if err := checkNamespace(ns); err != nil {
			return "", "", err
		}
	default:
		return "", "", fmt.Errorf("%q nests namespaces; one level (admin/posts) is what v1 supports", spec)
	}
	if !namePattern.MatchString(name) {
		return "", "", fmt.Errorf("%q is not a controller name: lowercase letters, digits and underscores", name)
	}
	return ns, name, nil
}

// Controller writes one Rails controller as one Go file: a <Name>Controller
// type holding its dependencies, SetupRoutes, one method per action, and a
// test that every route is mounted. With no namespace it goes in the
// controllers package as app/controllers/<name>_controller.go; with one, in
// app/controllers/<ns>_controller/<name>_controller.go, the package Namespace
// writes. Either way it returns the three lines that register it
// (docs/DESIGN.md §5).
func Controller(module, ns, name string, actions []string) ([]File, []Wire, error) {
	if ns != "" {
		if err := checkNamespace(ns); err != nil {
			return nil, nil, err
		}
	}
	if !namePattern.MatchString(name) {
		return nil, nil, fmt.Errorf("%q is not a controller name: lowercase letters, digits and underscores", name)
	}
	seen := map[string]bool{}
	for _, a := range actions {
		if a == "new" || a == "edit" {
			return nil, nil, fmt.Errorf("%s renders a form; this is an API-only service, so there is no %s action", a, a)
		}
		if !namePattern.MatchString(a) {
			return nil, nil, fmt.Errorf("%q is not an action name", a)
		}
		if seen[a] {
			return nil, nil, fmt.Errorf("action %s given twice", a)
		}
		seen[a] = true
	}

	c := controller{module: module, ns: ns, name: name, actions: actions}
	files := []File{
		{Path: c.dir() + name + "_controller.go", Content: c.source()},
		{Path: c.dir() + name + "_controller_test.go", Content: c.test()},
	}
	return files, c.wires(), nil
}

func checkNamespace(ns string) error {
	if !namePattern.MatchString(ns) {
		return fmt.Errorf("%q is not a namespace: lowercase letters, digits and underscores", ns)
	}
	return nil
}

// Namespace writes the package a namespaced controller lives in:
// app/controllers/<ns>_controller/ (Rails's app/controllers/admin/, with the
// suffix Go needs so that admin never collides with another import in
// app/application.go, and internal is not Go's internal) with server.go
// (the namespace Server, one named field per controller above its own
// marker) and routes.go (the group every controller in it mounts under,
// above its own marker), and the three root lines that register the
// namespace. The command calls it once, when the package does not exist yet.
func Namespace(module, ns string) ([]File, []Wire, error) {
	if err := checkNamespace(ns); err != nil {
		return nil, nil, err
	}
	pkg := ns + "_controller"
	dir := "app/controllers/" + pkg + "/"
	field := Camel(ns)
	importPath := module + "/app/controllers/" + pkg

	files := []File{
		{Path: dir + "server.go", Content: namespaceServer(pkg, ns)},
		{Path: dir + "routes.go", Content: namespaceRoutes(pkg, ns)},
	}
	wires := []Wire{
		{File: "app/controllers/application.go", Marker: "controllers", Line: fmt.Sprintf("%s *%s.Server", field, pkg), Import: importPath},
		{File: "app/application.go", Marker: "wire", Line: fmt.Sprintf("server.%s = %s.NewServer(log)", field, pkg), Import: importPath},
		{File: "app/controllers/routes.go", Marker: "routes", Line: fmt.Sprintf("s.%s.SetupRoutes(&r.RouterGroup)", field)},
	}
	return files, wires, nil
}

// WirePrefixes are what destroy removes: the start of each line Controller
// inserted, whatever actions it was given.
func WirePrefixes(ns, name string) []Wire {
	c := controller{ns: ns, name: name}
	field := Camel(name)
	if ns == "" {
		return []Wire{
			{File: "app/controllers/application.go", Line: c.typeField()},
			{File: "app/application.go", Line: "server." + field + " ="},
			{File: "app/controllers/routes.go", Line: "s." + field + ".SetupRoutes("},
		}
	}
	return []Wire{
		{File: c.dir() + "server.go", Line: c.typeField()},
		{File: "app/application.go", Line: "server." + Camel(ns) + "." + field + " ="},
		{File: c.dir() + "routes.go", Line: "s." + field + ".SetupRoutes("},
	}
}

// NamespaceWirePrefixes are what destroy removes once a namespace holds no
// controller: the start of each line Namespace inserted.
func NamespaceWirePrefixes(ns string) []Wire {
	field := Camel(ns)
	return []Wire{
		{File: "app/controllers/application.go", Line: field + " *" + ns + "_controller.Server"},
		{File: "app/application.go", Line: "server." + field + " ="},
		{File: "app/controllers/routes.go", Line: "s." + field + ".SetupRoutes("},
	}
}

// LooksLikeASingularResource reports whether a controller name is singular
// while its actions address a member by id (show, update, destroy): the
// shape of a resource, which Rails names in the plural. index alone does
// not count, since welcome or search are whole controllers of their own.
func LooksLikeASingularResource(name string, actions []string) bool {
	if Singular(name) != name {
		return false
	}
	for _, a := range actions {
		if r, ok := resourceful[a]; ok && strings.Contains(r.path, ":id") {
			return true
		}
	}
	return false
}

// controller is one Rails controller: the names every file and line derive
// from, computed once.
type controller struct {
	module, ns, name string
	actions          []string
}

func (c controller) pkg() string {
	if c.ns == "" {
		return "controllers"
	}
	return c.ns + "_controller"
}

func (c controller) dir() string {
	if c.ns == "" {
		return "app/controllers/"
	}
	return "app/controllers/" + c.ns + "_controller/"
}

func (c controller) typeName() string { return Camel(c.name) + "Controller" }
func (c controller) typeField() string {
	return Camel(c.name) + " *" + c.typeName()
}

// prefix is the URL every action sits under, for the comments.
func (c controller) prefix() string {
	if c.ns == "" {
		return "/" + c.name
	}
	return "/" + c.ns + "/" + c.name
}

// wires are the three lines that register the controller: a field on the
// Server that holds it (the root's, or the namespace's), its construction in
// app/application.go, and its mount in the routes file of the same Server.
func (c controller) wires() []Wire {
	field := Camel(c.name)
	if c.ns == "" {
		return []Wire{
			{File: "app/controllers/application.go", Marker: "controllers", Line: c.typeField()},
			{File: "app/application.go", Marker: "wire", Line: fmt.Sprintf("server.%s = controllers.New%s(log)", field, c.typeName()), Import: c.module + "/app/controllers"},
			{File: "app/controllers/routes.go", Marker: "routes", Line: fmt.Sprintf("s.%s.SetupRoutes(&r.RouterGroup)", field)},
		}
	}
	nsField := Camel(c.ns)
	return []Wire{
		{File: c.dir() + "server.go", Marker: "controllers", Line: c.typeField()},
		{File: "app/application.go", Marker: "wire", Line: fmt.Sprintf("server.%s.%s = %s.New%s(log)", nsField, field, c.pkg(), c.typeName()), Import: c.module + "/app/controllers/" + c.pkg()},
		{File: c.dir() + "routes.go", Marker: "routes", Line: fmt.Sprintf("s.%s.SetupRoutes(%s)", field, c.ns)},
	}
}

func (c controller) source() string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", c.pkg())
	if len(c.actions) == 0 {
		b.WriteString("import (\n\t\"log/slog\"\n\n\t\"github.com/gin-gonic/gin\"\n)\n\n")
	} else {
		fmt.Fprintf(&b, "import (\n\t\"log/slog\"\n\t\"net/http\"\n\n\t\"github.com/gin-gonic/gin\"\n\n\t%q\n)\n\n", c.module+"/app/views")
	}

	fmt.Fprintf(&b, `// %[1]s is the HTTP surface of %[2]s: its dependencies, its routes
// and its actions, one Rails controller in one file. Dependencies are small
// interfaces declared here (rule 2), holding only the methods the actions
// call, and the real thing is passed in from app/application.go, where this
// controller is wired:
//
//	type %[3]sService interface {
//		Get(ctx context.Context, id string) (domain.Thing, error)
//	}
type %[1]s struct {
	Log *slog.Logger
}

// New%[1]s builds the controller.
func New%[1]s(log *slog.Logger) *%[1]s {
	return &%[1]s{Log: log}
}

// SetupRoutes mounts the controller under its own prefix. Middleware the
// whole controller needs goes here; which group it sits in, and so how it is
// exposed, is decided by the routes.go that calls this.
func (ctl *%[1]s) SetupRoutes(router *gin.RouterGroup) {
	%[2]s := router.Group("/%[2]s")
`, c.typeName(), c.name, lowerCamel(c.name))
	if len(c.actions) == 0 {
		fmt.Fprintf(&b, "\t_ = %s // no actions yet; add them below and mount them here\n", c.name)
	}
	for _, a := range c.actions {
		method, path := routeFor(a)
		fmt.Fprintf(&b, "\t%s.%s(%q, ctl.%s)\n", c.name, method, path, Camel(a))
	}
	b.WriteString("}\n")

	for _, a := range c.actions {
		method, path := routeFor(a)
		fmt.Fprintf(&b, `
// %[1]s handles %[2]s %[3]s%[4]s.
//
// A handler converts HTTP into domain values, calls one service function,
// and converts the result back. Nothing below app/controllers sees
// *gin.Context (rule 5).
func (ctl *%[5]s) %[1]s(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, views.ErrorMessage("%[6]s#%[7]s is not implemented yet"))
}
`, Camel(a), method, c.prefix(), path, c.typeName(), c.name, a)
	}
	return b.String()
}

// methodConst is the net/http constant suffix for each verb.
var methodConst = map[string]string{"GET": "Get", "POST": "Post", "PATCH": "Patch", "DELETE": "Delete"}

func routeFor(action string) (method, path string) {
	if r, ok := resourceful[action]; ok {
		return r.method, r.path
	}
	return "GET", "/" + action
}

// test checks every action is mounted. A plain controller is mounted on a
// bare router; a namespaced one through its namespace Server, so the test
// also fails if the namespace's routes.go lost the mount line.
func (c controller) test() string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", c.pkg())
	b.WriteString("import (\n\t\"io\"\n\t\"log/slog\"\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n\n\t\"github.com/gin-gonic/gin\"\n)\n\n")
	b.WriteString("// Every action is mounted: a route that answers 404 was never registered.\n")
	fmt.Fprintf(&b, "func Test%sRoutesAreMounted(t *testing.T) {\n", Camel(c.name))
	b.WriteString("\tgin.SetMode(gin.TestMode)\n\trouter := gin.New()\n")
	b.WriteString("\tlog := slog.New(slog.NewTextHandler(io.Discard, nil))\n")
	if c.ns == "" {
		fmt.Fprintf(&b, "\tNew%s(log).SetupRoutes(&router.RouterGroup)\n\n", c.typeName())
	} else {
		b.WriteString("\tns := NewServer(log)\n")
		fmt.Fprintf(&b, "\tns.%s = New%s(log)\n", Camel(c.name), c.typeName())
		b.WriteString("\tns.SetupRoutes(&router.RouterGroup)\n\n")
	}
	b.WriteString("\troutes := []struct{ method, path string }{")
	if len(c.actions) == 0 {
		b.WriteString("} // none yet; list each action's route here as you add it\n")
	} else {
		b.WriteString("\n")
		for _, a := range c.actions {
			method, path := routeFor(a)
			fmt.Fprintf(&b, "\t\t{http.Method%s, \"%s%s\"},\n", methodConst[method], c.prefix(), strings.ReplaceAll(path, ":id", "1"))
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("\tfor _, r := range routes {\n")
	b.WriteString("\t\trec := httptest.NewRecorder()\n")
	b.WriteString("\t\trouter.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))\n")
	b.WriteString("\t\tif rec.Code == http.StatusNotFound {\n")
	b.WriteString("\t\t\tt.Errorf(\"%s %s is not mounted\", r.method, r.path)\n\t\t}\n\t}\n}\n")
	return b.String()
}

func namespaceServer(pkg, ns string) string {
	return fmt.Sprintf(`// Package %[1]s is the %[2]s namespace: the controllers mounted under
// /%[2]s, as Rails's "namespace :%[2]s" and the %[3]s:: module. One file per
// controller; the group and its shared middleware in routes.go.
package %[1]s

import (
	"log/slog"
)

// Server holds the namespace's controllers, one NAMED field each, so adding
// one is one line here, one in app/application.go and one in routes.go.
// "bogie g controller %[2]s/NAME" inserts all three.
type Server struct {
	Log *slog.Logger

	// bogie:controllers
}

// NewServer builds the namespace. Its controllers are constructed and
// assigned in app/application.go, where every dependency is wired.
func NewServer(log *slog.Logger) *Server {
	return &Server{Log: log}
}
`, pkg, ns, Camel(ns))
}

func namespaceRoutes(pkg, ns string) string {
	return fmt.Sprintf(`package %[1]s

import "github.com/gin-gonic/gin"

// SetupRoutes mounts every controller in the namespace under /%[2]s.
// Middleware the whole namespace shares, an auth check or a shared secret,
// goes on the group here, so the exposure boundary is visible in one place.
func (s *Server) SetupRoutes(router *gin.RouterGroup) {
	%[2]s := router.Group("/%[2]s")
	// bogie:routes
}
`, pkg, ns)
}
