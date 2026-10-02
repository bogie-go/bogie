package markers

import (
	"strings"
	"testing"
)

const server = `package controllers

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"example.com/blog/app/controllers/posts_controller"
	"example.com/blog/config"
)

type Server struct {
	Config config.Config
	Log    *slog.Logger

	Posts *posts_controller.Server
	// bogie:controllers
}
`

func TestInsertAboveMarkerWithItsIndent(t *testing.T) {
	out, changed, err := Insert([]byte(server), "x.go", "controllers", "Comments *comments_controller.Server")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), "\tPosts *posts_controller.Server\n\tComments *comments_controller.Server\n\t// bogie:controllers") {
		t.Errorf("not inserted above the marker with a tab:\n%s", out)
	}
}

func TestInsertIsIdempotent(t *testing.T) {
	once, _, _ := Insert([]byte(server), "x.go", "controllers", "Comments *comments_controller.Server")
	twice, changed, err := Insert(once, "x.go", "controllers", "  Comments *comments_controller.Server  ")
	if err != nil || changed || string(twice) != string(once) {
		t.Errorf("second insert changed the file: changed=%v err=%v", changed, err)
	}
}

func TestInsertNamesAMissingOrDuplicatedMarker(t *testing.T) {
	_, _, err := Insert([]byte(server), "app/x.go", "routes", "line")
	if err == nil || !strings.Contains(err.Error(), "app/x.go") || !strings.Contains(err.Error(), "bogie:routes") {
		t.Errorf("missing marker: err = %v", err)
	}
	dup := server + "\n// bogie:controllers\n"
	_, _, err = Insert([]byte(dup), "app/x.go", "controllers", "line")
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("duplicated marker: err = %v", err)
	}
}

func TestRemoveByPrefix(t *testing.T) {
	out, changed := Remove([]byte(server), "Posts *posts_controller")
	if !changed || strings.Contains(string(out), "Posts *posts_controller") {
		t.Errorf("not removed:\n%s", out)
	}
	_, changed = Remove(out, "Posts *posts_controller")
	if changed {
		t.Error("removed something on the second pass")
	}
}

func TestAddImportThenFormatSortsIt(t *testing.T) {
	out, changed, err := AddImport([]byte(server), "example.com/blog/app/controllers/comments_controller")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	out, err = Format(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	a := strings.Index(s, `"example.com/blog/app/controllers/comments_controller"`)
	b := strings.Index(s, `"example.com/blog/app/controllers/posts_controller"`)
	if a < 0 || b < 0 || a > b {
		t.Errorf("import not sorted into place:\n%s", s)
	}
	_, changed, _ = AddImport(out, "example.com/blog/app/controllers/comments_controller")
	if changed {
		t.Error("added an import that was already there")
	}
}

func TestPruneImportsDropsTheUnreferenced(t *testing.T) {
	src, _ := Remove([]byte(server), "Posts *posts_controller")
	out := string(PruneImports(src))
	if strings.Contains(out, "posts_controller") {
		t.Errorf("unreferenced import kept:\n%s", out)
	}
	if !strings.Contains(out, `"example.com/blog/config"`) || !strings.Contains(out, `"log/slog"`) {
		t.Errorf("a referenced import was dropped:\n%s", out)
	}
	if _, err := Format([]byte(out)); err != nil {
		t.Errorf("pruned file does not format: %v", err)
	}
}

func TestCheck(t *testing.T) {
	if err := Check([]byte(server), "x.go", "controllers"); err != nil {
		t.Error(err)
	}
	if err := Check([]byte(server), "x.go", "routes"); err == nil {
		t.Error("missing marker passed")
	}
}
