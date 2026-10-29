<p align="center"><img src="docs/bogie.png" width="260" alt="Bogie's mascot: a gopher and a red gem riding a mine cart along a track"></p>

<h1 align="center">Bogie</h1>

<p align="center"><strong>A Go API service, from zero to production.</strong><br>
A CLI that scaffolds an API-only, Postgres-only Go service
out of Gin, sqlc, goose, River and Kamal, with generators that also wire the code in.</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/bogie-go/bogie"><img src="https://pkg.go.dev/badge/github.com/bogie-go/bogie.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/bogie-go/bogie"><img src="https://goreportcard.com/badge/github.com/bogie-go/bogie" alt="Go Report Card"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/bogie-go/bogie" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/bogie-go/bogie" alt="MIT License"></a>
  <a href="https://github.com/bogie-go/bogie/releases/latest"><img src="https://img.shields.io/github/v/release/bogie-go/bogie?sort=semver" alt="Release"></a>
</p>

<p align="center"><img src="docs/assets/bogie-demo.gif" width="720" alt="Terminal demo, about 38 seconds: bogie new demoapp, make up to start Postgres, bogie g controller and bogie g scaffold post printing their create and insert lines, bogie db:prepare, bogie s, then curl calls that return a 501 stub from the new controller, create a post with a 201, and list it back"></p>

---

Go gives you freedom, and with it a long list of decisions before the first
endpoint works: which router, how to talk to the database, where migrations
live, how jobs run, how secrets are stored, how it ships. Getting those right
is the hard part of a Go service, and every team makes them again from
scratch.

**Why Bogie exists.** Bogie makes those decisions once, out of tools the Go
ecosystem already ships, so a service starts from a layout and a pipeline that
already work. It builds only what has no equivalent: the generator that creates
a new service, and generators that also **register** what they write, because
Go has no autoload.

The generated app **never imports Bogie**. Delete the tool tomorrow and the
app does not notice.

<p align="center"><img src="docs/welcome.webp" width="640" alt="The welcome page a new app serves at / in development"></p>

## Install

```sh
go install github.com/bogie-go/bogie@latest
```

You need **Docker with Compose v2** (or your own Postgres, see
[below](#your-own-postgres-without-docker)) and **Go 1.26+**. The tool itself builds
with Go 1.25, but the app it generates needs 1.26, because the pinned sqlc
requires it. The generated app runs its Postgres in Compose on port 5440, so it
stays clear of a Postgres already running on 5432.
sqlc and goose are pinned in the generated app's `go.mod` under `tool`, so
there is nothing else to install globally.

Required for `bin/ci`: [golangci-lint v2](https://golangci-lint.run/)
(`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`).
Optional: `gh extension install basecamp/gh-signoff`, so a green run signs off
the commit.

`@latest` is the newest tagged release, and `bogie version` prints it. For
`main` as it stands, `go install github.com/bogie-go/bogie@main`; for a
clone, `go install .`.

## Quickstart

```sh
bogie new blog                                  # an empty API service
cd blog
make up                                         # Postgres on :5440 via docker compose
bogie g scaffold post title:string body:text    # migration, SQL, domain type, store, controller, wired
bogie db:prepare                                # create, migrate, seed
bogie s                                         # serves on :8080; migrates at boot,
                                                #   rebuilds and restarts on save
```

From another terminal:

```sh
curl localhost:8080/healthz
curl -X POST localhost:8080/posts -H 'content-type: application/json' \
     -d '{"title":"Hello","body":"from Go"}'
curl localhost:8080/posts
```

Tests use their own database, `blog_test`. Create it once before `bogie test`:

```sh
BLOG_ENV=test bogie db:prepare    # the prefix is the app name, upper-cased: bogie new shop → SHOP_ENV
bogie test
```

`bin/ci` rebuilds the test database itself. Postgres for every Bogie app listens
on :5440, so run `make down` in one app before `make up` in another.

### Your own Postgres, without Docker

Docker only runs the bundled Postgres; the app needs one it can reach. To use
one you already run, point the app at it (the role needs `CREATEDB`, or create
the databases first):

```sh
export DATABASE_URL='postgres://USER:PASSWORD@localhost:5432/blog_development?sslmode=disable'
bogie db:prepare
bogie s
```

To keep the URL out of your shell, set `database.url` with
`bogie credentials:edit` instead. Tests never read the credentials or
`DATABASE_URL`, so a run can never touch development data; they take
their own URL:

```sh
export BLOG_TEST_DATABASE_URL='postgres://USER:PASSWORD@localhost:5432/blog_test?sslmode=disable'
BLOG_ENV=test bogie db:prepare
bogie test
```

`make up`, `make down`, `make psql` and `bin/ci`'s database step
(`docker compose up`) still use Docker.

`bogie g scaffold` prints what it did:

```
      create  db/migrate/20261003054047_create_posts.sql
      create  db/queries/posts.sql
      create  app/models/post.go
      create  db/adapter/posts.go
      create  db/adapter/posts_test.go
      create  test/factories/posts.go
      create  app/views/posts.go
      create  app/controllers/posts_controller.go
      create  app/controllers/posts_controller_test.go
      insert  app/controllers/application.go: Posts *PostsController
      insert  app/application.go: server.Posts = controllers.NewPostsController(store, log)
      insert  app/controllers/routes.go: s.Posts.SetupRoutes(&r.RouterGroup)
         run  sqlc generate
```

`bogie d scaffold post` takes all of it out again, including the three
registration lines. `bogie doctor` checks that every marker is intact and
every controller is constructed and mounted.

## What you get

One command surface, spelled the same everywhere:

| Command | What it does |
| --- | --- |
| `bogie new blog` | an empty API service (`--jobs` adds River, `--id=uuid` swaps the default bigint id scheme for uuid) |
| `bogie g model post title:string` | migration, queries, model type, store, then sqlc |
| `bogie g scaffold post …` | the model, JSON views and a wired controller; `--only index,show` writes just those actions; `admin/pages` is a model of its own (`admin_page`, table `admin_pages`) with its controller in the admin namespace |
| `bogie g scaffold_controller admin/categories` | the HTTP half over a model that already exists, mounted under a namespace; the attributes are read from the model |
| `bogie g controller posts index show` | a controller, registered as it is written |
| `bogie g controller admin/reports` | a namespace is a package |
| `bogie g job send_welcome` | a River worker on the same Postgres |
| `bogie g config smtp address:req port:int=1025 password:secret` | a typed group of the credentials file, registered, with development values; `smtp.address` is `SMTP_ADDRESS` |
| `bogie g authentication secret\|token\|api_key` | a middleware for a route group (shared secret, JWT bearer token, or hashed API key), its config and credentials entry, and a `WithSecret` / `WithUser` / `WithAPIKey` helper; you mount the routes it guards |
| `bogie db:migrate`, `db:rollback`, `db:seed` | run by the app's own binary |
| `bogie credentials:edit` | an encrypted file, key never committed |
| `bogie s`, `bogie t`, `bin/ci` | serve with live reload, test, and the whole pipeline |

Every new app also ships:

- **`AGENTS.md`** (and `CLAUDE.md`): the layout rules, the commands, and a
  recipe per task, each starting with `bogie g …`. An agent that runs the
  generator gets the registration right. One that writes it by hand has to
  work it out again.
- **`bin/ci`**: the whole pipeline, run locally. It rebuilds the
  test database, migrates down to zero and back up, runs `sqlc diff`, gofmt, vet,
  golangci-lint, the build and the tests. A green run ends with `gh signoff`.
  There is no hosted runner.
- **Deploy**: a Dockerfile and Kamal 2 config (`config/deploy.yml` for staging,
  plus a production overlay). The image carries the encrypted credentials and
  never the key.
- **Two binaries' worth of commands in one**: `bogie` knows templates and
  layout. The app's own binary (`blog serve | worker | db … | migrate …`)
  knows config and the database, so the commands you run in development are
  the ones the deployed container runs.

## Why Bogie

**Glue, then gaps.** For each concern, Bogie picks one ecosystem tool and
wires it once:

| Concern | Choice |
| --- | --- |
| HTTP | Gin |
| Queries | sqlc + pgx/v5. SQL in `.sql` files, checked against the migrations at generate time |
| Migrations | goose, SQL files embedded in the binary |
| Seeds | plain Go in `db/seeds`, one function per file; `db seed` re-runs every one, so each is written to be safe to run twice |
| Background jobs | River, opt-in with `--jobs`: Postgres-backed, no Redis, enqueue in the same transaction |
| Credentials | [`bogie-go/credentials`](https://github.com/bogie-go/credentials), an encrypted file committed as ciphertext, with a key that is never committed |
| Deploy | Kamal 2 |
| Logging | `log/slog` |

Bogie only builds what has no Go equivalent: `new`, generators that wire,
`destroy`, `doctor`, and the command surface.

### Compared with

| | Bogie | [Andurel](https://github.com/mbvlabs/andurel) | [go-blueprint](https://github.com/Melkeydev/go-blueprint) | [Buffalo](https://github.com/gobuffalo/buffalo) | Hand-rolled |
| --- | --- | --- | --- | --- | --- |
| Shape | scaffolder, then gone | scaffolder | project starter | framework with its own runtime | — |
| Scope | API-only, Postgres-only | full-stack: views, frontend, DI | choose-your-stack starter | full-stack | anything |
| Generators after `new` | yes, and they wire (`g`, `d`, `doctor`) | yes | no, `create` only | yes | — |
| Audience | a team that wants a Go API service with the decisions made | a full-stack Go app | anyone starting a Go project | Go web apps | — |

The short version: **if you want a full-stack framework in Go, with views,
use Andurel**, which does that well. If you want a blank Go project with your
pick of router and database, go-blueprint is quicker. Bogie is for the
narrower case: an API-only service on Postgres, where you would rather start
from decisions already made than make them again.

### What it is not

- **Not a framework.** No `bogie.Application`, no base classes, no DSL.
  Bogie renders `go:embed`ded templates and shells out.
- **Not full-stack.** API-only, no views. Whatever renders (a web app, a mobile app) calls it.
- **Not configurable (yet).** Postgres only, Gin only, one choice per concern.
  SQLite is deliberately out until River's SQLite driver is stable
  ([DESIGN.md §12.4](docs/DESIGN.md)).
- **Not the usual Go layout, on purpose.** Files live by role:
  `app/controllers`, `db/migrate`, underscored package names for namespaces.
  Every file has one obvious home. If that layout bothers you, this tool will
  too.

## Status

**v0.5.x, single maintainer, MIT.** Working today: `bogie new` (with or
without `--jobs`); `g`/`d` for `scaffold`, `model`, `migration`, `controller`,
`service`, `seed`, `job`, `authentication` and `config`; `doctor`; the `db:*` and `credentials:*` tasks; `server` and
`worker` (both live-reload under Air in development), `test`, `lint`, `ci`; `model:sync`; and `app:update`.
Generated apps test with Ginkgo v2 and Gomega: a `test/` tree (`testdb`,
`testapp`, `factories`, `requests`) for what tests share, a suite file per
package, and specs from every generator (v0.4.0 and v0.4.1). v0.4.2 words the generated model specs as one Describe per store method; v0.5.0 moves the layers: `app/models` (types), `db/sqlc` (generated), `db/adapter` (the Store). v0.5.1 drops the Rails wording, has `g model` write the model's own spec, and words the generated not-found tables as sentences. v0.5.2 declares the model spec's variable and BeforeEach at the Describe, so its example Context compiles where it sits. v0.5.3 adds `--only ACTIONS` to `g scaffold` and `g scaffold_controller [NS/]RESOURCE`, the HTTP half of a scaffold for an existing model. v0.5.4 names a namespace package `admin_controllers` (plural, like every other directory) and lets `g scaffold admin/pages` make the model `admin_page`, as Rails does. v0.5.5 makes the generated "is mounted" spec look the route up instead of expecting a non-404, and keeps snake_case model names out of Go identifiers. v0.5.6 names the adapter suite "Adapter", says "model" where generated comments said "domain", camelCases route-group variables, and drops the `exit status 1` that `go run` adds after an app has already said what failed. The next release adds `bogie model:sync [TABLE]`, which follows a migration written by hand: it adds the field for each new column and removes the field for each dropped one, rewrites the queries, the store and the factory from the columns and merges them with your copy, and leaves Validate and everything else of yours alone; and the `:null` attribute modifier, for a column that may be NULL.

Known gaps:

- `bogie app:update` (a three-way merge that moves an app to the current
  templates, using the version recorded in `bogie.toml`) is new. It has been
  exercised across v0.1.0 to v0.2.x (apps generated by v0.1.0 updated to
  v0.2.1 and v0.2.2), but it has had little real-world use.
- `g controller` actions start as 501 stubs. Wiring a dependency into one is by
  hand (the comment above the `bogie:wire` marker shows the line).
- One level of namespace. `api/v1/posts` is refused; a version is a route
  group inside the namespace.

Exact state and machine setup: [`docs/STATUS.md`](docs/STATUS.md).
The design and the reasoning behind each choice: [`docs/DESIGN.md`](docs/DESIGN.md).

No telemetry, now or later.

## Contributing

Issues and design arguments are welcome, especially from people who
have shipped a Go service. Before a PR: `go install . && bin/ci` (about a
minute; it scaffolds two apps, runs every generator and `destroy` against them,
and runs their pipelines). There is no hosted CI, so `bin/ci` is the check of
record. The decisions in `docs/DESIGN.md` are deliberate. If one needs to
change, change the doc first.

## Links

- Website: <https://bogie-go.com>
- Design: [`docs/DESIGN.md`](docs/DESIGN.md) · Status: [`docs/STATUS.md`](docs/STATUS.md)
- Credentials library: [bogie-go/credentials](https://github.com/bogie-go/credentials)

A **bogie** (BOH-gee) is the wheeled frame under a rail car that carries it
along the track. The car body, your application code, is yours.

## License

MIT. See [`LICENSE`](LICENSE).
