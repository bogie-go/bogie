# Status

Where Bogie stands, for whoever picks it up next, on whatever machine. Updated
2026-10-03, on top of commit `1be0831`. `docs/DESIGN.md` is the design and the
reasoning; this file is the state.

## Milestones

| Milestone | State |
| --- | --- |
| M0 decide | done, except: edit the launch essay (see Loose ends). `bogie-go.com` registered 2026-10-03 |
| M1 skeleton: `new`, database, credentials, seeds, `bin/ci` | **done** |
| M2 generators: `g`/`d` for migration, model, controller, service, job; markers; `doctor`; golden and marker-removal tests; `--jobs` with River | **done** |
| M3 agent layer: fuller `AGENTS.md`, `docs/FROM_RAILS.md`, a test that catches an unregistered controller | **done** 2026-10-03: `g scaffold`, `FROM_RAILS.md`, `doctor` as the registration check, and `AGENTS.md` with recipes for a dependency, a query, tests and troubleshooting |
| M4 dogfood: regenerate kchat's skeleton and diff | **done** 2026-10-03; found the Dockerfile and Kamal config missing, added them |
| M5 publish: version from the git tag, first release, launch | version done; `bin/release` ready; needs the essay and the tag. No Homebrew tap, decided 2026-10-03: `go install` is the one install |
| M6 `app:update`: three-way merge against the version in `bogie.toml` | **done** 2026-10-03; tested across two of the day's commits |

## What works today

Install: `cd bogie && go install .` (until there is a tagged release; `@latest`
through the module proxy can lag behind main).

    bogie new NAME [--module=PATH] [--jobs]      an empty service, as rails new makes: it
                                                 builds, migrates, reads its credentials,
                                                 serves /healthz and passes its own bin/ci
    bogie s | server, t | test, lint, ci, worker
    bogie db:prepare db:migrate db:rollback db:migrate:status db:migrate:redo
          db:reset db:seed db:seed:rollback db:create db:drop db:version
    bogie credentials:edit [-e production] | credentials:show | credentials:get
    bogie g model comment body:text post:references   migration, queries, domain type,
                                                      store methods, store test; runs sqlc
    bogie g scaffold post title:string body:text      the model, app/views/posts.go and a
                                                      PostsController with real handlers over
                                                      the store, registered; no service layer
    bogie g migration add_slug_to_posts slug:string:uniq
    bogie g controller comments index show create     one file, a <Name>Controller type with
                                                      its routes and actions, a test, and the
                                                      three registration lines
    bogie g controller admin/reports index show       the same inside a namespace package,
                                                      written and registered on first use
    bogie g service publish_post
    bogie g job send_welcome                          apps made with --jobs
    bogie d <generator> NAME                          reverses any of the above
    bogie doctor                                      markers once each, tools pinned, layout
                                                      files, every controller and namespace
                                                      constructed and mounted
    bogie app:update [--pretend]                      three-way merge to this Bogie's layout,
                                                      base rendered by the recorded version

Every command is spelled as Rails spells it; the app's own binary uses spaces
(`blog db prepare`) and the tool accepts that too. `bogie help` is the
authoritative list.

Each generated app ships: the six rules in `AGENTS.md` with a recipe per
generator, `bin/ci` (drop and rebuild the test database, migrate down to zero
and back, sqlc diff, gofmt, vet, golangci-lint, build, tests, worker boot with
`--jobs`, then `gh signoff`), a development welcome page at `/`, encrypted
credentials with env-only fallback, a Dockerfile and Kamal 2 config (staging
plus a production overlay, `.kamal/secrets` from the credentials), and no
example resource (removed 2026-10-03; the generators are the shape to copy).

## Setting up a new machine

1. Go 1.25 or newer (`mise use go@latest` or the installer).
2. Docker with Compose v2; the generated apps' Postgres runs there on port 5440.
3. golangci-lint v2: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.
   `bin/ci` looks on PATH, then in `$(go env GOPATH)/bin`, and fails loudly if
   it is missing. With mise, `go install` lands in the active Go's bin, which
   is on PATH.
4. `gh` logged in, plus `gh extension install basecamp/gh-signoff`. Without
   it `bin/ci` still runs green and says the run was not recorded.
5. `git clone git@github.com:bogie-go/bogie.git && cd bogie && go install . && bin/ci`.
   Green in about a minute: it scaffolds two apps, one with `--jobs`, runs
   every generator and destroy into them, and runs their pipelines.

There is no hosted CI, by design (Rails 8.1's `bin/ci`). Run `bin/ci` before
pushing; on a clean tree a green run signs off the commit.

## How to work on it

- Templates are `templates/app/**`. `.tmpl` means text/template over
  `scaffold.Vars` (Name, Module, EnvPrefix, LayoutVersion, Jobs); a `dot_`
  path segment becomes a dotfile; `bin/` files are written executable; a
  template that renders to only whitespace produces no file (that is how
  `{{if .Jobs}}` leaves `app/jobs` out). Never leave a bare `.go` file under
  `templates/`: the tool's own build would compile it.
- Every rendered Go file must be gofmt-clean and pass the generated app's
  `.golangci.yml`, in both the plain and the `--jobs` shape. Mind newlines
  around `{{end}}`. Composite literals like `[]T{{...}}` are template actions;
  write them on separate lines.
- Generators are pure in `internal/generate` (clock passed in) and pinned by
  golden files: `go test ./internal/generate -update` rewrites them after an
  intended change; review the diff.
- Markers (`// bogie:controllers`, `bogie:wire`, `bogie:routes`, `bogie:jobs`)
  are edited by `internal/markers`: insert above, idempotent, import added,
  file formatted with `go/format`; every marker checked before any file is
  written. A namespace package carries its own `controllers` and `routes`
  markers, nested one level under the root's.
- A Rails controller is a Go type in one file; a Rails namespace is a Go
  package (`admin_controller/`). Decided 2026-10-03, DESIGN.md §5; the first
  cut had a package per controller, which is not what kchat has.
- `bin/ci` here is the test of record for templates. A failed run leaves the
  scaffolded app under `/tmp/tmp.*` for a look; a green run removes it.
- The commit messages carry the reasoning; `git log` is worth reading.

## Decisions that are easy to undo by accident

- Rails layout and underscored package names on purpose; ST1003 is off and
  nothing else is.
- Test never reads a credentials file. Development reads the shared file,
  staging and production their own.
- Every generated column is NOT NULL with a default, so sqlc emits no pointers.
- A generator runs sqlc at once; the migrations are the schema sqlc compiles
  against.
- Migrate at boot is on by default; `<NAME>_MIGRATE_AT_BOOT=false` turns it off.
- River's schema comes from `rivermigrate` at `migrate`, not from `db/migrate`.
- Postgres is the only database, and SQLite was declined on 2026-10-03 for
  as long as River's SQLite driver is a preview (DESIGN.md §12.4).
- Tasks that destroy pass `--yes` through the tool; the binary in a container
  keeps asking.
- `domain.ErrNotFound` and `ErrInvalid` live in `domain.go`, not with a model.

## Loose ends

- **Launch essay**, drafted, needs the author's edits on four facts (city, the
  company link, "in production", file counts):
  https://claude.ai/code/artifact/7be1e8e6-8b29-4822-9627-134dc1bac8b8
- **Site**: https://bogie-go.com is live (2026-10-03): GitHub Pages from the
  `bogie-go/bogie-go.com` repo, `main` at the root, plain HTML, DNS at
  GoDaddy. Edit `index.html` there and push. In M5 it gets a `go-import`
  meta tag so `bogie-go.com/bogie` can be the vanity import path.
- `bogie-go/credentials` changelog has no 1.4.0 entry; 1.5.0 is the path move.
- Release, for M5: `bin/release v0.1.0` from a clean, pushed `main`. It runs
  `bin/ci` (green signs off), tags, pushes the tag and creates the GitHub
  release. The install is `go install github.com/bogie-go/bogie@v0.1.0`, and
  only that: a tool for people about to write Go can ask for Go, so there is
  no Homebrew tap and nothing prebuilt (decided 2026-10-03). Until the tag,
  `@latest` installs the newest commit and reports it.
- `bogie version` reads the build: the release flag, or the module version
  Go stamped, which is the tag for `go install @vX.Y.Z` and
  `v0.0.0-<date>-<commit>` (`+dirty` if uncommitted) for a build from a
  clone; the same string goes into every generated `bogie.toml` (done
  2026-10-03). The first tag and the launch essay remain
  for M5.
- kchat still imports `roonglit/credentials`; it can move whenever convenient.
- `g controller` actions are 501 stubs with no dependencies; wiring a
  dependency into one is by hand (the comments above the `bogie:wire` marker
  show the line, and a scaffolded controller shows the shape).
- Nested namespaces (`api/v1/posts`) are refused; v1 supports one level, and a
  version is a route group inside the namespace's `routes.go`.
