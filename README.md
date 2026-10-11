<p align="center"><img src="docs/bogie.png" width="260" alt="Bogie's mascot: a gopher and a ruby riding a mine cart on rails"></p>

<h1 align="center">Bogie</h1>

<p align="center"><strong>The Go service next to your Rails app.</strong><br>
A <code>rails new</code>-style CLI that scaffolds an API-only, Postgres-only Go service
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

Every Rails shop eventually needs one service that Rails is the wrong tool
for: a webhook receiver that has to answer in under a second while a platform
retries at it, a socket gateway holding thousands of idle connections, a
worker draining a queue against a rate-limited API. The web app stays in
Rails. That one thing goes to Go. The first attempt usually goes badly, not
because of Go, but because everything Rails decided for you is now yours to
decide.

**Why Bogie exists.** We needed parts of our system to handle what Rails
doesn't do well, like heavy concurrency and high load, so we moved those
parts to Go. But Go gives you so much freedom and so many choices that
building the app itself was the hard part. Bogie brings Rails's convention
over configuration to Go, so the Go side starts from decisions already made.

Bogie makes those decisions the way Rails would, out of tools the Go
ecosystem already ships, and builds only what has no equivalent: the
`rails new`-style generator, and `rails g`-style generators that also
**register** what they write, because Go has no autoload.

The generated app **never imports Bogie**. Delete the tool tomorrow and the
app does not notice.

<p align="center"><img src="docs/welcome.webp" width="640" alt="The welcome page a new app serves at / in development"></p>

## Install

```sh
go install github.com/bogie-go/bogie@latest
```

You need **Docker with Compose v2** and **Go 1.26+**. The tool itself builds
with Go 1.25, but the app it generates needs 1.26, because the pinned sqlc
requires it. The generated app runs its Postgres in Compose on port 5440, so it
stays clear of the Rails app's 5432.
sqlc and goose are pinned in the generated app's `go.mod` under `tool`, so
there is nothing else to install globally.

Required for `bin/ci`: [golangci-lint v2](https://golangci-lint.run/).
Optional: `gh extension install basecamp/gh-signoff`, so a green run signs off
the commit.

`@latest` is the newest tagged release, and `bogie version` prints it. For
`main` as it stands, `go install github.com/bogie-go/bogie@main`; for a
clone, `go install .`.

## Quickstart

```sh
bogie new blog                                  # an empty API service, like rails new
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

`bogie g scaffold` prints what it did, the way Rails does:

```
      create  db/migrate/20261003054047_create_posts.sql
      create  db/queries/posts.sql
      create  app/domain/post.go
      create  app/models/posts.go
      create  app/models/posts_test.go
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

Rails spelling, Rails layout, Go underneath:

| Rails | Bogie |
| --- | --- |
| `rails new blog` | `bogie new blog` (`--jobs` adds River) |
| `rails g model post title:string` | `bogie g model post title:string`: migration, queries, domain type, store, then sqlc |
| `rails g scaffold post …` | `bogie g scaffold post …`: model + JSON views + a wired controller |
| `rails g controller posts index show` | `bogie g controller posts index show`, registered as it is written |
| `namespace :admin` | `bogie g controller admin/reports`, a package per namespace |
| `rails g job send_welcome` | `bogie g job send_welcome`, a River worker on the same Postgres |
| `rails g authentication` | `bogie g authentication secret\|token\|api_key`: a middleware for a route group (shared secret, JWT bearer token, or hashed API key), its config and credentials entry, and a `WithSecret` / `WithUser` / `WithAPIKey` helper; you mount the routes it guards |
| `rails db:migrate`, `db:rollback`, `db:seed` | the same words, run by the app's own binary |
| `rails credentials:edit` | `bogie credentials:edit`: encrypted file, key never committed |
| `rails s`, `rails t`, `bin/ci` | `bogie s`, `bogie t`, `bin/ci` |

The full dictionary, including what has no equivalent (no console, no
`schema.rb`, no autoload), is in [`docs/FROM_RAILS.md`](docs/FROM_RAILS.md).

Every new app also ships:

- **`AGENTS.md`** (and `CLAUDE.md`): the layout rules, the commands, and a
  recipe per task, each starting with `bogie g …`. An agent that runs the
  generator gets the registration right. One that writes it by hand has to
  work it out again.
- **`bin/ci`**: the whole pipeline, run locally as in Rails 8.1. It rebuilds the
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

**Glue, then gaps.** For each thing Rails gives you, Bogie picks one
ecosystem tool and wires it once:

| Concern | Choice |
| --- | --- |
| HTTP | Gin |
| Queries | sqlc + pgx/v5. SQL in `.sql` files, checked against the migrations at generate time |
| Migrations, seeds | goose, SQL files embedded in the binary |
| Background jobs | River, opt-in with `--jobs`: Postgres-backed, no Redis, enqueue in the same transaction |
| Credentials | [`bogie-go/credentials`](https://github.com/bogie-go/credentials), the Rails `credentials.yml.enc` pattern |
| Deploy | Kamal 2 |
| Logging | `log/slog` |

Bogie only builds what has no Go equivalent: `new`, generators that wire,
`destroy`, `doctor`, and the Rails-spelled command surface.

### Compared with

Be honest with yourself first: if the job fits in Rails (Sidekiq/Solid Queue,
Action Cable), keep it in Rails. Bogie is for when it doesn't.

| | Bogie | [Andurel](https://github.com/mbvlabs/andurel) | [go-blueprint](https://github.com/Melkeydev/go-blueprint) | [Buffalo](https://github.com/gobuffalo/buffalo) | Hand-rolled |
| --- | --- | --- | --- | --- | --- |
| Shape | scaffolder, then gone | scaffolder | project starter | framework with its own runtime | — |
| Scope | API-only, Postgres-only | full-stack: views, frontend, DI | choose-your-stack starter | full-stack | anything |
| Generators after `new` | yes, and they wire (`g`, `d`, `doctor`) | yes | no, `create` only | yes | — |
| Audience | a Rails shop adding one Go service | a Rails-like Go app on its own | anyone starting a Go project | Go web apps | — |

The short version: **if you want a Rails-like framework in Go, with views,
use Andurel**, which does that well. If you want a blank Go project with your
pick of router and database, go-blueprint is quicker. Bogie is for the
narrower case: a Rails team that already has the Rails app and wants the one
Go service beside it to look and behave like something they already know.

### What it is not

- **Not a framework.** No `bogie.Application`, no base classes, no DSL.
  Bogie renders `go:embed`ded templates and shells out.
- **Not full-stack.** API-only, no views. The Rails app renders.
- **Not configurable (yet).** Postgres only, Gin only, one choice per concern.
  SQLite is deliberately out until River's SQLite driver is stable
  ([DESIGN.md §12.4](docs/DESIGN.md)).
- **Not idiomatic Go layout, on purpose.** `app/controllers`, `db/migrate`,
  underscored package names. Moving between the two repos costs nothing. If
  that layout bothers you, this tool will too.

## Status

**v0.1.0, single maintainer, MIT.** Working today: `bogie new` (with or
without `--jobs`); `g`/`d` for `scaffold`, `model`, `migration`, `controller`,
`service`, `job` and `authentication`; `doctor`; the `db:*` and `credentials:*` tasks; `server`,
`worker`, `test`, `lint`, `ci`; and `app:update`.

Known gaps:

- `bogie app:update` (a three-way merge that moves an app to the current
  templates, using the version recorded in `bogie.toml`) is new. v0.1.0 is the
  first tag, so no app has yet been upgraded from one tagged release to the next.
- `g controller` actions start as 501 stubs. Wiring a dependency into one is by
  hand (the comment above the `bogie:wire` marker shows the line).
- One level of namespace. `api/v1/posts` is refused; a version is a route
  group inside the namespace.

Exact state and machine setup: [`docs/STATUS.md`](docs/STATUS.md).
The design and the reasoning behind each choice: [`docs/DESIGN.md`](docs/DESIGN.md).

No telemetry, now or later.

## Contributing

Issues and design arguments are welcome, especially from Rails developers who
have shipped a Go service. Before a PR: `go install . && bin/ci` (about a
minute; it scaffolds two apps, runs every generator and `destroy` against them,
and runs their pipelines). There is no hosted CI, so `bin/ci` is the check of
record. The decisions in `docs/DESIGN.md` are deliberate. If one needs to
change, change the doc first.

## Links

- Website: <https://bogie-go.com>
- Rails → Bogie dictionary: [`docs/FROM_RAILS.md`](docs/FROM_RAILS.md)
- Design: [`docs/DESIGN.md`](docs/DESIGN.md) · Status: [`docs/STATUS.md`](docs/STATUS.md)
- Credentials library: [bogie-go/credentials](https://github.com/bogie-go/credentials)

A **bogie** (BOH-gee) is the wheeled frame under a rail car that carries it
along the track. The car body, your application code, is yours.

## License

MIT. See [`LICENSE`](LICENSE).
