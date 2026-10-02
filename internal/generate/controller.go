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

// Controller writes app/controllers/<name>_controller/: server.go, routes.go,
// one file per action, a test that every route is mounted, and the three
// lines that register it (docs/DESIGN.md §5).
func Controller(module, name string, actions []string) ([]File, []Wire, error) {
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

	pkg := name + "_controller"
	dir := "app/controllers/" + pkg + "/"
	field := Camel(name)
	importPath := module + "/app/controllers/" + pkg

	files := []File{
		{Path: dir + "server.go", Content: controllerServer(module, pkg, name)},
		{Path: dir + "routes.go", Content: controllerRoutes(pkg, name, actions)},
	}
	for _, a := range actions {
		files = append(files, File{Path: dir + a + ".go", Content: controllerAction(module, pkg, name, a)})
	}
	files = append(files, File{Path: dir + name + "_test.go", Content: controllerTest(pkg, name, actions)})

	wires := []Wire{
		{File: "app/controllers/application.go", Marker: "controllers", Line: fmt.Sprintf("%s *%s.Server", field, pkg), Import: importPath},
		{File: "app/application.go", Marker: "wire", Line: fmt.Sprintf("server.%s = %s.NewServer(log)", field, pkg), Import: importPath},
		{File: "app/controllers/routes.go", Marker: "routes", Line: fmt.Sprintf("s.%s.SetupRoutes(&r.RouterGroup)", field)},
	}
	return files, wires, nil
}

// WirePrefixes are what destroy removes: the start of each line Controller
// inserted, whatever arguments it was later given.
func WirePrefixes(name string) []Wire {
	field := Camel(name)
	pkg := name + "_controller"
	return []Wire{
		{File: "app/controllers/application.go", Line: field + " *" + pkg + ".Server"},
		{File: "app/application.go", Line: "server." + field + " ="},
		{File: "app/controllers/routes.go", Line: "s." + field + ".SetupRoutes("},
	}
}

func controllerServer(module, pkg, name string) string {
	return fmt.Sprintf(`// Package %[1]s is the HTTP surface of %[2]s. One file per action; the
// routes in routes.go; the dependencies here, as interfaces declared by this
// package (rule 2), holding only the methods this controller calls.
package %[1]s

import (
	"log/slog"
)

// Server holds the controller's dependencies. Add what the actions need as
// a small interface declared here, and pass the real thing in from
// app/application.go, where this controller is wired:
//
//	type service interface {
//		Get(ctx context.Context, id string) (domain.Thing, error)
//	}
type Server struct {
	Log *slog.Logger
}

// NewServer builds the controller.
func NewServer(log *slog.Logger) *Server {
	return &Server{Log: log}
}
`, pkg, name)
}

func controllerRoutes(pkg, name string, actions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nimport \"github.com/gin-gonic/gin\"\n\n", pkg)
	b.WriteString("// SetupRoutes mounts the controller under its own prefix. Middleware the\n")
	b.WriteString("// whole group needs goes here; the exposure boundary stays visible in\n")
	b.WriteString("// app/controllers/routes.go, which calls this.\n")
	b.WriteString("func (s *Server) SetupRoutes(router *gin.RouterGroup) {\n")
	fmt.Fprintf(&b, "\t%s := router.Group(\"/%s\")\n", name, name)
	if len(actions) == 0 {
		fmt.Fprintf(&b, "\t_ = %s // no actions yet; add them above and mount them here\n", name)
	}
	for _, a := range actions {
		method, path := routeFor(a)
		fmt.Fprintf(&b, "\t%s.%s(%q, s.%s)\n", name, method, path, Camel(a))
	}
	b.WriteString("}\n")
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

func controllerAction(module, pkg, name, action string) string {
	method, path := routeFor(action)
	return fmt.Sprintf(`package %[1]s

import (
	"net/http"

	"github.com/gin-gonic/gin"

	%[6]q
)

// %[2]s handles %[3]s /%[4]s%[5]s.
//
// A handler converts HTTP into domain values, calls one service function,
// and converts the result back. Nothing below app/controllers sees
// *gin.Context (rule 5).
func (s *Server) %[2]s(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, views.ErrorMessage("%[4]s#%[7]s is not implemented yet"))
}
`, pkg, Camel(action), method, name, path, module+"/app/views", action)
}

func controllerTest(pkg, name string, actions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import (\n\t\"io\"\n\t\"log/slog\"\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n\n\t\"github.com/gin-gonic/gin\"\n)\n\n")
	b.WriteString("// Every action is mounted: a route that answers 404 was never registered.\n")
	fmt.Fprintf(&b, "func TestRoutesAreMounted(t *testing.T) {\n")
	b.WriteString("\tgin.SetMode(gin.TestMode)\n\trouter := gin.New()\n")
	b.WriteString("\tNewServer(slog.New(slog.NewTextHandler(io.Discard, nil))).SetupRoutes(&router.RouterGroup)\n\n")
	b.WriteString("\troutes := []struct{ method, path string }{\n")
	for _, a := range actions {
		method, path := routeFor(a)
		fmt.Fprintf(&b, "\t\t{http.Method%s, \"/%s%s\"},\n", methodConst[method], name, strings.ReplaceAll(path, ":id", "1"))
	}
	b.WriteString("\t}\n\tfor _, r := range routes {\n")
	b.WriteString("\t\trec := httptest.NewRecorder()\n")
	b.WriteString("\t\trouter.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))\n")
	b.WriteString("\t\tif rec.Code == http.StatusNotFound {\n")
	b.WriteString("\t\t\tt.Errorf(\"%s %s is not mounted\", r.method, r.path)\n\t\t}\n\t}\n}\n")
	return b.String()
}
