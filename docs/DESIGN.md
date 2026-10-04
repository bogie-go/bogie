# Go on Rails — design

Status: **draft for review**, revised 2026-09-30. Nothing here is built.

The tool is called **Bogie** (§11): the wheeled frame under a rail car that
carries it along the track. The command is `bogie`.

---

## 0. The one-line version

**The Go service next to your Rails app.** A CLI that scaffolds it out of tools
the Go ecosystem already ships, and builds only the parts that have no
equivalent. Not a framework, not full-stack, not a Rails replacement: the one
network-facing service a Rails shop eventually needs, with Rails conventions
and generators that wire.

    go install github.com/<org>/bogie@latest
    bogie new blog
    cd blog && bogie server

The generated app **never imports bogie**. After `bogie new`, the code is the user's.

---

## 1. Goals and non-goals

Goals

1. A Rails developer can read, run and extend a generated service on day one,
   because the layout, the commands and the vocabulary match what they know.
2. **Agents write most of the Go.** The scaffold is built so an agent extends it
   by running generators and following written conventions, not by inventing
   structure. Guardrails matter more than boilerplate savings.
3. Small to maintain: the tool renders templates and shells out. It does not
   contain a router, an ORM, a job queue or a template engine.
4. Extracted from working code — kchat — and kept honest by generating an app in
   CI and building it.

Non-goals

- A runtime framework. No `bogie.Application`, no base classes, no DSL.
- Swappable parts in v1. One choice per concern; flags come when someone needs one.
- Idiomatic-Go-layout purity. The layout follows Rails on purpose (kchat
  `CLAUDE.md`: "Do not 'fix' this into idiomatic Go layout").
- Replacing Rails. The target is the Rails shop that needs a Go service for
  something that touches a network.

---

## 2. Principle: glue, then gaps

Use whatever the Go ecosystem provides. Wire it once. Build only what is missing.

| Rails concept | Choice | Kind |
| --- | --- | --- |
| HTTP router | **Gin** | ecosystem |
| Query layer (ActiveRecord) | **sqlc** + pgx/v5 | ecosystem |
| Migrations | **goose**, SQL files | ecosystem |
| Background jobs (ActiveJob) | **River** (Postgres, pgx/v5), opt-in via `--jobs` — decided §12.2 | ecosystem |
| Credentials | `bogie-go/credentials` | ecosystem (ours) |
| Deploy | **Kamal 2** | ecosystem |
| Lint / test | golangci-lint, `go test` | ecosystem |
| CI | **`bin/ci`**, run locally, Rails 8.1 style; `gh signoff` records the green run on the commit | **gap** (a port) |
| Task runner | `make` | ecosystem |
| Logging | `log/slog` | stdlib |
| Tool versions | Go 1.25+ for the tool (what gin v1.12 needs), Go 1.26+ for a generated app (what sqlc v1.31 needs); `tool` directive in `go.mod` | ecosystem |
| Project generator (`rails new`) | **bogie new** | **gap** |
| Generators (`rails g`) that also *wire* | **bogie g** | **gap** |
| One command surface (`rails db:*`, `rails s`) | app binary + `bogie` proxy | **gap** |
| Conventions and agent instructions | `AGENTS.md`, `docs/` | **gap** |
| Rails → Go cheat sheet | `docs/FROM_RAILS.md` | **gap** |
| Console | none — `make psql` plus test helpers | **won't fix** |

Why sqlc suits agent-written code: SQL lives in `.sql` files, the compiler
checks it against the migrations, and the generated Go is deterministic. A wrong
column name fails at `sqlc generate`, not in production, and an agent cannot
invent an ORM method that does not exist.

kchat today calls `sqlc` bare (`cd db && sqlc generate`), which needs a global
install. The scaffold pins sqlc and goose under the `tool` directive in
`go.mod` so a fresh clone needs only Go. Checked 2026-10-02: sqlc v1.31.1
builds with `CGO_ENABLED=0` (its Postgres parser runs in WebAssembly), the
directive adds about 30 lines to `go.mod` and 80 to `go.sum`, and `go tool
sqlc` compiles in under 20 seconds, once. The generated `*.sql.go` files are
committed and `bin/ci` runs `sqlc diff` so they cannot go stale.

---

## 3. What a generated app looks like

Taken from kchat's layout (`docs/STRUCTURE.md`), with everything channel-specific removed.

```
main.go                       dispatches: serve | worker | db | migrate
cli.go                        the subcommands (see §4)
docker-compose.yml            postgres on 5440, because the Rails app next door holds 5432
bogie.toml                      layout version — which templates generated this
AGENTS.md                     conventions, commands, recipes (§7)
app/
  application.go              wiring: config, connections, controllers
  controllers/
    application.go            root Server, one NAMED field per controller
    routes.go                 the whole routing table
    welcome.go                the development page at /, as `rails new` has; gone once a route claims /
    <name>_controller.go      one file per Rails controller: a <Name>Controller type, its routes, its actions
    <ns>_controller/          one package per Rails namespace: server.go · routes.go · <name>_controller.go …
  middlewares/                request id · logging · recover
  services/<name>/            ctx first, knows nothing about HTTP
  models/                     sqlc output + the store
  views/                      wire contracts; domain structs never double as wire structs
  domain/                     THE DOMAIN — imports nothing third-party (§12.10)
  jobs/                       only with --jobs: one River worker per file, typed args
config/                       encrypted credentials → validated struct, no globals
db/
  migrate/                    goose SQL, timestamped, go:embed
  queries/                    sqlc input
  seeds/                      goose SQL in its own version table, also go:embed
lib/logger/
docs/
Makefile · Dockerfile · docker-compose.yml · config/deploy.yml · .golangci.yml
```

The rules ship in `AGENTS.md` and are enforced where a linter can enforce them
(kchat `docs/STRUCTURE.md` §3):

1. `ctx context.Context` first for anything doing I/O.
2. Interfaces are declared by the consumer.
3. `app/domain` imports nothing third-party.
4. Constructors return `(T, error)`; only `main` exits.
5. `*gin.Context` never travels below `app/controllers`.
6. No `util/`, `helpers/`, `common/`.

`.golangci.yml` disables ST1003 for the underscore package names, exactly as kchat does.

**No `spec/`.** kchat created `spec/{factories,fixtures,support}` from Rails
habit and never used it: all 37 of its test files sit beside the code, stdlib
`testing` only, no mocking library. A vestigial Rails directory teaches the
wrong lesson about where Go tests go, so the scaffold does not create one.

---

## 4. Two CLIs, and why

| | installed tool `bogie` | the app's own binary |
| --- | --- | --- |
| Where it runs | a developer's laptop | laptop **and** the deployed container |
| Knows about | templates, the layout | config, database, the running app |
| Commands | `new`, `g`, `d`, `doctor`, `app:update`, and the `db:*` / `credentials:*` tasks as proxies | `serve`, `worker`, `db …`, `migrate …` |

The runtime commands cannot live only in `bogie`: on a deployed host there is no
generator, but there must be a way to migrate and roll back. kchat already does
this — `docker exec <container> kchat migrate status` — because the migrations
are embedded and the binary that applies them is the only thing that can roll
them back. Shipping the goose CLI separately would carry a second copy of the
migrations and invite version skew.

So `bogie db migrate` is a thin proxy for `go run . db migrate` inside the app, and
`make db-migrate` calls the same thing. **Development runs the code production
runs** (kchat Makefile, comment above `db-create`).

Seeds go through the binary too. kchat's `make db-seed` shells out to the goose
CLI with a URL pulled from a second tool, so it is the one path that can drift
from what the server resolves. The scaffold embeds `db/seeds` beside
`db/migrate` and `db seed` is a subcommand like the rest.

Commands, v1:

    bogie new NAME [--db=postgres] [--jobs] [--deploy=kamal]   # --jobs adds River; no extra datastore
    bogie g migration add_slug_to_posts slug:string
    bogie g model post title:string body:text
    bogie g scaffold post title:string body:text   # model + views + a wired controller
    bogie g controller posts index show create
    bogie g service publish_post
    bogie g authentication secret|token|api_key   # a middleware on a route group, with its config
    bogie d controller posts            # destroy what g created
    bogie server | worker | test | lint
    bogie db:create | db:drop | db:prepare | db:reset | db:seed
    bogie db:migrate | db:migrate:status | db:rollback | db:migrate:redo | db:version
    bogie credentials:edit [-e production] | credentials:show
    bogie doctor                        # layout and markers intact? tools pinned?

**Spelling, decided 2026-10-02: every command is spelled exactly as Rails
spells it.** Rails mixes two forms, colons for its Rake-derived tasks
(`db:migrate`, `credentials:edit`, `app:update`) and spaces for commands
(`server`, `generate`, `new`), and Bogie copies the mix rather than tidying it,
so a Rails developer never translates. The app's own binary stays a plain Go
CLI with spaces (`blog db prepare`, what `docker exec` runs), and the tool
accepts that form too. Tasks that destroy pass `--yes` on the developer's
behalf, as `rails db:rollback` asks nothing either; the binary in a container
keeps asking, because there a mistake is not undoable.

Later: `g job`, `g mailer`, `app:update` (§5a), `deploy`.

Output follows Rails: `create` / `insert` / `skip` / `conflict` per file, `--pretend`
for a dry run, and never overwrite without `--force`.

---

## 5. The hard part: generators that wire

Writing a handler is easy. **Registering it is not**, because Go has no autoload.
In kchat, adding one controller touches three hand-written places:

1. `app/application.go` — constructs it, injecting its dependencies.
2. `app/controllers/application.go` — one field per sub-controller on the root `Server`.
3. `app/controllers/routes.go` — mounts it.

Worse, kchat builds the root positionally —
`controllers.NewServer(cfg, log, gw, line_controller.NewServer(...), meta_controller.NewServer(...), ...)`
— so adding a controller changes a function signature. **The scaffold must not
copy that.** The root takes named fields, so a new controller is three
independent one-line insertions.

Options considered

| Approach | For | Against |
| --- | --- | --- |
| A. AST editing (`go/ast`, `dst`) | robust to reformatting | fiddly for struct fields and composite literals; hard for agents and humans to predict |
| B. **Marker comments** | greppable, obvious, easy to test | a deleted marker breaks the generator |
| C. Generated registry file, wholly owned by bogie | never edits hand-written code | a second source of truth; regenerated files invite merge noise |

**Recommendation: B for v1.** Built 2026-10-02: `g controller` inserts one
line above each of three markers (`bogie:controllers`, `bogie:wire`,
`bogie:routes`), adds the import, and formats the file in-process with
`go/format` so the gofmt check holds; a rerun is a no-op; every marker is
checked before any file is written, so a missing one means nothing changed.
`d controller` removes the files and the lines and prunes imports the removal
left unused. `doctor` checks each marker appears exactly once and that every
`*_controller` package is mounted. The tool's `bin/ci` generates a controller
and a service into a fresh app, runs that app's pipeline, destroys them, and
runs it again.

```go
type Server struct {
	Config *config.Config
	// bogie:controllers
}

func (s *Server) SetupRoutes(r *gin.RouterGroup) {
	// bogie:routes
}
```

`bogie g controller posts` inserts a line above each marker, idempotently. If a
marker is missing it stops and names the file — it does not guess. `bogie doctor`
checks that every marker exists exactly once. Keep C in reserve if marker drift
proves common.

**Controllers and namespaces, decided 2026-10-03.** The first cut made every
Rails controller a Go package (`posts_controller/`, one file per action). That
is not what kchat has: its `internal_controller` holds conversations, inboxes,
messages and tags, so the package is Rails's `Internal::` module and the file
is the Rails controller. Bogie now maps Rails's two things to Go's two things,
and `g controller` follows Rails's own spelling, where a slash (or `::`) means
a namespace:

- **A Rails controller is a Go type in one file.** `bogie g controller posts
  index show` writes `app/controllers/posts_controller.go` in the `controllers`
  package: `type PostsController struct` holding its dependencies as
  consumer-declared interfaces, `SetupRoutes`, and one method per action. The
  test sits beside it. Three one-line wires as before, with no import: a
  `Posts *PostsController` field, `server.Posts = controllers.NewPostsController(log)`,
  and `s.Posts.SetupRoutes(&r.RouterGroup)`. The type is named
  `PostsController`, not `Posts`, because it matches `posts_controller.rb` one
  for one and says what the file is in a directory that also holds
  `application.go`, `routes.go` and `welcome.go`.
- **A Rails namespace is a Go package.** `bogie g controller internal/conversations
  index show` writes `app/controllers/internal_controller/conversations_controller.go`
  with `type ConversationsController`. If the package does not exist, the
  generator first writes its `server.go` (the namespace `Server`, one named
  field per controller above its own `// bogie:controllers`) and `routes.go`
  (the `/internal` group, where middleware the whole namespace shares goes,
  above its own `// bogie:routes`), and registers the namespace in the root
  with the three root wires. The markers nest one level; the generator checks
  the namespace's markers in the content it is about to write.
- **All construction stays in `app/application.go`.** The namespace `Server`
  is a holder plus routes. The controller's wire line is
  `server.Internal.Conversations = internal_controller.NewConversationsController(log)`,
  inserted after the namespace's own `server.Internal = ...`, so a dependency
  is never threaded through a namespace constructor.
- **The namespace lives for its controllers.** `d controller internal/conversations`
  removes the controller and its three lines; when that leaves the package
  holding only `server.go` and `routes.go`, it removes those and the root
  registration too, since an empty namespace is an unused group variable
  that does not compile.
- **The `_controller` suffix stays on namespace packages**, the one place the
  folder name departs from Rails's `app/controllers/admin/`. Considered and
  rejected on 2026-10-03: a plain `admin/` package is imported into
  `app/application.go` beside every service and layout package, so a
  namespace named like a service (`posts`), or `jobs`, `models`, `config`,
  collides the moment both exist, and Go's only remedy is a hand-written
  import alias on a line the generator wrote. A namespace literally called
  `internal` would also be a Go `internal/` directory, importable only from
  inside `app/controllers`, which `app/application.go` is not. The suffix
  removes both, matches kchat, and ST1003 is already off for it. One level of
  namespace in v1; a version such as `v1` is a route group inside the
  namespace's `routes.go`, as kchat's `/internal/v1` is.

**`g scaffold`, built 2026-10-03.** `rails g scaffold` for an API: the
model, the wire shapes in `app/views`, and a controller with the five
resourceful actions over the store, registered with the store passed in.
No service layer, decided the same day: a scaffold's service was two lines
per action, validate then store, which is the layer Rails does not have
either. Instead the store's create and update run the model's `Validate`,
as Rails validates on save, and the controller consumes a `postsStore`
interface declared beside it (rule 2) so its test needs no database. The
model is singular and the rest plural, as Rails names them, and that is the
one place the tool pluralizes; `g controller` uses the name as typed. A
service is for logic that is not CRUD, and `g service` is there when a
resource outgrows the store. One level: no namespace in v1, and a scaffold
over an existing model is reported as a conflict rather than merged.

**`g authentication`, built 2026-10-03.** Rails 8 has `rails g
authentication`; Bogie's takes a shape, because the two reference services
needed three different things and all of them were a middleware on a route
group. `secret` is kchat's control secret: a header, constant-time
compared, with a Basic fallback for a browser, for the Rails app calling
this service. `token` is line-connect's user sessions, as JWT HS256 rather
than PASETO: line-connect reached for JWT every time it had to talk to
another system, and with HS256 the Rails app mints the token with the `jwt`
gem and this service verifies, so users keep living in Rails. `api_key` is
line-connect's applications table: a public prefix, a bcrypt digest, a key
shown once by `api_keys create`. Each shape writes the middleware and its
test, a `WithSecret`, `WithUser` or `WithAPIKey` group helper on the root
`Server`, and registers its config in `config/config.go` above three new
markers (`bogie:config`, `bogie:env`, `bogie:validate`): the field, the
environment override, and a check that refuses to boot in production
without the secret. The development value is written into the credentials
file by the generator, so the app boots as before; staging and production
are given theirs by hand. `api_key` also registers a subcommand in
`main.go` above `bogie:commands`, which `bogie api_keys:create NAME`
reaches through the proxy's rule that any other colon task is one of the
binary's own. Applying the guard is one edit by hand, mounting a controller
on the group instead of the router, because that line is the exposure
boundary and belongs in `routes.go` where it can be read.

The file rule is one sentence: one file per Rails controller, one package per
Rails namespace. kchat's `conversations.go` already obeys it; "one file per
action" is gone. `doctor` checks that every `*Controller` type is constructed
and mounted, in the root or in its namespace, and that every `*_controller`
package is.

Attribute types for `g model` / `g migration` map Rails-style names to Postgres
types: `string`→`text`, `text`→`text`, `integer`→`integer`, `bigint`→`bigint`,
`boolean`→`boolean`, `datetime`→`timestamptz`, `uuid`→`uuid`, `jsonb`→`jsonb`,
`references`→`uuid` with a foreign key. `g model` writes the migration, a starter
`db/queries/<table>.sql` (get, list, create, update, delete), the domain type
and the store's conversion methods, and runs `sqlc generate`. Built
2026-10-02, with three rules learned on the way: every generated column is
NOT NULL with a type-appropriate default, so sqlc emits plain Go types and the
domain carries no pointers; a generator runs sqlc immediately, because the
migrations are the schema sqlc compiles against and a new file changes the
models before it is applied; and a migration's version is moved forward a
second at a time past any existing one, since goose refuses duplicates and two
generators in one second would make them. Output is pinned by golden files,
and `bin/ci` runs every generator into a fresh app and that app's pipeline.

Templates are `go:embed`ded and rendered with `text/template`. `bogie.toml` records
the Bogie version that generated the app so a later `bogie app:update` knows what
it is upgrading from (§5a).

---

## 5a. `bogie app:update`: moving an app to a newer layout

Decided 2026-10-02. Rails has `rails app:update`: re-run the generator over the
app for the files Rails owns, then prompt per conflicting file. Bogie does the
same job, two differences in how.

**The name is `app:update`, as in Rails.** An earlier draft chose `upgrade`
on the grounds that the colon is Rake syntax and Bogie used spaces. That was
reversed the same day when the command surface adopted Rails's spelling
wholesale (§4): the rule that holds together is "spell it as Rails does", and
`rails app:update` is what a Rails developer will type.

**It is a three-way merge, not a two-way prompt.** `app:update` does not know
what a file looked like when Rails generated it, so every edit the user ever
made surfaces as a conflict. Bogie knows, because `bogie.toml` records the
exact version that generated the app:

| side | what it is | where it comes from |
| --- | --- | --- |
| base | what the old Bogie generated | `go run github.com/bogie-go/bogie@<old> new <name> --module=<module>` into a temp dir; no old templates shipped in the binary |
| theirs | what the current Bogie generates | the embedded templates, rendered to a temp dir |
| ours | the file on disk, with the user's edits | the app |

`git merge-file ours base theirs`, per file. A file never touched updates
silently. A file the user edited in one place while the template changed in
another merges cleanly. Only a real overlap leaves conflict markers, and the
report names those files. It does not guess, the same rule the generators
follow for a missing marker (§5).

```
bogie app:update            refuses on a dirty git tree, so the result is one reviewable diff
                            renders base and theirs, merges into ours
                            one line per file: identical / updated / merged / conflict / create
                            writes the new version to bogie.toml, then runs bogie doctor
bogie app:update --pretend  the report only; nothing written
```

Files the templates never produced are not touched, so everything `bogie g`
and the user wrote is safe by construction. That includes `config/master.key`
and `config/credentials.yml.enc`, which `new` makes outside the templates
because the key is random; `app:update` never looks at them. Templates render deterministically
from `(version, name, module)` and nothing else, which is what makes base
reproducible; `new` must never consult the clock, the environment or the
machine.

**Built 2026-10-03**, before the first tag after all: the module proxy serves
any pushed commit as a pseudo-version, so base is `go run bogie@<recorded>
new` for a development build's app too, and the first real run updated an
app from the morning's commit to the afternoon's, merging an edited
`AGENTS.md` and respecting a deleted `welcome.go`. Per file the report says
`identical`, `updated` (yours was base), `merged`, `conflict`, `create`,
`kept` (the template did not change, or you deleted the file) or `retired`
(no longer generated; left as it is). `go.mod` and `go.sum` are tidied
rather than merged, the marker is rewritten, and the credentials file and
key are never looked at. A `+dirty` recorded version cannot be served by any
proxy, and the command says so and reports two-way without writing.

**When.** Two prerequisites landed in M5 before the tag, or the first upgrade
would have been impossible: the tool's version comes from the build, not a constant; and
`bogie.toml` records that exact version, not a separate layout number. One
version, one source of truth. Built 2026-10-03: a release build sets it with
`-ldflags -X`, a `go install module@vX.Y.Z` carries it in the build info, and
a build from a clone reports the pseudo-version Go stamps from the commit,
`v0.0.0-<date>-<commit>` (`+dirty` with uncommitted changes), so even a
development build's apps say which commit made them. A `dev` build with no tag to compare
falls back to a two-way report with every differing file marked `conflict`.

---

## 6. Configuration and deploy

Both are carried over from kchat unchanged in shape:

- Encrypted credentials, Rails pattern, via `bogie-go/credentials`. The
  `<NAME>_ENV` closed set controls behaviour; `<NAME>_CREDENTIALS` selects
  *which* credentials file, so staging is "production strictness, staging
  secrets"; `<NAME>_MASTER_KEY` carries the key into a container. Keys are
  gitignored. Every file is flat with one section named after the app, whose
  keys give the environment-variable names. **Test never reads a credentials
  file** (decided 2026-10-02): it runs from defaults and the environment, so
  tests need nobody's secrets, `bin/ci` needs no key, and a store test can
  never open the development database. `bogie new` generates
  `config/master.key` and a sealed development file; staging and production
  files are made by `make credentials ENV=<name>` when someone first needs
  them, so the development key opens nothing else.
- Kamal 2. `config/deploy.yml` is staging; production is an overlay reached with
  `-d production`. The container migrates itself at boot.

kchat's own note applies: an edit to `deploy.yml` without a matching edit to the
overlay changes both. The generated `deploy.yml` says so in a comment.

`bogie-go/credentials` is our own library. Publishing bogie means it must be
public, versioned and maintained, or replaced. See §12.

---

## 7. Agent-first

The generated app is read and edited mostly by agents, so the instructions are a
deliverable, not an afterthought.

- **`AGENTS.md`** (with `CLAUDE.md` as a symlink or one-line include): the six
  rules, the commands, the layout, and a short recipe per task — *add an
  endpoint, add a table, add a job, add a channel adapter* — each of which starts
  with "run `bogie g …`".
- **Generators are the deterministic path.** An agent that runs `bogie g
  controller` gets the registration right every time; one that hand-writes a
  controller has to rediscover it.
- **Guardrails fail loudly**: `sqlc generate` in CI, `golangci-lint`, `go vet`,
  `bogie doctor`, and a test that fails if a controller is not registered.
- **Decisions are written down where they'd be found.** kchat's CLAUDE.md records
  the case of a nil-guard that exists for one reason and silently serves another.
  The template comments should name what a line is *for*.

Andurel ships an agent `skill` subcommand for the same reason; worth comparing
notes rather than reinventing.

---

## 8. Testing the tool itself

1. **Golden files** for each generator: given `g controller posts index show`,
   the produced tree and every inserted line match a checked-in expectation.
2. **A scaffold step in `bin/ci`**: run `bogie new`, then run the generated
   app's own `bin/ci` (`go build`, `go vet`, `golangci-lint`, `go test`, and
   later `sqlc generate` against a real Postgres), then start it and ask for
   `/healthz`; later, `bogie g` of every generator followed by the same. This
   is what stops templates rotting.

   **CI runs locally, not on a hosted runner.** Rails 8.1 moved CI to
   `bin/ci`, a script that runs every check on the developer's machine, with
   `gh signoff` recording a green run as a commit status that branch
   protection can require. Bogie follows that: the tool has a `bin/ci`, every
   generated app ships one, and there is no `.github/workflows`. The one tool
   Go does not bring with it, golangci-lint, is looked for on PATH and its
   absence fails the step loudly rather than skipping it.
3. **Marker-removal tests**: delete each marker, expect a named error and no
   partial write.

---

## 9. Extracting from kchat and line-connect

kchat is the reference implementation, not the template. Extraction is pinned
to kchat commit `485b80f` (2026-09-14) so the M4 diff has a fixed target; kchat
keeps moving. Reviewed 2026-09-30 against that commit, these are the findings
the scaffold fixes rather than copies:

- **Positional root wiring.** `controllers.NewServer` takes five controllers
  as ordered arguments (`app/application.go`, `app/controllers/application.go`).
  Adding one changes a signature. Scaffold: named fields plus markers (§5).
- **`spec/` is empty** (three `.gitkeep`s). Scaffold: no `spec/` (§3).
- **Seeds bypass the binary.** Scaffold: `db seed` subcommand (§4).
- **sqlc is a global install.** goose and credentials are already under the
  `tool` directive; sqlc is not. Scaffold: pin all three.
- **The README drifted from the code within two days** (describes `cmd/` and
  `internal/`, says "no `app/`"; the code has used `app/` since the first
  commit). This is the drift `AGENTS.md` plus `bogie doctor` exist to prevent.
- **`config.Load` already runs env-only** when no credentials file exists
  (`source = "environment only (no credentials file)"`), so §12.3 is mostly
  answered.

A second repository, **line-connect** (`klangtech/line-connect/backend`,
~1,070 commits), has asynq jobs in production and was the candidate source for
`app/jobs`. Reviewed 2026-09-30: what is worth keeping is the *shape*, one
`task_<name>.go` per task, `schedule_<name>.go` for periodic work, a worker
heartbeat, and a `critical` queue above `default`. What is not worth keeping
is the code: a `TaskDistributor` interface with one method per task, a
`TaskProcessor` interface with one method per task, hand-written JSON
marshalling in every task, zerolog, and a `log.Fatal` in a constructor. That
boilerplate is what asynq's untyped `[]byte` payload invites, and it is the
reason jobs moved to River (§12.2). The scaffold ports the shape, not the code.

Extraction, in order:

1. Copy the skeleton: `config`, `lib/logger`, `app/services/database`,
   `app/services/migrate`, `app/middlewares`, `cli.go`, the Makefile, the
   Dockerfile, the Kamal files.
2. Strip everything channel-shaped: LINE, Meta, `omnichat`, gateway, bus, media,
   templates, ingest.
3. ~~Keep one small example resource end to end — migration, query, model,
   service, controller, test — so a new app has a working shape to copy.~~
   Reversed 2026-10-03: `bogie new` makes an empty app, as `rails new` does.
   The example made every new app start by deleting three things, and its
   seed outlived `d model post`, which broke `db:prepare` on the emptied
   app. The working shape now comes from the generators themselves, and
   `bin/ci` proves it by generating a model, a model that references it, a
   migration, a controller, a namespace and a service into a fresh app and
   running that app's pipeline, then destroying them all and running it
   again. An app with no models compiles and serves: `db.Migrations` embeds
   the directory with `all:`, the app's `bin/ci` and `make sqlc` skip sqlc
   when `db/queries` is empty, and the tool's `g model` runs sqlc as soon as
   there is a query file.
4. Turn the positional root wiring into named fields with markers (§5).
5. Convert the global `sqlc` install to a pinned tool.
6. Add River behind `--jobs`: one `river.Worker[Args]` per file in
   `app/jobs`, registered on the `river.Workers` bundle with a
   `// bogie:jobs` marker, and `worker` as a role of the app binary exactly
   as kchat does. `g job send_welcome` writes the args struct, the worker, a
   test, and the registration line. Built 2026-10-02. River's schema is
   applied by `migrate` through `rivermigrate` rather than copied into
   `db/migrate` as first planned: River versions its own tables in
   `river_migration` and ships the SQL with the library, so a River upgrade
   brings its schema change with it instead of needing a hand-copied
   migration. The web process holds a client it never starts, so it only
   inserts; the worker process starts the same client. River v0.48.0.
6a. **`g job` on an app with no jobs adds them first.** Built 2026-10-04,
    once the asymmetry was noticed: `--jobs` is opt-in at `new` time, but
    there was no way back in short of hand-copying nine files. `g job`
    renders the app twice, Jobs false and Jobs true, and merges the
    difference into the app exactly as `app:update` merges a version
    change — base and theirs now vary `Jobs` instead of (or alongside) the
    recorded version. Only the files `--jobs` touches ever differ between
    the two renders, so only those show up as anything but identical, and a
    file the user has already edited merges rather than being overwritten.
    Refuses, as `app:update` does, on a dirty git tree, since it touches
    several files in one diff; also refuses an app not yet brought to this
    bogie's version, since base would otherwise be rendered from the wrong
    templates and every unrelated template change since would show up as
    noise in what is meant to be a jobs-only diff. Caught on a scaffolded app
    that already had generators run on it, not a bare one: two of theirs's
    lines land at an anchor another generator also writes to (the next
    import after `app/controllers`, and the command map above
    `bogie:commands`, which gofmt column-aligns, so even an untouched entry's
    spacing shifts once a longer key joins) — a blind 3-way merge cannot
    tell two independent insertions apart there, so those two lines are
    stripped out of theirs before merging and added back afterwards through
    the same marker mechanism (`markers.Insert`, `markers.AddImport`) every
    other generator already uses for exactly this. Chasing it down also
    surfaced a real, independent bug in `markers.PruneImports`: it named a
    versioned import (`.../pgx/v5`) by its last path segment ("v5") rather
    than the package's actual name ("pgx"), so destroying any controller or
    service on a --jobs app silently deleted the still-used pgx import and
    broke the build. Fixed the same day: a last segment matching `v[0-9]+`
    is skipped in favor of the segment before it.
    The alternative considered to all of this was flipping the default to
    jobs-on with `--no-jobs` to opt out, rejected because it taxes every app
    with the worker role and River's dependencies for a problem that was
    really "no way back",
    not "the wrong default."
7. **Later:** regenerate a clean app with the tool and diff it against kchat.

**Done 2026-10-03 (M4).** `bogie new kchat --module=github.com/klangtech/kchat
--jobs` against commit `485b80f`, skeleton file by skeleton file. Findings:

- **The deploy half was missing.** §3 and §6 promised a Dockerfile and
  Kamal config and the templates had neither; `bin/ci` never noticed
  because nothing built an image. Added, ported from kchat: `Dockerfile`
  (one image, every role, no CMD, the schema inside), `.dockerignore` (keys
  never enter the context), `config/deploy.yml` as staging with
  `deploy.production.yml` as the overlay, and `.kamal/secrets` reading the
  key from disk and the registry password from the credentials through the
  pinned tool. `--jobs` adds the `job` role as the same image run as
  `worker`. The tool's `bin/ci` now builds the image from a fresh app and
  checks it carries the credentials and not the key.
- **The logger names the service.** kchat's `.With("service", "kchat")` is
  what tells its lines from the Rails app's in a shared shipper. Adopted.
- **The rest of the diff is what the extraction intended:** named fields
  and markers instead of positional wiring, seeds through the binary, a
  goose provider with its own version table per source, River's schema at
  boot, a bounded boot migration, `--help` on the binary. kchat's
  `APP_ENV` is `<NAME>_CREDENTIALS` here (§6, decided). Not adopted:
  kchat's `mise.toml` (the Go version is `go.mod`'s), its `script/`
  helpers (product-specific), and its `spec/` (§3).
- **What kchat would gain from the tool**, for when it is regenerated for
  real: the markers and `doctor`, `db seed` through the binary, pinned
  sqlc, River in place of asynq (§12.2), and the `internal_controller`
  namespace maps straight onto `g controller internal/...` with one file
  per controller, which its `conversations.go` already is.
   Where they disagree, one of them is wrong.

Do this in the new repository, not in kchat or line-connect. Both keep shipping.

---

## 10. Prior art

Found by search on 2026-09-28, rechecked 2026-09-30. Not audited; star counts
and details are as reported by GitHub and package pages at the time.

| Project | Shape | Why not join it |
| --- | --- | --- |
| [Andurel](https://github.com/mbvlabs/andurel) | scaffold-only, v2, `new`/`generate`/`db`/`doctor`/`upgrade`/`skill`, JSON discovery, ships an `AGENTS.md`; MIT; 224 stars, one maintainer, five commits on 2026-09-29 alone | Full-stack and heavy: Echo, Uber Fx DI, Templ + Datastar or Inertia with React/Vue/Svelte, River, Tailwind, its own typed-SQL tool. Owns the "Rails-like Go framework for humans and agents" slot. A Rails shop that already has a Rails app does not want a second full-stack framework. |
| [go-blueprint](https://github.com/Melkeydev/go-blueprint) | `create` only; 9,000 stars | Proves demand for `new`. No generators, no wiring, no agent layer. |
| [goforge](https://github.com/VictorTarnovski/goforge) | `new` + `generate domain` with auto-wiring, `.agents/rules`, `AGENTS.md`; 0 stars, 4 commits | Nearly our thesis, `internal/` layout, Cobra, OIDC. Evidence the idea is in the air and that shipping it quietly gets nothing. |
| [Buffalo](https://github.com/gobuffalo/buffalo) | full framework with its own runtime, ORM and templates | Wrong shape. |
| [Goravel](https://github.com/goravel/goravel) | Laravel clone | Wrong audience. |
| [Gon](https://github.com/mickamy/gon) | `g scaffold` only, clean architecture, Echo | Generators only; no `new`. |
| geng | Nest-inspired generator for Gin apps | Same router, different model; **not yet read**. |
| Autostrada, go-web-scaffold | project scaffolds | Not Rails-shaped; **not yet read**. |

Worth borrowing from Andurel: `doctor`, `upgrade`, the agent skill, and a lock
file recording the generator version (`bogie.toml`).

Worth reading before building: geng, since it targets Gin, and Autostrada.

Positioning: **the Go service next to your Rails app.** Not "a Rails-like Go
framework"; Andurel holds that and does it well. Bogie competes on being thin,
API-only, and on the mapping from what a Rails developer knows to what the Go
code does (`docs/FROM_RAILS.md`). Every feature Andurel has and Bogie drops is
a point in Bogie's favour, not against it.

---

## 11. Naming

**Chosen: Bogie.** Decided 2026-09-28.

A bogie (pronounced "BOH-gee"; American English: *truck*) is the wheeled frame
with axles and suspension under a locomotive, carriage or wagon. The car body
sits on it and it carries the weight along the track, pivoting so a long car can
follow a curve. That is the tool's job: the frame that lets a Go service run on
Rails-shaped tracks. It also continues the railway naming of Cargo and Loco.

    bogie new blog

Why it won

- Five letters, distinctive, and the only candidate that literally "goes on rails".
- The registries were clear when checked on 2026-09-28: no Homebrew formula, and
  on pkg.go.dev only an abandoned 2017 CLI (`sethpollack/bogie`, v0.0.6) and
  unrelated packages.

Reserved

- **GitHub org: `bogie-go`.** Claimed 2026-09-29 (the user `bogie` was taken by
  an unrelated individual). Repository will be `github.com/bogie-go/bogie`.
- **Domain: `bogie-go.com`.** Confirmed available 2026-09-29, not yet
  registered. Chosen over `.org` (also available) because `.com` is what people
  type by default and carries no nonprofit/foundation connotation. `.dev` is
  worth grabbing too if available, as a redirect — more idiomatic for a dev
  tool but not required.

Still to do before announcing

- **Register the domain** before someone else does.
- **RubyGems.** A gem named `bogie` exists (v0.1.0, May 2024, ~35k downloads,
  MIT; its repository returned 404 when checked, so what it does is unknown).
  Not a conflict for a Go tool, but a Rails developer searching for "bogie" may
  land on it. Look at what it is before the first release.

Costs to accept

- "Bogie" reads as "bogey" — a golf score over par, an unidentified aircraft, or
  British slang for something unwanted. In railway English the spelling is
  *bogie*, and people will misspell it.
- Outside Britain few people know the railway meaning. Explain it once, in the
  README's first paragraph.

Rejected, with reasons

| Name | Why |
| --- | --- |
| `gor` (working name) | GoReplay's binary is `gor`, and the project is commonly called "Gor"; GoRails (gorails.com, since 2014) is a well-known Rails screencast site |
| Gauge | ThoughtWorks test-automation tool, binary `gauge` |
| Gantry | an existing Go CLI |
| Gails | an old Go package, one letter from Grails |
| Loco | the Rust equivalent of this project |
| Spur | Spur Intelligence's Go CLI uses the binary `spur`; Rancher has a `spur` CLI library |

Considered but not checked: **Sleeper** (the beam under the rails, which is the
best metaphor for a scaffold, but a common word), **Ballast**, **Turnout**,
**Roundhouse**.

---

## 12. Open questions and risks

1. **Views.** ~~Open~~ **Decided 2026-09-30: API-only.** No HTML in v1. The
   Rails app renders; that is the whole premise.
2. **Jobs.** ~~Open~~ **Decided 2026-09-30: River, opt-in via `--jobs`.**
   asynq (v0.26.0, 2026-02) and River (release 2026-08-31, 4.6k stars) are
   both maintained. River wins on the tool's own principles:
   - **Postgres-only is already decided (§12.4).** River uses the pgx/v5 pool
     the app already has. asynq adds Redis: a Kamal accessory, a URL in
     credentials, a second thing to be down.
   - **Transactional enqueue** is what a Rails developer expects from
     ActiveJob with Solid Queue: the job row commits with the business row or
     not at all. With Redis the enqueue can race the commit.
   - **Typed args via generics** (`river.Worker[SendWelcomeArgs]`) remove the
     marshal/unmarshal and interface-per-task boilerplate that line-connect's
     asynq layer needed (§9). Less code for an agent to get wrong.
   - Andurel chose River too. Weak evidence on its own, but it means the
     ecosystem is converging and a Rails developer will find examples.
   Costs accepted: River is younger (2023); it needs its own tables, shipped
   as the scaffold's first goose migration; it polls Postgres, which is fine
   at the scale a Rails shop's side service runs at; it has no asynqmon, but
   River UI exists. Revisit only if a deployment needs Redis-class throughput.
3. **`bogie-go/credentials`.** ~~Open~~ **Decided 2026-10-02.** The library
   moved from `roonglit/credentials` into the `bogie-go` organisation and is
   public, tagged v1.5.0 under the new path with the code unchanged from
   1.4.0. Named in the plural because Rails says `credentials:edit`,
   `credentials.yml.enc`, and so does the package and command inside it.
   `config.Load` already falls back to environment-only when no file exists,
   so credentials ship as the default and env-only is documented.
4. **Postgres-only.** sqlc supports others, but goose dialects, type mapping and
   tests multiply. Postgres only in v1.

   **SQLite, considered and declined 2026-10-03.** Rails 8 defaults to
   SQLite, so the question will come up again; this is why the answer is
   still no. The database is wired in nine places, and SQLite changes every
   one: the driver (`modernc.org/sqlite` over `database/sql` instead of a
   pgx pool), `DATABASE_URL` becoming a file per environment under
   `storage/`, `db create` and `db drop` becoming file operations, the sqlc
   engine, a second column in the `g model` type table (`uuid` as `TEXT`
   with the id made in Go since there is no `gen_random_uuid()`, `datetime`
   as ISO text with `_time_format` on the connection, `jsonb` as `TEXT`,
   booleans as integers), store methods without `pgtype`, the test store's
   `TRUNCATE ... CASCADE` becoming `DELETE FROM`, Kamal needing a volume
   and a single replica, and `bin/ci` scaffolding a third app. All of that
   is tractable. What decides it is jobs: River's SQLite driver, added in
   v0.23, is described by River as an early preview with little real-world
   use that wants a pool of one connection, so `--jobs` would have to
   refuse SQLite or ship on a preview. Bogie's premise is the service next
   to a Rails app that already runs Postgres, and the thing that service
   most often exists for, a queue, a webhook receiver, a socket gateway, is
   the thing SQLite serves least well. So Postgres stays the only database
   until River calls its SQLite driver stable, and then it comes in as
   `bogie new NAME --database=sqlite3`, Rails's spelling, with Postgres
   still the default. Not before.
5. **Gin's future.** A single dependency behind the controller layer, and rule 5
   keeps it there, so swapping is contained. Confirm the layer holds in the scaffold.
6. **Template drift.** Mitigated by §8.2; only if that job actually runs on every change.
7. **Scope creep.** The list of missing Rails features is endless. Each addition
   should pass: is there an ecosystem tool? If yes, wire it; if no, is the gap
   large enough to be worth owning?
8. **Telemetry.** None. State it in the README; an install-and-run tool that
   phones home costs trust.
9. **License and governance.** MIT, single maintainer initially. Say so.
10. **The domain package name.** ~~Open~~ **Decided 2026-09-30: `app/domain/`,
    fixed.** kchat calls it `app/omnichat`, a product name no generated app
    would share. A fixed name is greppable, the templates never need the app
    name, and the generators always know where the domain lives.
11. **Migrate at boot.** ~~Open~~ **Decided 2026-10-02: on by default,
    `<NAME>_MIGRATE_AT_BOOT=false` to disable.** kchat applies migrations
    before opening the pool. goose holds a Postgres advisory lock for the
    run, so two containers booting together serialise rather than race; the
    second finds nothing to do. The flag exists for a deployment that
    migrates from one place on purpose. The run is capped at two minutes so
    a stuck lock fails a deploy loudly instead of hanging it.

---

## 13. Milestones

- **M0 — decide.** ~~Name~~ (Bogie, §11); ~~claim the GitHub org~~; register
  the domain; ~~answers to §12.1–12.3~~ and ~~§12.10~~ (decided 2026-09-30);
  read geng and Autostrada; draft the launch essay (done, in review).
- **M1 — skeleton.** Repo, `bogie new` producing an app that builds, migrates,
  seeds, reads its credentials and serves; `bin/ci` green. **Done 2026-10-02.**
- **M2 — generators.** `g migration`, `g model`, `g controller`,
  `g service`, `g job`, `d`, markers, `doctor`, golden tests, marker-removal
  tests, River behind `--jobs`. **Done 2026-10-02.**
- **M3 — agent layer.** `AGENTS.md`, recipes, `docs/FROM_RAILS.md`, a lint rule
  or test that catches an unregistered controller.
- **M4 — dogfood.** Regenerate the kchat skeleton with the tool and diff (§9,
  "Done 2026-10-03"). It found the deploy half missing; fixed the same day.
- **M5 — publish.** README, first tagged release, `go install` as the one
  install (no Homebrew tap, decided 2026-10-03: a tool for people about to
  write Go can ask for Go). Before the tag: version from the build, recorded
  as-is in `bogie.toml` (§5a). Done except the tag and the essay.
- **M6 — app:update.** `bogie app:update` as a three-way merge (§5a); the
  first feature after the first release, because it needs two versions to exist.
