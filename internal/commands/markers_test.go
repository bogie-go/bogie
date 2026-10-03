package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is the smallest app the controller generator can register into:
// bogie.toml, go.mod, and the three files that carry the markers.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("bogie.toml", "bogie = \"dev\"\n")
	write("go.mod", "module example.com/blog\n\ngo 1.25\n")
	write("app/controllers/application.go", "package controllers\n\nimport (\n\t\"log/slog\"\n)\n\ntype Server struct {\n\tLog *slog.Logger\n\t// bogie:controllers\n}\n")
	write("app/application.go", "package app\n\nimport (\n\t\"log/slog\"\n)\n\nfunc wire(log *slog.Logger) {\n\t// bogie:wire\n}\n")
	write("app/controllers/routes.go", "package controllers\n\nfunc (s *Server) SetupRoutes() {\n\t// bogie:routes\n}\n")
	return root
}

func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGenerateControllerRegistersAboveEveryMarker(t *testing.T) {
	root := fixture(t)
	var out bytes.Buffer
	if err := generateIn(root, []string{"controller", "comments", "index", "show"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	files := tree(t, root)
	for file, want := range map[string]string{
		"app/controllers/application.go": "Comments *CommentsController\n\t// bogie:controllers",
		"app/application.go":             "server.Comments = controllers.NewCommentsController(log)\n\t// bogie:wire",
		"app/controllers/routes.go":      "s.Comments.SetupRoutes(&r.RouterGroup)\n\t// bogie:routes",
	} {
		if !strings.Contains(files[file], want) {
			t.Errorf("%s: missing %q:\n%s", file, want, files[file])
		}
	}
	if !strings.Contains(files["app/application.go"], `"example.com/blog/app/controllers"`) {
		t.Error("import not added")
	}
	if _, ok := files["app/controllers/comments_controller.go"]; !ok {
		t.Error("controller file not written")
	}
}

// A namespaced controller: the first one writes the package and registers
// it in the root; the second only registers itself in the package; destroy
// reverses both, and the package goes with its last controller.
func TestGenerateControllerInANamespaceRoundTrips(t *testing.T) {
	root := fixture(t)
	var out bytes.Buffer
	if err := generateIn(root, []string{"controller", "admin/reports", "index", "show"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	files := tree(t, root)
	for file, want := range map[string][]string{
		"app/controllers/application.go":                         {"Admin *admin_controller.Server\n\t// bogie:controllers", `"example.com/blog/app/controllers/admin_controller"`},
		"app/application.go":                                     {"server.Admin = admin_controller.NewServer(log)\n\tserver.Admin.Reports = admin_controller.NewReportsController(log)\n\t// bogie:wire"},
		"app/controllers/routes.go":                              {"s.Admin.SetupRoutes(&r.RouterGroup)\n\t// bogie:routes"},
		"app/controllers/admin_controller/server.go":             {"Reports *ReportsController\n\t// bogie:controllers"},
		"app/controllers/admin_controller/routes.go":             {"s.Reports.SetupRoutes(admin)\n\t// bogie:routes"},
		"app/controllers/admin_controller/reports_controller.go": {"type ReportsController struct"},
	} {
		for _, w := range want {
			if !strings.Contains(files[file], w) {
				t.Errorf("%s: missing %q:\n%s", file, w, files[file])
			}
		}
	}

	// A second controller in the same namespace touches nothing in the root.
	rootBefore := map[string]string{}
	for _, f := range []string{"app/controllers/application.go", "app/controllers/routes.go"} {
		rootBefore[f] = files[f]
	}
	out.Reset()
	if err := generateIn(root, []string{"controller", "admin/audits", "index"}, &out); err != nil {
		t.Fatalf("second generate: %v\n%s", err, out.String())
	}
	files = tree(t, root)
	for f, before := range rootBefore {
		if files[f] != before {
			t.Errorf("%s changed for a second controller in an existing namespace", f)
		}
	}
	if strings.Count(files["app/application.go"], "server.Admin = ") != 1 {
		t.Errorf("namespace constructed more than once:\n%s", files["app/application.go"])
	}
	if !strings.Contains(files["app/controllers/admin_controller/server.go"], "*AuditsController\n\t// bogie:controllers") {
		t.Error("second controller not registered in the namespace")
	}

	// Destroy the first: the namespace stays for the second.
	out.Reset()
	if err := destroyIn(root, []string{"controller", "admin/reports"}, &out); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out.String())
	}
	files = tree(t, root)
	if _, ok := files["app/controllers/admin_controller/reports_controller.go"]; ok {
		t.Error("controller file not removed")
	}
	if _, ok := files["app/controllers/admin_controller/server.go"]; !ok {
		t.Error("namespace removed while it still held a controller")
	}
	if strings.Contains(files["app/application.go"], "Reports") || !strings.Contains(files["app/application.go"], "server.Admin = ") {
		t.Errorf("wrong lines removed:\n%s", files["app/application.go"])
	}

	// Destroy the last: the namespace goes, and the root is as it began.
	out.Reset()
	if err := destroyIn(root, []string{"controller", "admin/audits"}, &out); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out.String())
	}
	files = tree(t, root)
	for file := range files {
		if strings.HasPrefix(file, "app/controllers/admin_controller/") {
			t.Errorf("%s left behind", file)
		}
	}
	for _, f := range []string{"app/controllers/application.go", "app/application.go", "app/controllers/routes.go"} {
		if strings.Contains(files[f], "Admin") || strings.Contains(files[f], "admin_controller") {
			t.Errorf("%s still mentions the namespace:\n%s", f, files[f])
		}
	}
}

// --pretend on a new namespace plans every line, including those into the
// files it would have written, and writes nothing.
func TestGenerateControllerInANamespacePretendWritesNothing(t *testing.T) {
	root := fixture(t)
	before := tree(t, root)
	var out bytes.Buffer
	if err := generateIn(root, []string{"controller", "admin/reports", "index", "--pretend"}, &out); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}
	after := tree(t, root)
	if len(after) != len(before) {
		t.Errorf("files written under --pretend: %d before, %d after", len(before), len(after))
	}
	for _, want := range []string{"create  app/controllers/admin_controller/server.go", "Reports *ReportsController", "s.Reports.SetupRoutes(admin)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan lacks %q:\n%s", want, out.String())
		}
	}
}

// §8.3: delete each marker in turn; the generator must stop, name the file,
// and write nothing: not the package, not the other two registrations.
func TestGenerateControllerStopsOnAMissingMarkerWithNoPartialWrite(t *testing.T) {
	for _, m := range []struct{ file, marker string }{
		{"app/controllers/application.go", "controllers"},
		{"app/application.go", "wire"},
		{"app/controllers/routes.go", "routes"},
	} {
		t.Run(m.marker, func(t *testing.T) {
			root := fixture(t)
			path := filepath.Join(root, filepath.FromSlash(m.file))
			src, _ := os.ReadFile(path)
			if err := os.WriteFile(path, bytes.ReplaceAll(src, []byte("// bogie:"+m.marker), nil), 0o644); err != nil {
				t.Fatal(err)
			}
			before := tree(t, root)

			var out bytes.Buffer
			err := generateIn(root, []string{"controller", "comments", "index"}, &out)
			if err == nil {
				t.Fatal("generate succeeded with a marker missing")
			}
			if !strings.Contains(err.Error(), m.file) || !strings.Contains(err.Error(), "bogie:"+m.marker) {
				t.Errorf("error does not name the file and marker: %v", err)
			}
			after := tree(t, root)
			if len(after) != len(before) {
				t.Errorf("files were written despite the error: %d before, %d after", len(before), len(after))
			}
			for file, content := range before {
				if after[file] != content {
					t.Errorf("%s was changed despite the error", file)
				}
			}
		})
	}
}

func TestGenerateControllerIsIdempotent(t *testing.T) {
	root := fixture(t)
	var out bytes.Buffer
	if err := generateIn(root, []string{"controller", "comments", "index"}, &out); err != nil {
		t.Fatal(err)
	}
	once := tree(t, root)
	out.Reset()
	if err := generateIn(root, []string{"controller", "comments", "index"}, &out); err != nil {
		t.Fatalf("second run: %v", err)
	}
	twice := tree(t, root)
	for file := range once {
		if once[file] != twice[file] {
			t.Errorf("%s changed on the second run", file)
		}
	}
	if strings.Contains(out.String(), "insert") || strings.Contains(out.String(), "create") {
		t.Errorf("second run reported writes:\n%s", out.String())
	}
}
