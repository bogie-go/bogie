# Status

Where Bogie stands, for whoever picks it up next, on whatever machine. Updated
2026-11-14, at v1.0.0 (see "v1.0.0" below). `docs/DESIGN.md` is the design and the reasoning; this file is the
state.

## Milestones

| Milestone | State |
| --- | --- |
| M0 decide | done, except: edit the launch essay (see Loose ends). `bogie-go.com` registered 2026-10-03 |
| M1 skeleton: `new`, database, credentials, seeds, `bin/ci` | **done** |
| M2 generators: `g`/`d` for migration, model, controller, service, job; markers; `doctor`; golden and marker-removal tests; `--jobs` with River | **done**; `g`/`d seed` added 2026-10-16, replacing versioned goose seeds with plain Go (DESIGN.md §4.1) |
| M3 agent layer: fuller `AGENTS.md`, a test that catches an unregistered controller | **done** 2026-10-03: `g scaffold`, `doctor` as the registration check, and `AGENTS.md` with recipes for a dependency, a query, tests and troubleshooting |
| M4 dogfood: regenerate kchat's skeleton and diff | **done** 2026-10-03; found the Dockerfile and Kamal config missing, added them |
| M5 publish: version from the git tag, first release, launch | **v0.1.0 tagged 2026-10-03**. No Homebrew tap, decided the same day: `go install` is the one install. The launch essay remains |
| M6 `app:update`: three-way merge against the version in `bogie.toml` | **done** 2026-10-03; tested across two of the day's commits |
| `model:sync` and `:null`: follow a hand-written migration into the model, queries, store and factory | **done** 2026-10-29, v0.6.0 (2026-10-30), fixed in v0.6.1; DESIGN §5b |
| Authorization, status and schedules: `g policy`, `g scaffold --policy`, the token `role` claim and `MintFor`, `db:status`, `g job --every` | **done**, v0.7.0 (2026-11-13) |
| v1.0.0: the first stable release and the 1.x compatibility promise | **v1.0.0 tagged 2026-11-14**: #41, #42 and #43 merged 2026-11-13; release notes in `docs/release-notes/v1.0.0.md` |

## What works today

Install: `go install github.com/bogie-go/bogie@latest` for the newest tag,
`@main` for main, `go install .` from a clone. First release: v0.1.0,
2026-10-03; latest: v1.0.0, 2026-11-14.

    bogie new NAME [--module=PATH] [--id=bigint|uuid] [--jobs]
                                                 an empty service: it
                                                 builds, migrates, reads its credentials,
                                                 serves /healthz and passes its own bin/ci
    bogie s | server, t | test, lint, ci, worker
                                                 server and worker rebuild and restart
                                                 on save in development (Air, .air.toml);
                                                 production runs the compiled binary
    bogie db:prepare db:migrate db:rollback db:migrate:status db:migrate:redo
          db:reset db:seed db:create db:drop db:version
    bogie db:status [-h]                         read-only, in the tool itself: host,
                                                 port, database, Postgres version, the
                                                 latest migration and how many are
                                                 pending, the table count; a failed
                                                 connection keeps the driver's error
                                                 and adds a one-line hint
    bogie credentials:edit [-e production] | credentials:show | credentials:get
    bogie g model comment body:text post:references   migration, queries, domain type,
                                                      store methods, store test; runs sqlc
    bogie g scaffold post title:string body:text      the model, app/views/posts.go and a
                                                      PostsController with real handlers over
                                                      the store, registered; no service layer
                                                      --only index,show writes just those;
                                                      admin/posts is the model admin_post
                                                      with its controller in the namespace
    bogie g scaffold_controller admin/categories      the HTTP half over an existing model,
                                                      mounted in a namespace; attributes read
                                                      from app/models; --only too
    bogie g migration add_slug_to_posts slug:string:uniq
    bogie g controller comments index show create     one file, a <Name>Controller type with
                                                      its routes and actions, a test, and the
                                                      three registration lines
    bogie g controller admin/reports index show       the same inside a namespace package,
                                                      written and registered on first use
    bogie g service publish_post
    bogie g job send_welcome                          on an app with no jobs,
                                                      adds River first (as if
                                                      --jobs had been passed
                                                      to new), merged in the
                                                      same way as app:update
    bogie g job daily_digest --every "0 9 * * *"      the same, and a River
                                                      periodic job on that cron
                                                      schedule (RunOnStart off);
                                                      bogie d job removes it
    bogie g policy post                           app/policies/post_policy.go: CanIndex,
                                                      CanShow, CanCreate, CanUpdate, CanDestroy
                                                      over an actor and *models.Post, a table
                                                      spec, and a service that checks the policy.
                                                      Defaults deny except what the actor owns
    bogie g scaffold post title:string --policy      the same, wired: the controller calls the
                                                      service and maps a denial to 403. Needs
                                                      `g authentication token` first; adds user_id
    bogie g authentication secret|token|api_key       a middleware on a route group, its config
                                                      keys wired and a development secret written
                                                      to the credentials; api_key adds a table
                                                      and `bogie api_keys:create NAME`
    bogie g config smtp address:req port:int=1025 ... a group of the credentials file: a typed
                                                      struct in config/smtp.go, registered in
                                                      config.go, with development credentials;
                                                      run again to add keys; hello=world makes a
                                                      flat top-level key. Top-level entries are kept
                                                      alphabetical, a group's own keys as typed.
                                                      smtp.address is SMTP_ADDRESS (no app prefix)
    bogie g seed demo_posts                           db/seeds/demo_posts.go, registered above
                                                      // bogie:seeds; db seed re-runs every seed,
                                                      so each one is written to run twice safely
    bogie d <generator> NAME                          reverses any of the above, including
                                                      `d policy` (a policied scaffold's policy
                                                      goes with `d scaffold`)
    bogie doctor [-h]                                 markers once each, tools pinned, layout
                                                      files, every controller and namespace
                                                      constructed and mounted
    bogie app:update [--pretend]                      three-way merge to this Bogie's layout,
                                                      base rendered by the recorded version

Commands that name a task use a colon (`db:migrate`); the app's own binary uses spaces
(`blog db prepare`) and the tool accepts that too. `bogie help` is the
authoritative list.

Each generated app ships: the six rules in `AGENTS.md` with a recipe per
generator, `bin/ci` (drop and rebuild the test database, migrate down to zero
and back, sqlc diff, gofmt, vet, golangci-lint, build, tests, worker boot with
`--jobs`, then `gh signoff`), a development welcome page at `/`, a
go:embedded `public/404.html`/`500.html` for every other no-route request
and for a panic (added 2026-10-17, DESIGN.md §5), encrypted credentials with
env-only fallback, a Dockerfile and Kamal 2 config (staging plus a
production overlay, `.kamal/secrets` from the credentials), and no example
resource (removed 2026-10-03; the generators are the shape to copy).

## v1.0.0

Tagged 2026-11-14, the first stable release, with a GitHub release whose
body is `docs/release-notes/v1.0.0.md`. What it adds since v0.7.0, all
merged on 2026-11-13:

- #41: `db:status -h` / `--help` / `help` print usage through `WantsHelp`,
  the same way `doctor` does.
- #42: the generated `AGENTS.md` says `model:sync [TABLE]`, not `<model>`.
- #43: this section and `docs/release-notes/v1.0.0.md`.

The release notes hold the 1.x compatibility promise (`MintFor`,
`middlewares.Role`, the three-value `jobs.Workers`, `policies.Actor`,
`policies.ErrDenied`, `g job --every` and its restart-skip behaviour). They
also have the manual upgrade for an app whose token authentication predates
v0.7.0, which `app:update` does not move onto `MintFor`, `Claims.Role` or
`middlewares.Role`. Every 1.x release keeps that promise; a change that
breaks it waits for 2.0. The bogie-go.com version-bump PR merges after the
tag exists.

## Setting up a new machine

1. Go 1.26 or newer (`mise use go@latest` or the installer). The tool itself
   builds with 1.25, but a generated app needs 1.26: sqlc v1.31 requires it,
   and with an older Go on PATH `go test -cover` (so `bogie test` and
   `bin/ci`) fails under the auto-downloaded toolchain.
2. Docker with Compose v2; the generated apps' Postgres runs there on port 5440.
3. golangci-lint v2: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.
   `bin/ci` looks on PATH, then in `$(go env GOPATH)/bin`, and fails loudly if
   it is missing. With mise, `go install` lands in the active Go's bin, which
   is on PATH.
4. `gh` logged in, plus `gh extension install basecamp/gh-signoff`. Without
   it `bin/ci` still runs green and says the run was not recorded.
5. `git clone git@github.com:bogie-go/bogie.git && cd bogie && go install . && bin/ci`.
   Green in a few minutes: it scaffolds two apps, one with `--jobs`, runs
   every generator and destroy into them, and runs their pipelines.

There is no hosted CI, by design. Run `bin/ci` before
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
- Markers (`// bogie:controllers`, `bogie:wire`, `bogie:routes`, `bogie:jobs`,
  `bogie:config`, `bogie:env`, `bogie:validate`, `bogie:commands`)
  are edited by `internal/markers`: insert above, idempotent, import added,
  file formatted with `go/format`; every marker checked before any file is
  written. A namespace package carries its own `controllers` and `routes`
  markers, nested one level under the root's.
- A controller is a Go type in one file; a namespace is a Go
  package (`admin_controllers/`). Decided 2026-10-03, DESIGN.md §5; the first
  cut had a package per controller, which is not what kchat has.
- `bin/ci` here is the test of record for templates. A failed run leaves the
  scaffolded app under `/tmp/tmp.*` for a look; a green run removes it.
- The commit messages carry the reasoning; `git log` is worth reading.

## Decisions that are easy to undo by accident

- The layout by role and underscored package names on purpose; ST1003 is off and
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
- `models.ErrNotFound` and `ErrInvalid` live in `models.go`, not with a model.
- `g model` writes a spec for the model too (`app/models/<name>_test.go`): a plain call to `Validate` on the factory's defaults, plus where the next rule's Context goes. The app ships `app/models/models_suite_test.go` so it runs. `destroy` removes it with the model.
- **Layout, v0.5.0 (2026-10-27).** The types and their `Validate` are
  `app/models` (they were `app/domain`). sqlc writes only `db/sqlc`
  (`package sqlc`). The Store, `withTx` and the row <-> models conversion are
  `db/adapter` (`package adapter`; they were `app/models`). `withTx` hands its
  closure the `pgx.Tx` as well as the bound `*sqlc.Queries`. `bogie app:update`
  cannot make this move for an existing app, because it renames two packages:
  do it by hand (point `db/sqlc.yaml` at `package: "sqlc"`, `out: "sqlc"`,
  regenerate, move the hand-written files to `db/adapter`, then rename the
  imports) or keep the old layout.
- A hand-written store method goes in a sibling file (`db/adapter/<table>_extra.go`),
  not inside the generated `<table>.go`: that file is regenerated whole on
  the next `g model`, and a hand-written addition inside it is reported
  `conflict` instead of kept.
- The `NoRoute` chain (`staticFile`, `welcome`, `notFound` in
  `app/controllers`) relies on `c.Abort()`, not a plain `return`, to stop at
  the handler that answered: Gin's own driving loop calls the next handler
  regardless unless a prior one aborts. Dropping an `Abort()` call while
  "simplifying" one of these three reopens the bug DESIGN.md §5 describes —
  a later handler's content silently appended underneath an earlier one's.

## Loose ends

- **Launch essay**, drafted, needs the author's edits on four facts (city, the
  company link, "in production", file counts):
  https://claude.ai/code/artifact/7be1e8e6-8b29-4822-9627-134dc1bac8b8
- **Site**: https://bogie-go.com is live (2026-10-03): GitHub Pages from the
  `bogie-go/bogie-go.com` repo, `main` at the root, plain HTML, DNS at
  GoDaddy. Edit `index.html` there and push. In M5 it gets a `go-import`
  meta tag so `bogie-go.com/bogie` can be the vanity import path.
- **Issue #11**, the rest of it: `bin/setup`, and a documented
  native-Postgres path next to the Compose one in both READMEs. Live reload
  landed 2026-10-06 (DESIGN.md §12.12), and so did the actionable hint: a
  failure to reach Postgres now says which of the three things went wrong
  (nothing listening, credentials refused, database not created) and what to
  run, keeping the original error. `app/services/database.Hint`, applied in
  `NewApplication` and in the `db`/`migrate` subcommands.
- **`bin/dev`**: built with hivemind on a `Procfile.dev`, then withdrawn
  before release because Ctrl-C orphans a process roughly one time in eight
  and it then holds the port. DESIGN.md §12.13 has the failure rates, why
  `bogie server` is unaffected, the `exec` dead end, and the one idea worth
  trying next. Worth another pass with `bin/setup`.
- `bin/ci` on macOS: the live-reload step uses GNU `sed -i` and `setsid`.
  BSD sed and a missing `setsid` fail that step, so put GNU sed and a
  `setsid` on PATH before running it on a Mac.
- `bogie-go/credentials` changelog has no 1.4.0 entry; 1.5.0 is the path move.
- Releases are cut by hand from a clean, pushed `main` with a green,
  signed-off `bin/ci`: an annotated tag, pushed, then `gh release create`.
  The `bin/release` script that earlier notes here describe is not in the
  repository. The install is `go install github.com/bogie-go/bogie@vX.Y.Z`,
  and only that: a tool for people about to write Go can ask for Go, so there is
  no Homebrew tap and nothing prebuilt (decided 2026-10-03). Until the tag,
  `@latest` installs the newest commit and reports it.
- `bogie version` reads the build: the release flag, or the module version
  Go stamped, which is the tag for `go install @vX.Y.Z` and
  `v0.0.0-<date>-<commit>` (`+dirty` if uncommitted) for a build from a
  clone; the same string goes into every generated `bogie.toml` (done
  2026-10-03). The launch essay remains for M5.
- kchat still imports `roonglit/credentials`; it can move whenever convenient.
- `g controller` actions are 501 stubs with no dependencies; wiring a
  dependency into one is by hand (the comments above the `bogie:wire` marker
  show the line, and a scaffolded controller shows the shape).
- Nested namespaces (`api/v1/posts`) are refused; v1 supports one level, and a
  version is a route group inside the namespace's `routes.go`.
