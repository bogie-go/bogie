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
| Credentials | `roonglit/credentials` | ecosystem (ours) |
| Deploy | **Kamal 2** | ecosystem |
| Lint / test | golangci-lint, `go test` | ecosystem |
| CI | **`bin/ci`**, run locally, Rails 8.1 style; `gh signoff` records the green run on the commit | **gap** (a port) |
| Task runner | `make` | ecosystem |
| Logging | `log/slog` | stdlib |
| Tool versions | Go 1.25+ (what gin v1.12 needs); `tool` directive in `go.mod` | ecosystem |
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

Seeds go through the binary too. kchat's `make db-seed` shells out to the goose
CLI with a URL pulled from a second tool, so it is the one path that can drift
from what the server resolves. The scaffold embeds `db/seeds` beside
`db/migrate` and `db seed` is a subcommand like the rest.

Commands, v1:

    bogie new NAME [--db=postgres] [--jobs] [--deploy=kamal]   # --jobs adds River; no extra datastore
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
3. Keep one small example resource end to end — migration, query, model,
   service, controller, test — so a new app has a working shape to copy.
4. Turn the positional root wiring into named fields with markers (§5).
5. Convert the global `sqlc` install to a pinned tool.
6. Add River behind `--jobs`: its schema as a goose migration in
   `db/migrate` (River publishes the SQL; `river migrate-get`), one
   `river.Worker[Args]` per file in `app/jobs`, registered on the
   `river.Workers` bundle with a `// bogie:jobs` marker, and `worker` as a
   role of the app binary exactly as kchat does. `g job send_welcome` writes
   the args struct, the worker, a test, and the registration line.
7. **Later:** regenerate a clean app with the tool and diff it against kchat.
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
3. **`roonglit/credentials`.** Mostly answered: `config.Load` already falls back
   to environment-only when no file exists. Ship credentials as the default,
   document env-only, and commit to maintaining the library publicly.
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
10. **The domain package name.** ~~Open~~ **Decided 2026-09-30: `app/domain/`,
    fixed.** kchat calls it `app/omnichat`, a product name no generated app
    would share. A fixed name is greppable, the templates never need the app
    name, and the generators always know where the domain lives.
11. **Migrate at boot.** kchat applies migrations before opening the pool,
    which is right for one container and a race for two starting together.
    Keep it as the default with a flag to disable, and a comment saying why.

---

## 13. Milestones

- **M0 — decide.** ~~Name~~ (Bogie, §11); ~~claim the GitHub org~~; register
  the domain; ~~answers to §12.1–12.3~~ and ~~§12.10~~ (decided 2026-09-30);
  read geng and Autostrada; draft the launch essay (done, in review).
- **M1 — skeleton.** Repo, `bogie new` producing an app that builds, migrates and
  serves; CI scaffold job green.
- **M2 — generators.** `g migration`, `g model`, `g controller`, `g service`,
  `d`, markers, `doctor`, golden tests.
- **M3 — agent layer.** `AGENTS.md`, recipes, `docs/FROM_RAILS.md`, a lint rule
  or test that catches an unregistered controller.
- **M4 — dogfood.** Regenerate the kchat skeleton with the tool and diff (§9.6).
- **M5 — publish.** README, Homebrew tap, first tagged release.
