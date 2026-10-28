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
	f, err := Migration("create_comments", attrs(t, "body:text", "post:references", "score:integer", "published_at:datetime"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "migration_create", []File{f})
}

func TestMigrationAddAndRemoveColumns(t *testing.T) {
	add, err := Migration("add_slug_to_posts", attrs(t, "slug:string:uniq", "draft:boolean"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	remove, err := Migration("remove_draft_from_posts", attrs(t, "draft:boolean"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "migration_add_remove", []File{add, remove})
}

func TestMigrationUnknownShapeIsEmpty(t *testing.T) {
	f, err := Migration("fix_the_thing", nil, IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Content, "TODO") || f.Path != "db/migrate/20261002093000_fix_the_thing.sql" {
		t.Errorf("file = %+v", f)
	}
}

func TestModel(t *testing.T) {
	files, err := Model("example.com/blog", "comment", attrs(t, "body:text", "post:references", "score:integer", "published_at:datetime", "meta:jsonb"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_comment", files)
}

// The not-found tables name what is wrong with the id in the app's own terms,
// so the spec reads as a sentence: a number under bigint ids, a uuid under uuid.
func TestModelSpecNamesTheKindOfIDPerScheme(t *testing.T) {
	for scheme, want := range map[IDScheme]string{
		IDBigint: `Entry("when the id is not a number", "nope")`,
		IDUUID:   `Entry("when the id is not a uuid", "nope")`,
	} {
		files, err := Model("example.com/blog", "cart_item", nil, scheme, at)
		if err != nil {
			t.Fatal(err)
		}
		var spec string
		for _, f := range files {
			if f.Path == "db/adapter/cart_items_test.go" {
				spec = f.Content
			}
		}
		for _, line := range []string{`DescribeTable("returns ErrNotFound"`, want, `Entry("when no cart item has that id"`} {
			if !strings.Contains(spec, line) {
				t.Errorf("%s ids: the store spec lacks %s", scheme, line)
			}
		}
	}
}

func TestModelWithNoAttributes(t *testing.T) {
	files, err := Model("example.com/blog", "ping", nil, IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_ping", files)
}

func TestModelRefusesWhatEveryModelAlreadyHas(t *testing.T) {
	for _, bad := range []string{"id:uuid", "created_at:datetime"} {
		if _, err := Model("m", "thing", attrs(t, bad), IDBigint, at); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	for _, plural := range []string{"posts", "categories", "boxes"} {
		if _, err := Model("m", plural, nil, IDBigint, at); err == nil {
			t.Errorf("plural model name %q was accepted", plural)
		}
	}
	for _, singular := range []string{"status", "address", "analysis", "bus"} {
		if _, err := Model("m", singular, nil, IDBigint, at); err != nil {
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
	for _, bad := range []string{"title:money", "Title:string", "x:string:nope", ":string"} {
		if _, err := ParseAttrs([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// bogie g config says int and bool, scaffold says integer and boolean; each
// accepts the other's spelling and means the same column.
func TestParseAttrsAcceptsIntAndBoolAliases(t *testing.T) {
	got := attrs(t, "qty:int", "price:integer", "ok:bool", "done:boolean", "n:int:index")
	for i, want := range []string{"integer", "integer", "boolean", "boolean", "integer"} {
		if got[i].Type != want {
			t.Errorf("%s: Type = %q, want %q", got[i].Name, got[i].Type, want)
		}
	}
	if !got[4].Index {
		t.Errorf("n:int:index lost its modifier: %+v", got[4])
	}
}

func TestParseConfigKeysAcceptsIntegerAndBooleanAliases(t *testing.T) {
	keys, err := ParseConfigKeys([]string{"port:integer=25", "tls:boolean=yes"})
	if err != nil {
		t.Fatal(err)
	}
	if keys[0].Type != "int" || keys[0].Value != "25" || keys[1].Type != "bool" || keys[1].Value != "true" {
		t.Errorf("keys = %+v", keys)
	}
}

func TestParseAttrsDefaultsBareNameToString(t *testing.T) {
	got := attrs(t, "title", "description")
	for _, a := range got {
		if a.Type != "string" {
			t.Errorf("%q: Type = %q, want string", a.Name, a.Type)
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
	files, wires, err := Scaffold("example.com/blog", "post", attrs(t, "title:string", "body:text", "score:integer", "published_at:datetime", "author:references"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "scaffold_post", files)
	if len(wires) != 3 || wires[1].Line != "server.Posts = controllers.NewPostsController(store, log)" || wires[0].Line != "Posts *PostsController" {
		t.Errorf("wires = %+v", wires)
	}
	// destroy removes by the same paths, whatever the attributes were
	paths, err := ScaffoldFiles("example.com/blog", "post", IDBigint)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(files); i++ { // 0 is the migration, whose stamp differs
		if files[i].Path != paths[i].Path {
			t.Errorf("file %d: %s vs %s", i, files[i].Path, paths[i].Path)
		}
	}
	if _, _, err := Scaffold("m", "posts", nil, IDBigint, at); err == nil {
		t.Error("plural model name accepted")
	}
}

func TestScaffoldWithNoAttributes(t *testing.T) {
	files, _, err := Scaffold("example.com/blog", "ping", nil, IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "scaffold_ping", files)
}

func TestAuthentication(t *testing.T) {
	for _, shape := range AuthenticationShapes {
		files, wires, err := Authentication("example.com/blog", "blog", shape, IDBigint, at)
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
	if _, _, err := Authentication("m", "blog", "oauth", IDBigint, at); err == nil {
		t.Error("unknown shape accepted")
	}
	w, ok := AuthenticationSetting("token")
	if !ok || w.Path() != "token.secret" || w.Env() != "TOKEN_SECRET" || w.Field() != "Token.Secret" || w.Min != 32 {
		t.Errorf("token setting = %+v, %v", w, ok)
	}
	if c, ok := AuthenticationSetting("secret"); !ok || c.Path() != "control.secret" {
		t.Errorf("secret setting = %+v, %v", c, ok)
	}
	if _, ok := AuthenticationSetting("api_key"); ok {
		t.Error("api_key reads no config setting: its keys live in a table")
	}
}

func TestParseConfigKeys(t *testing.T) {
	keys, err := ParseConfigKeys([]string{"address:req", "port:int=1025", "username", "password:secret", "tls:bool=true", "timeout:duration=5s", "retry_limit:int:req"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ConfigKey{
		{Name: "address", Type: "string", Req: true, Min: 1},
		{Name: "port", Type: "int", Value: "1025", HasValue: true, Min: 1},
		{Name: "username", Type: "string", Min: 1},
		{Name: "password", Type: "string", Secret: true, Min: 1},
		{Name: "tls", Type: "bool", Value: "true", HasValue: true, Min: 1},
		{Name: "timeout", Type: "duration", Value: "5s", HasValue: true, Min: 1},
		{Name: "retry_limit", Type: "int", Req: true, Min: 1},
	}
	if len(keys) != len(want) {
		t.Fatalf("keys = %+v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key %d = %+v, want %+v", i, keys[i], want[i])
		}
	}
	if got := keys[0].Env("smtp"); got != "SMTP_ADDRESS" {
		t.Errorf("Env = %q", got)
	}
	for name, bad := range map[string][]string{
		"none":                     nil,
		"upper case":               {"Address"},
		"unknown suffix":           {"port:float"},
		"twice":                    {"a", "a"},
		"required bool":            {"tls:bool:req"},
		"int secret":               {"pin:int:secret"},
		"secret with a value":      {"password:secret=hunter2"},
		"int that is not an int":   {"port:int=abc"},
		"duration that is not one": {"timeout:duration=soon"},
	} {
		if _, err := ParseConfigKeys(bad); err == nil {
			t.Errorf("%s: accepted %v", name, bad)
		}
	}
}

// Values are written the way a person would: bare when safe, quoted when YAML
// would read them as something else; and a bool takes yes/no.
func TestConfigKeyYAML(t *testing.T) {
	keys, err := ParseConfigKeys([]string{"username=xxx", "mail=help@example.com", "football=yes", "addr=localhost:1125",
		"pin=007", "ttl:duration=5m", "name=two words", "on:bool=yes", "off:bool=no", "limit:int=3", "unset", "secret_one:secret"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`username: xxx`,
		`mail: help@example.com`,
		`football: "yes"`, // a string: YAML 1.1 would read a bare yes as a boolean
		`addr: "localhost:1125"`,
		`pin: "007"`, // a bare 007 is a number
		`ttl: 5m`,
		`name: "two words"`,
		`on: true`,
		`off: false`,
		`limit: 3`,
		`# unset: ""`,
		`# secret_one: ""`,
	}
	for i, k := range keys {
		if got := k.YAML(); got != want[i] {
			t.Errorf("key %s: YAML() = %q, want %q", k.Name, got, want[i])
		}
	}
}

// A core group takes keys but is not created or destroyed here, and the
// check for a required one reaches the key through the group.
func TestCoreConfigGroups(t *testing.T) {
	if err := CheckConfigGroupName("database"); err != nil {
		t.Errorf("adding keys to database refused: %v", err)
	}
	if err := ParseConfigGroup("database"); err == nil {
		t.Error("database accepted as a group to create or destroy")
	}
	keys, _ := ParseConfigKeys([]string{"username:req", "password:secret", "pool:int"})
	ws := CoreConfigKeyWires("database", keys)
	var markers, lines []string
	for _, w := range ws {
		markers, lines = append(markers, w.Marker), append(lines, w.Line)
	}
	if strings.Join(markers, ",") != "config:database,validate,config:database,validate,config:database" {
		t.Errorf("markers = %v", markers)
	}
	if !strings.Contains(lines[1], `c.Database.Username != ""`) || !strings.Contains(lines[1], "c.Env,") || !strings.Contains(lines[3], `c.Database.Password, 1`) {
		t.Errorf("validate lines = %q", lines)
	}
	src := []byte("type DatabaseConfig struct {\n\tURL string `mapstructure:\"url\"`\n}\ntype ServerConfig struct {\n\tAddr string `mapstructure:\"addr\"`\n}\n")
	if !ConfigStructHas(src, "DatabaseConfig", "url") || ConfigStructHas(src, "DatabaseConfig", "addr") || !ConfigStructHas(src, "ServerConfig", "addr") {
		t.Error("ConfigStructHas looked outside its own struct")
	}
}

func TestParseConfigGroup(t *testing.T) {
	for _, ok := range []string{"smtp", "mail_gateway", "s3"} {
		if err := ParseConfigGroup(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"Smtp", "", "server", "database", "a-b", "1st"} {
		if err := ParseConfigGroup(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestConfigGroup(t *testing.T) {
	keys, err := ParseConfigKeys([]string{"address:req", "port:int=1025", "username", "password:secret", "timeout:duration=5s"})
	if err != nil {
		t.Fatal(err)
	}
	file, wires, err := ConfigGroup("smtp", keys)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "config_smtp", []File{file})
	prefixes := ConfigGroupWirePrefixes("smtp")
	if len(prefixes) != len(wires) {
		t.Fatalf("%d wires, %d prefixes", len(wires), len(prefixes))
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(wires[i].Line, p.Line) || p.File != wires[i].File {
			t.Errorf("prefix %+v does not match %+v", p, wires[i])
		}
	}

	// A group that exists gets only the keys it lacks, above its markers.
	more, _ := ParseConfigKeys([]string{"retries:int:req", "timeout:duration"})
	if !ConfigGroupHas([]byte(file.Content), "timeout") || ConfigGroupHas([]byte(file.Content), "retries") {
		t.Error("ConfigGroupHas disagrees with the generated file")
	}
	ws := ConfigKeyWires("smtp", more[:1])
	if len(ws) != 2 || ws[0].Marker != "config:smtp" || ws[1].Marker != "validate:smtp" {
		t.Errorf("key wires = %+v", ws)
	}
	if ws := ConfigKeyWires("smtp", more[1:]); len(ws) != 1 || ws[0].Import != "time" {
		t.Errorf("a duration key needs the time import: %+v", ws)
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
	files, err := Model("example.com/blog", "thing", attrs(t, "name:string"), IDBigint, at)
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

// A :uniq attr needs Create and Update to tell a duplicate value apart from
// any other database error, which no other golden model attribute exercises.
func TestModelWithUniqueAttribute(t *testing.T) {
	files, err := Model("example.com/blog", "user", attrs(t, "email:string:uniq", "password_digest:string"), IDBigint, at)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "model_user", files)
}

func TestJob(t *testing.T) {
	files, wires, err := Job("example.com/blog", "send_welcome")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "job_send_welcome", files)
	if len(wires) != 1 || wires[0].Marker != "jobs" || wires[0].Line != "registered += add(workers, &SendWelcomeWorker{Log: log})" {
		t.Errorf("wires = %+v", wires)
	}
}

func TestSeed(t *testing.T) {
	files, wires, err := Seed("example.com/blog", "demo_posts")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "seed_demo_posts", files)
	if len(wires) != 1 || wires[0].File != "db/seeds/seeds.go" || wires[0].Marker != "seeds" ||
		wires[0].Line != "func() error { return SeedDemoPosts(ctx, s) }," {
		t.Errorf("wires = %+v", wires)
	}
}

func TestSeedBadName(t *testing.T) {
	if _, _, err := Seed("example.com/blog", "DemoPosts"); err == nil {
		t.Fatal("want an error for a non-snake_case name")
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
		{"model", "session", "db/adapter/sessions.go", ", s models.Session)"},
		// An action's own variable is c *gin.Context.
		{"scaffold", "comment", "app/controllers/comments_controller.go", "c, err := ctl.Store."},
		// The fake store a controller test builds has receiver f.
		{"scaffold", "form", "app/controllers/forms_controller_test.go", ", f models.Form)"},
	} {
		t.Run(c.generator+" "+c.name, func(t *testing.T) {
			var files []File
			var err error
			if c.generator == "model" {
				files, err = Model("example.com/blog", c.name, nil, IDBigint, at)
			} else {
				files, _, err = Scaffold("example.com/blog", c.name, nil, IDBigint, at)
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
