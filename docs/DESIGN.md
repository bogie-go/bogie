# Go on Rails — design

Status: **draft for review**, 2026-09-28. Nothing here is built.

The tool is called **Bogie** (§11): the wheeled frame under a rail car that
carries it along the track. The command is `bogie`.

---

## 0. The one-line version

A CLI that scaffolds a Rails-shaped Go service out of tools the Go ecosystem
already ships, and builds only the parts that have no equivalent.

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
| Background jobs (ActiveJob) | asynq (Redis) — *open, see §12* | ecosystem |
| Credentials | `roonglit/credentials` | ecosystem (ours) |
| Deploy | **Kamal 2** | ecosystem |
| Lint / test | golangci-lint, `go test` | ecosystem |
| Task runner | `make` | ecosystem |
| Logging | `log/slog` | stdlib |
| Tool versions | Go 1.24+ `tool` directive in `go.mod` | ecosystem |
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
install. The scaffold pins sqlc and goose with `go get -tool` so a fresh clone
needs only Go.

---

## 3. What a generated app looks like

Taken from kchat's layout (`docs/STRUCTURE.md`), with everything channel-specific removed.

```
main.go                       dispatches: serve | worker | db | migrate
cli.go                        the subcommands (see §4)
bogie.toml                      layout version — which templates generated this
AGENTS.md                     conventions, commands, recipes (§7)
app/
  application.go              wiring: config, connections, controllers
  controllers/
    application.go            root Server, one NAMED field per controller
    routes.go                 the whole routing table
    <name>_controller/        server.go · routes.go · one file per action
  middlewares/                request id · logging · recover
  services/<name>/            ctx first, knows nothing about HTTP
  models/                     sqlc output + the store
  views/                      wire contracts; domain structs never double as wire structs
  jobs/
config/                       encrypted credentials → validated struct, no globals
db/
  migrate/                    goose SQL, timestamped, go:embed
  queries/                    sqlc input
  seeds/
lib/logger/
spec/{factories,fixtures,support}
docs/
Makefile · Dockerfile · docker-compose.yml · config/deploy.yml · .golangci.yml
```

The rules ship in `AGENTS.md` and are enforced where a linter can enforce them
(kchat `docs/STRUCTURE.md` §3):

1. `ctx context.Context` first for anything doing I/O.
2. Interfaces are declared by the consumer.
3. `app/<domain>` imports nothing third-party.
4. Constructors return `(T, error)`; only `main` exits.
5. `*gin.Context` never travels below `app/controllers`.
6. No `util/`, `helpers/`, `common/`.

`.golangci.yml` disables ST1003 for the underscore package names, exactly as kchat does.

---

## 4. Two CLIs, and why

| | installed tool `bogie` | the app's own binary |
| --- | --- | --- |
| Where it runs | a developer's laptop | laptop **and** the deployed container |
| Knows about | templates, the layout | config, database, the running app |
| Commands | `new`, `g`, `d`, `doctor`, `upgrade` | `serve`, `worker`, `db …`, `migrate …` |

The runtime commands cannot live only in `bogie`: on a deployed host there is no
generator, but there must be a way to migrate and roll back. kchat already does
this — `docker exec <container> kchat migrate status` — because the migrations
are embedded and the binary that applies them is the only thing that can roll
them back. Shipping the goose CLI separately would carry a second copy of the
migrations and invite version skew.

So `bogie db migrate` is a thin proxy for `go run . db migrate` inside the app, and
`make db-migrate` calls the same thing. **Development runs the code production
runs** (kchat Makefile, comment above `db-create`).

Commands, v1:

    bogie new NAME [--db=postgres] [--redis] [--deploy=kamal]
    bogie g migration add_slug_to_posts slug:string
    bogie g model post title:string body:text
    bogie g controller posts index show create
    bogie g service publish_post
    bogie d controller posts            # destroy what g created
    bogie server | worker | test | lint
    bogie db create | drop | migrate | rollback | seed | reset | status
    bogie credentials edit [-e production]
    bogie doctor                        # layout and markers intact? tools pinned?

Later: `g job`, `g mailer`, `upgrade` (re-render templates, show a diff),
`deploy`.

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

**Recommendation: B for v1.**

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

Attribute types for `g model` / `g migration` map Rails-style names to Postgres
types: `string`→`text`, `text`→`text`, `integer`→`integer`, `bigint`→`bigint`,
`boolean`→`boolean`, `datetime`→`timestamptz`, `uuid`→`uuid`, `jsonb`→`jsonb`,
`references`→`uuid` with a foreign key. `g model` writes the migration, a starter
`db/queries/<table>.sql` (get, list, create, update, delete) and runs `sqlc generate`.

Templates are `go:embed`ded and rendered with `text/template`. `bogie.toml` records
the layout version so a later `bogie upgrade` knows what it is upgrading from.

---

## 6. Configuration and deploy

Both are carried over from kchat unchanged in shape:

- Encrypted credentials, Rails pattern. `KCHAT_ENV`-style closed set controls
  behaviour; a second variable selects *which* credentials file, so staging can
  be "production strictness, staging secrets". Keys are gitignored.
- Kamal 2. `config/deploy.yml` is staging; production is an overlay reached with
  `-d production`. The container migrates itself at boot.

kchat's own note applies: an edit to `deploy.yml` without a matching edit to the
overlay changes both. The generated `deploy.yml` says so in a comment.

`roonglit/credentials` is our own library. Publishing bogie means it must be
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
  `bogie doctor`, and a test that fails if a controller package is not registered.
- **Decisions are written down where they'd be found.** kchat's CLAUDE.md records
  the case of a nil-guard that exists for one reason and silently serves another.
  The template comments should name what a line is *for*.

Andurel ships an agent `skill` subcommand for the same reason; worth comparing
notes rather than reinventing.

---

## 8. Testing the tool itself

1. **Golden files** for each generator: given `g controller posts index show`,
   the produced tree and every inserted line match a checked-in expectation.
2. **A scaffold job in CI**: run `bogie new`, then in the result run `go build`,
   `go vet`, `sqlc generate`, `golangci-lint`, `go test`, and `bogie g` of every
   generator followed by the same. Against a real Postgres. On the Go versions
   we claim to support. This is what stops templates rotting.
3. **Marker-removal tests**: delete each marker, expect a named error and no
   partial write.

---

## 9. Extracting from kchat

kchat is the reference implementation, not the template. Extraction, in order:

1. Copy the skeleton: `config`, `lib/logger`, `app/services/database`,
   `app/services/migrate`, `app/middlewares`, `cli.go`, the Makefile, the
   Dockerfile, the Kamal files.
2. Strip everything channel-shaped: LINE, Meta, `omnichat`, gateway, bus, media,
   templates, ingest.
3. Keep one small example resource end to end — migration, query, model,
   service, controller, test — so a new app has a working shape to copy.
4. Turn the positional root wiring into named fields with markers (§5).
5. Convert the global `sqlc` install to a pinned tool.
6. **Later:** regenerate a clean app with the tool and diff it against kchat.
   Where they disagree, one of them is wrong.

Do this in the new repository, not in kchat. kchat keeps shipping.

---

## 10. Prior art

Found by search on 2026-09-28. Not audited; star counts and details are as
reported by GitHub and package pages at the time.

| Project | Shape | Why not join it |
| --- | --- | --- |
| [Andurel](https://github.com/mbvlabs/andurel) | scaffold-only, `new`/`generate`/`db`; MIT; ~224 stars | Echo, Fx, Templ + Datastar, River, Postgres-only. Closest in spirit; author calls it "still very exploratory". Ours is Gin, sqlc, Rails-shaped. |
| [Buffalo](https://github.com/gobuffalo/buffalo) | full framework with its own runtime, ORM and templates | Wrong shape. |
| [Goravel](https://github.com/goravel/goravel) | Laravel clone | Wrong audience. |
| [Gon](https://github.com/mickamy/gon) | `g scaffold` only, clean architecture, Echo | Generators only; no `new`. |
| geng | Nest-inspired generator for Gin apps | Same router, different model; **not yet read**. |
| Autostrada, go-web-scaffold | project scaffolds | Not Rails-shaped; **not yet read**. |

Worth borrowing from Andurel: `doctor`, `upgrade`, the agent skill, and a lock
file recording the generator version (`bogie.toml`).

Worth reading before building: geng, since it targets Gin, and Autostrada.

Positioning: **the thin, Rails-shaped one.** Not more features than Andurel —
a clearer mapping from what a Rails developer knows to what the Go code does.

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

1. **Views.** kchat renders no HTML. Rails developers will expect them. v1 could
   be API-only, or add `templ` / `html/template`. Leaning API-only.
2. **Jobs.** kchat uses asynq (Redis). River (Postgres-backed) needs no second
   datastore. Both are ecosystem tools; which is the default?
3. **`roonglit/credentials`.** Ours, small, and now on the critical path of an
   open-source tool. Either commit to maintaining it publicly or offer a plain
   env-var mode.
4. **Postgres-only.** sqlc supports others, but goose dialects, type mapping and
   tests multiply. Postgres only in v1.
5. **Gin's future.** A single dependency behind the controller layer, and rule 5
   keeps it there, so swapping is contained. Confirm the layer holds in the scaffold.
6. **Template drift.** Mitigated by §8.2; only if that job actually runs on every change.
7. **Scope creep.** The list of missing Rails features is endless. Each addition
   should pass: is there an ecosystem tool? If yes, wire it; if no, is the gap
   large enough to be worth owning?
8. **Telemetry.** None. State it in the README; an install-and-run tool that
   phones home costs trust.
9. **License and governance.** MIT, single maintainer initially. Say so.

---

## 13. Milestones

- **M0 — decide.** ~~Name~~ (Bogie, §11); claim the GitHub org and domain;
  answers to §12.1–12.3; read geng and Autostrada.
- **M1 — skeleton.** Repo, `bogie new` producing an app that builds, migrates and
  serves; CI scaffold job green.
- **M2 — generators.** `g migration`, `g model`, `g controller`, `g service`,
  `d`, markers, `doctor`, golden tests.
- **M3 — agent layer.** `AGENTS.md`, recipes, `docs/FROM_RAILS.md`, a lint rule
  or test that catches an unregistered controller.
- **M4 — dogfood.** Regenerate the kchat skeleton with the tool and diff (§9.6).
- **M5 — publish.** README, Homebrew tap, first tagged release.
