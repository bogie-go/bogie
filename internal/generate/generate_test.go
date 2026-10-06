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
	files, wires, err := Controller("example.com/blog", "", "comments", []string{"index", "show", "create", "search"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "controller_comments", files)
	if len(wires) != 3 || wires[0].Marker != "controllers" || wires[1].Marker != "wire" || wires[2].Marker != "routes" {
		t.Errorf("wires = %+v", wires)
	}
	if wires[0].Line != "Comments *CommentsController" || wires[0].Import != "" {
		t.Errorf("field line = %+v", wires[0])
	}
	if wires[1].Line != "server.Comments = controllers.NewCommentsController(log)" || wires[1].Import != "example.com/blog/app/controllers" {
		t.Errorf("wire line = %+v", wires[1])
	}
	if wires[2].Line != "s.Comments.SetupRoutes(&r.RouterGroup)" {
		t.Errorf("mount line = %q", wires[2].Line)
	}
}

// A namespace is a package, written once; the controller in it is one file,
// registered on the namespace's Server and constructed from app/application.go.
func TestControllerInANamespace(t *testing.T) {
	nsFiles, nsWires, err := Namespace("example.com/blog", "admin")
	if err != nil {
		t.Fatal(err)
	}
	files, wires, err := Controller("example.com/blog", "admin", "reports", []string{"index", "show"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "controller_admin_reports", append(nsFiles, files...))

	if nsWires[0].Line != "Admin *admin_controller.Server" || nsWires[1].Line != "server.Admin = admin_controller.NewServer(log)" || nsWires[2].Line != "s.Admin.SetupRoutes(&r.RouterGroup)" {
		t.Errorf("namespace wires = %+v", nsWires)
	}
	want := []Wire{
		{File: "app/controllers/admin_controller/server.go", Marker: "controllers", Line: "Reports *ReportsController"},
		{File: "app/application.go", Marker: "wire", Line: "server.Admin.Reports = admin_controller.NewReportsController(log)", Import: "example.com/blog/app/controllers/admin_controller"},
		{File: "app/controllers/admin_controller/routes.go", Marker: "routes", Line: "s.Reports.SetupRoutes(admin)"},
	}
	for i, w := range want {
		if wires[i] != w {
			t.Errorf("wire %d = %+v, want %+v", i, wires[i], w)
		}
	}
	// The prefixes destroy removes by must match the lines, and the
	// namespace's own line must not match its controllers' lines.
	for i, p := range WirePrefixes("admin", "reports") {
		if !strings.HasPrefix(wires[i].Line, p.Line) || p.File != wires[i].File {
			t.Errorf("prefix %d %+v does not match %+v", i, p, wires[i])
		}
	}
	for i, p := range NamespaceWirePrefixes("admin") {
		if !strings.HasPrefix(nsWires[i].Line, p.Line) {
			t.Errorf("namespace prefix %d %q does not match %q", i, p.Line, nsWires[i].Line)
		}
		if strings.HasPrefix(wires[i].Line, p.Line) {
			t.Errorf("namespace prefix %d %q would also remove the controller's %q", i, p.Line, wires[i].Line)
		}
	}
}

func TestParseControllerName(t *testing.T) {
	for spec, want := range map[string][2]string{
		"posts": {"", "posts"}, "Posts": {"", "posts"}, "blog_posts": {"", "blog_posts"},
		"admin/posts": {"admin", "posts"}, "Admin::Posts": {"admin", "posts"}, "Admin/BlogPosts": {"admin", "blog_posts"},
		"internal/reports": {"internal", "reports"}, // the package is internal_controller, so Go's internal rule never applies
	} {
		ns, name, err := ParseControllerName(spec)
		if err != nil || ns != want[0] || name != want[1] {
			t.Errorf("ParseControllerName(%q) = %q, %q, %v; want %v", spec, ns, name, err, want)
		}
	}
	for _, bad := range []string{"", "a/b/c", "admin/", "/posts", "Admin::", "1posts"} {
		if _, _, err := ParseControllerName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLooksLikeASingularResource(t *testing.T) {
	cases := map[string]bool{
		"post index show": true, "post destroy": true,
		"posts index show": false, "post index": false, "welcome index": false,
		"search index": false, "status show": false, "comments show": false,
	}
	for spec, want := range cases {
		parts := strings.Fields(spec)
		if got := LooksLikeASingularResource(parts[0], parts[1:]); got != want {
			t.Errorf("LooksLikeASingularResource(%q) = %v, want %v", spec, got, want)
		}
	}
}

func TestControllerRefusesForms(t *testing.T) {
	for _, bad := range [][]string{{"new"}, {"edit"}, {"index", "index"}} {
		if _, _, err := Controller("m", "", "comments", bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestControllerWithNoActions(t *testing.T) {
	files, _, err := Controller("example.com/blog", "", "health", nil)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "controller_empty", files)
}

func TestScaffold(t *testing.T) {
	files, wires, err := Scaffold("example.com/blog", "post", attrs(t, "title:string", "body:text", "score:integer", "published_at:datetime", "author:references"), at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "scaffold_post", files)
	if len(wires) != 3 || wires[1].Line != "server.Posts = controllers.NewPostsController(store, log)" || wires[0].Line != "Posts *PostsController" {
		t.Errorf("wires = %+v", wires)
	}
	// destroy removes by the same paths, whatever the attributes were
	paths, err := ScaffoldFiles("example.com/blog", "post")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(files); i++ { // 0 is the migration, whose stamp differs
		if files[i].Path != paths[i].Path {
			t.Errorf("file %d: %s vs %s", i, files[i].Path, paths[i].Path)
		}
	}
	if _, _, err := Scaffold("m", "posts", nil, at); err == nil {
		t.Error("plural model name accepted")
	}
}

func TestScaffoldWithNoAttributes(t *testing.T) {
	files, _, err := Scaffold("example.com/blog", "ping", nil, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "scaffold_ping", files)
}

func TestAuthentication(t *testing.T) {
	for _, shape := range AuthenticationShapes {
		files, wires, err := Authentication("example.com/blog", "blog", shape, at)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "authentication_"+shape, files)
		prefixes := AuthenticationWirePrefixes("blog", shape)
		if len(prefixes) != len(wires) {
			t.Fatalf("%s: %d wires, %d prefixes", shape, len(wires), len(prefixes))
		}
		for i, p := range prefixes {
			if !strings.HasPrefix(wires[i].Line, p.Line) || p.File != wires[i].File {
				t.Errorf("%s: prefix %+v does not match %+v", shape, p, wires[i])
			}
		}
	}
	if _, _, err := Authentication("m", "blog", "oauth", at); err == nil {
		t.Error("unknown shape accepted")
	}
	w := AuthenticationSettings("blog", "token")[0]
	if w.Env != "BLOG_TOKEN_SECRET" || w.Field != "TokenSecret" || w.Key != "token_secret" {
		t.Errorf("setting = %+v", w)
	}
}

func TestService(t *testing.T) {
	files, err := Service("example.com/blog", "publish_post")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "service_publish_post", files)
}

// sqlc passes a single parameter as itself and only builds a Params struct
// for two or more, so a one-column model's Create must not name one.
func TestModelWithOneAttribute(t *testing.T) {
	files, err := Model("example.com/blog", "thing", attrs(t, "name:string"), at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_thing", files)
	for _, f := range files {
		if strings.HasSuffix(f.Path, "things.go") && strings.Contains(f.Content, "CreateThingParams") {
			t.Error("one-attribute Create names a Params struct sqlc will not generate")
		}
	}
}

func TestJob(t *testing.T) {
	files, wires, err := Job("send_welcome")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "job_send_welcome", files)
	if len(wires) != 1 || wires[0].Marker != "jobs" || wires[0].Line != "registered += add(workers, &SendWelcomeWorker{Log: log})" {
		t.Errorf("wires = %+v", wires)
	}
}

// The generated files parse whatever name a model has, so a parameter that
// shadows a variable already in scope is a type error no golden file and no
// format.Source call would notice. Every method the generators write holds one
// of these: s for the *Store receiver, c for the *gin.Context an action
// handles, f for the fake store in a controller test.
func TestGeneratedParamDoesNotShadow(t *testing.T) {
	for _, c := range []struct {
		generator, name, file, shadowed string
	}{
		// The Store's methods all have receiver s.
		{"model", "session", "app/models/sessions.go", ", s domain.Session)"},
		// An action's own variable is c *gin.Context.
		{"scaffold", "comment", "app/controllers/comments_controller.go", "c, err := ctl.Store."},
		// The fake store a controller test builds has receiver f.
		{"scaffold", "form", "app/controllers/forms_controller_test.go", ", f domain.Form)"},
	} {
		t.Run(c.generator+" "+c.name, func(t *testing.T) {
			var files []File
			var err error
			if c.generator == "model" {
				files, err = Model("example.com/blog", c.name, nil, at)
			} else {
				files, _, err = Scaffold("example.com/blog", c.name, nil, at)
			}
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, f := range files {
				if f.Path != c.file {
					continue
				}
				found = true
				if strings.Contains(f.Content, c.shadowed) {
					t.Errorf("%s declares %q, shadowing a variable already in scope", f.Path, c.shadowed)
				}
			}
			if !found {
				t.Fatalf("%s did not write %s; the generators moved and this test no longer checks anything", c.generator, c.file)
			}
		})
	}
}

func TestParamName(t *testing.T) {
	for _, c := range []struct{ name, taken, want string }{
		{"post", "s", "p"},
		{"session", "s", "session"},
		{"comment", "c", "comment"},
		{"comment", "s", "c"},
		{"form", "f", "form"},
		{"api_key", "s", "a"},
		{"s", "s", "sVal"},
	} {
		if got := paramName(c.name, c.taken); got != c.want {
			t.Errorf("paramName(%q, %q) = %q, want %q", c.name, c.taken, got, c.want)
		}
	}
}
