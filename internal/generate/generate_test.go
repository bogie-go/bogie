package generate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var at = time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)

// golden compares got with testdata/<name>.golden, or rewrites it with
// -update. Every generator's output is pinned byte for byte this way.
func golden(t *testing.T, name string, files []File) {
	t.Helper()
	var b strings.Builder
	for _, f := range files {
		b.WriteString("==> " + f.Path + "\n")
		b.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			b.WriteString("\n")
		}
	}
	got := b.String()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file; diff with:\n  go test ./internal/generate -run %s -update && git diff %s", name, t.Name(), path)
	}
}

func attrs(t *testing.T, args ...string) []Attr {
	t.Helper()
	a, err := ParseAttrs(args)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestMigrationCreateTable(t *testing.T) {
	f, err := Migration("create_comments", attrs(t, "body:text", "post:references", "score:integer", "published_at:datetime"), at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "migration_create", []File{f})
}

func TestMigrationAddAndRemoveColumns(t *testing.T) {
	add, err := Migration("add_slug_to_posts", attrs(t, "slug:string:uniq", "draft:boolean"), at)
	if err != nil {
		t.Fatal(err)
	}
	remove, err := Migration("remove_draft_from_posts", attrs(t, "draft:boolean"), at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "migration_add_remove", []File{add, remove})
}

func TestMigrationUnknownShapeIsEmpty(t *testing.T) {
	f, err := Migration("fix_the_thing", nil, at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Content, "TODO") || f.Path != "db/migrate/20261002093000_fix_the_thing.sql" {
		t.Errorf("file = %+v", f)
	}
}

func TestModel(t *testing.T) {
	files, err := Model("example.com/blog", "comment", attrs(t, "body:text", "post:references", "score:integer", "published_at:datetime", "meta:jsonb"), at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_comment", files)
}

func TestModelWithNoAttributes(t *testing.T) {
	files, err := Model("example.com/blog", "ping", nil, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_ping", files)
}

func TestModelRefusesWhatEveryModelAlreadyHas(t *testing.T) {
	for _, bad := range []string{"id:uuid", "created_at:datetime"} {
		if _, err := Model("m", "thing", attrs(t, bad), at); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	for _, plural := range []string{"posts", "categories", "boxes"} {
		if _, err := Model("m", plural, nil, at); err == nil {
			t.Errorf("plural model name %q was accepted", plural)
		}
	}
	for _, singular := range []string{"status", "address", "analysis", "bus"} {
		if _, err := Model("m", singular, nil, at); err != nil {
			t.Errorf("singular model name %q refused: %v", singular, err)
		}
	}
}

func TestParseAttrs(t *testing.T) {
	got := attrs(t, "title:string", "author:references", "slug:string:uniq", "tag:string:index")
	if got[1].Name != "author_id" || !got[1].Index {
		t.Errorf("references: %+v", got[1])
	}
	if !got[2].Unique || !got[3].Index {
		t.Errorf("modifiers: %+v %+v", got[2], got[3])
	}
	for _, bad := range []string{"title", "title:money", "Title:string", "x:string:nope"} {
		if _, err := ParseAttrs([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestInflections(t *testing.T) {
	plurals := map[string]string{"post": "posts", "comment": "comments", "category": "categories", "box": "boxes", "address": "addresses", "day": "days", "person": "people", "status": "statuses"}
	for s, p := range plurals {
		if got := Plural(s); got != p {
			t.Errorf("Plural(%q) = %q, want %q", s, got, p)
		}
		if got := Singular(p); got != s {
			t.Errorf("Singular(%q) = %q, want %q", p, got, s)
		}
	}
	snakes := map[string]string{"Post": "post", "BlogPost": "blog_post", "AddSlugToPosts": "add_slug_to_posts", "post": "post", "blog_post": "blog_post"}
	for s, want := range snakes {
		if got := Snake(s); got != want {
			t.Errorf("Snake(%q) = %q, want %q", s, got, want)
		}
	}
	camels := map[string]string{"post_id": "PostID", "image_url": "ImageUrl", "id": "ID", "created_at": "CreatedAt"}
	for s, c := range camels {
		if got := Camel(s); got != c {
			t.Errorf("Camel(%q) = %q, want %q", s, got, c)
		}
	}
}

func TestController(t *testing.T) {
	files, wires, err := Controller("example.com/blog", "comments", []string{"index", "show", "create", "search"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "controller_comments", files)
	if len(wires) != 3 || wires[0].Marker != "controllers" || wires[1].Marker != "wire" || wires[2].Marker != "routes" {
		t.Errorf("wires = %+v", wires)
	}
	if wires[1].Line != "server.Comments = comments_controller.NewServer(log)" {
		t.Errorf("wire line = %q", wires[1].Line)
	}
}

func TestControllerRefusesForms(t *testing.T) {
	for _, bad := range [][]string{{"new"}, {"edit"}, {"index", "index"}} {
		if _, _, err := Controller("m", "comments", bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestControllerWithNoActions(t *testing.T) {
	files, _, err := Controller("example.com/blog", "health", nil)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "controller_empty", files)
}

func TestService(t *testing.T) {
	files, err := Service("example.com/blog", "publish_post")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "service_publish_post", files)
}
