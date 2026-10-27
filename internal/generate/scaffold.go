package generate

import (
	"fmt"
	"go/format"
	"strings"
	"time"
)

// Scaffold writes what `bogie generate scaffold post title:string body:text`
// produces: the model (everything Model writes),
// the wire shapes in app/views, and a controller with real handlers, a request
// test, and the three lines that register it with the store
// passed in. No service: the controller talks to the store, and the store
// validates on save, and a controller talks to the model (docs/DESIGN.md
// §5). The model is named in the singular and the rest in the plural.
func Scaffold(module, name string, attrs []Attr, scheme IDScheme, now time.Time) ([]File, []Wire, error) {
	files, err := Model(module, name, attrs, scheme, now)
	if err != nil {
		return nil, nil, err
	}
	table := Plural(name)
	s := scaffold{module: module, name: name, table: table, attrs: attrs, scheme: scheme}
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
func ScaffoldFiles(module, name string, scheme IDScheme) ([]File, error) {
	files, _, err := Scaffold(module, name, nil, scheme, time.Time{})
	return files, err
}

type scaffold struct {
	module, name, table string
	attrs               []Attr
	scheme              IDScheme
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
		for _, imp := range kindFor(a, s.scheme).imports {
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
		return fmt.Sprintf("\t%-*s %s `json:%q`\n", width, a.goName(), a.domainType(s.scheme), a.Name)
	}

	fmt.Fprintf(&b, "// %sRequest is the body of POST /%s and PATCH /%s/:id.\n", s.typ(), s.table, s.table)
	fmt.Fprintf(&b, "type %sRequest struct {\n", s.typ())
	for _, a := range s.attrs {
		b.WriteString(field(a))
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// %sResponse is one %s on the wire.\n", s.typ(), s.name)
	fmt.Fprintf(&b, "type %sResponse struct {\n", s.typ())
	fmt.Fprintf(&b, "\t%-*s %s `json:\"id\"`\n", width, "ID", s.scheme.idDomain())
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
	recv := paramName(s.name, "c")
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
// its caller and holding only the methods the actions call.
// *adapter.Store satisfies it, and app/application.go passes it in.
type %[3]s interface {
	Create%[4]s(ctx context.Context, %[5]s models.%[4]s) (models.%[4]s, error)
	Get%[4]s(ctx context.Context, id string) (models.%[4]s, error)
	List%[6]s(ctx context.Context, limit int32) ([]models.%[4]s, error)
	Update%[4]s(ctx context.Context, %[5]s models.%[4]s) (models.%[4]s, error)
	Delete%[4]s(ctx context.Context, id string) error
}

// %[7]sListLimit is how many a listing returns, newest first. Paging is
// yours to add when a client needs more.
const %[7]sListLimit = 100

// %[8]s is the HTTP surface of %[9]s: a JSON resource over the store. A
// handler converts HTTP into domain values, calls one store method, and
// converts the result back. Validation is the domain's and runs on save,
// which is why a bad value is a 422 from the store and not a binding rule
// here. Nothing below app/controllers sees *gin.Context.
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

`, s.module+"/app/models", s.module+"/app/views", iface, typ, recv, many, lowerCamel(s.table), ctl, s.table, lowerCamel(s.name))

	// Create and Update bind the request body, when there is one to bind.
	fmt.Fprintf(&b, "// Create handles POST /%s.\n", s.table)
	fmt.Fprintf(&b, "func (ctl *%s) Create(c *gin.Context) {\n", ctl)
	b.WriteString(s.bind())
	fmt.Fprintf(&b, "\t%s, err := ctl.Store.Create%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest(""))
	fmt.Fprintf(&b, "\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n\tc.JSON(http.StatusCreated, %sView(%s))\n}\n\n", lowerCamel(s.name), recv)

	fmt.Fprintf(&b, "// Update handles PATCH /%s/:id.", s.table)
	if len(s.attrs) == 0 {
		// Nothing to bind, but the id still has to be resolved to the
		// domain's id type (not every scheme's id is a string), so this
		// loads the record the same way the attributed case does and saves
		// it back unchanged but for updated_at.
		fmt.Fprintf(&b, "\nfunc (ctl *%s) Update(c *gin.Context) {\n", ctl)
		fmt.Fprintf(&b, "\t%s, err := ctl.Store.Get%s(c.Request.Context(), c.Param(\"id\"))\n", recv, typ)
		b.WriteString("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n")
		fmt.Fprintf(&b, "\t%s, err = ctl.Store.Update%s(c.Request.Context(), %s)\n", recv, typ, recv)
	} else {
		// Find, then update: load the record, decode the body
		// over its current values so a key left out keeps its value, then
		// save, so validation sees the whole record. The id comes from the
		// loaded record, not the URL, so it is already the domain's id type.
		fmt.Fprintf(&b, " Only the fields in the body change: the %s is loaded,\n", s.name)
		b.WriteString("// the body is decoded over its current values,\n")
		b.WriteString("// so a key left out keeps its value, and the result is saved and validated\n// whole.\n")
		fmt.Fprintf(&b, "func (ctl *%s) Update(c *gin.Context) {\n", ctl)
		fmt.Fprintf(&b, "\t%s, err := ctl.Store.Get%s(c.Request.Context(), c.Param(\"id\"))\n", recv, typ)
		b.WriteString("\tif err != nil {\n\t\tctl.fail(c, err)\n\t\treturn\n\t}\n")
		fmt.Fprintf(&b, "\treq := %s\n", s.toRequest(recv))
		b.WriteString("\tif err := c.ShouldBindJSON(&req); err != nil {\n\t\tc.JSON(http.StatusBadRequest, views.Error(err))\n\t\treturn\n\t}\n")
		fmt.Fprintf(&b, "\t%s, err = ctl.Store.Update%s(c.Request.Context(), %s)\n", recv, typ, s.fromRequest(recv+".ID"))
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

// The controller converts between the domain and the wire. views never
// imports domain, so this is where %[3]sResponse is built.

`, s.table, ctl, typ, s.name)

	fmt.Fprintf(&b, "func %sView(%s models.%s) views.%sResponse {\n\treturn views.%sResponse{\n", lowerCamel(s.name), recv, typ, typ, typ)
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

	fmt.Fprintf(&b, "func %sView(%s []models.%s) views.%sResponse {\n", lowerCamel(s.table), s.table, typ, many)
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
	pathOf := fmt.Sprintf(`"/%s/" + fmt.Sprint(record.ID)`, table)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	// GET /things
	w("var _ = Describe(\"GET /%s\", func() {\n", table)
	w("\tContext(\"with no %s\", func() {\n\t\tIt(\"is an empty list\", func() {\n\t\t\tapp.Get(\"/%s\").Do().Status(http.StatusOK).Contains(\"\\\"%s\\\":[]\")\n\t\t})\n\t})\n", table, table, table)
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
			app.Get("/%[1]s").Do().Status(http.StatusOK).JSON(&list)

			Expect(list.%[3]s).To(HaveLen(2))
			Expect(list.%[3]s[0].ID).To(Equal(second.ID))
			Expect(list.%[3]s[1].ID).To(Equal(first.ID))
		})
	})
`, table, typ, many, factory)
	}
	w("})\n\n")

	// GET /things/:id
	w("var _ = Describe(\"GET /%s/:id\", func() {\n", table)
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
			app.Get("/%[2]s/%[3]s").Do().Status(http.StatusNotFound)
		})
	})
})

`, s.name, table, missing)

	// POST /things
	w("var _ = Describe(\"POST /%s\", func() {\n", table)
	if len(s.attrs) == 0 {
		w(`	Context("with no attributes to send", func() {
		It("creates one", func() {
			app.Post("/%s", nil).Do().Status(http.StatusCreated)
		})
	})
`, table)
	} else {
		if !hasRefs {
			w("\tvar body views.%sRequest\n\n", typ)
			w(`	Context("with a valid body", func() {
		BeforeEach(func() {
			body = %[1]sRequest(factories.New%[2]s())
		})

		It("creates it", func() {
			var created views.%[2]sResponse
			app.Post("/%[3]s", body).Do().Status(http.StatusCreated).JSON(&created)

			Expect(created.ID).NotTo(BeZero())
			_, err := app.Store.Get%[2]s(GinkgoT().Context(), fmt.Sprint(created.ID))
			Expect(err).NotTo(HaveOccurred())
		})
	})
`, lowerCamel(s.name), typ, table)
			if uniq := uniqueAttrs(s.attrs); len(uniq) > 0 {
				names := make([]string, len(uniq))
				for i, a := range uniq {
					names[i] = a.Name
				}
				w(`
	Context("when a %[1]s with the same %[2]s exists", func() {
		BeforeEach(func() {
			body = %[3]sRequest(%[4]s)
		})

		It("is unprocessable", func() {
			app.Post("/%[5]s", body).Do().Status(http.StatusUnprocessableEntity)
		})
	})
`, s.name, strings.Join(names, " and "), lowerCamel(s.name), factory, table)
			}
			w("\n")
		}
		w(`	Context("with a malformed body", func() {
		It("is a bad request", func() {
			app.Post("/%s", "{\"nope\":").Do().Status(http.StatusBadRequest)
		})
	})
`, table)
	}
	w("})\n\n")

	// PATCH /things/:id
	var sent *Attr
	var sentLit string
	for i, a := range s.attrs {
		if _, lit := edited(a, s.scheme); lit != "" && a.Type != "references" && a.Type != "uuid" {
			sent, sentLit = &s.attrs[i], lit
			break
		}
	}
	w("var _ = Describe(\"PATCH /%s/:id\", func() {\n", table)
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
			app.Patch("/%[2]s/%[3]s", map[string]any{}).Do().Status(http.StatusNotFound)
		})
	})
})

`, s.name, table, missing)

	// DELETE /things/:id
	w("var _ = Describe(\"DELETE /%s/:id\", func() {\n", table)
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
			app.Delete("/%[2]s/%[3]s").Do().Status(http.StatusNotFound)
		})
	})
})
`, s.name, table, missing)

	if hasRefs {
		w(`
// The specs that save a %[1]s are left out: it belongs to a parent that has to
// be saved first. Make the parent with its factory, set the reference in the
// "with" function of factories.%[2]s, and add them.
`, s.name, typ)
	} else if len(s.attrs) > 0 {
		w("\n// %[1]sRequest is the body that creates a record like this one.\nfunc %[1]sRequest(record models.%[2]s) views.%[2]sRequest {\n\treturn views.%[2]sRequest{", lowerCamel(s.name), typ)
		for i, a := range s.attrs {
			if i > 0 {
				w(", ")
			}
			w("%[1]s: record.%[1]s", a.goName())
		}
		w("}\n}\n")
	}

	var head strings.Builder
	head.WriteString("package controllers_test\n\nimport (\n")
	if !hasRefs {
		head.WriteString("\t\"fmt\"\n")
	}
	head.WriteString("\t\"net/http\"\n\n\t. \"github.com/onsi/ginkgo/v2\"\n")
	if !hasRefs {
		// Only the specs that save a record assert with Expect.
		head.WriteString("\t. \"github.com/onsi/gomega\"\n")
		fmt.Fprintf(&head, "\n\t%q\n\t%q\n\t%q\n", s.module+"/app/models", s.module+"/app/views", s.module+"/test/factories")
	}
	head.WriteString(")\n\n")
	return head.String() + b.String()
}
