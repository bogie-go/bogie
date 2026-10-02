# Status

Where Bogie stands, for whoever picks it up next, on whatever machine. Updated
2026-10-02, at commit `75185ba`. `docs/DESIGN.md` is the design and the
reasoning; this file is the state.

## Milestones

| Milestone | State |
| --- | --- |
| M0 decide | done, except: register `bogie-go.com`; edit the launch essay (see Loose ends) |
| M1 skeleton: `new`, database, credentials, seeds, `bin/ci` | **done** |
| M2 generators: `g`/`d` for migration, model, controller, service, job; markers; `doctor`; golden and marker-removal tests; `--jobs` with River | **done** |
| M3 agent layer: fuller `AGENTS.md`, `docs/FROM_RAILS.md`, a test that catches an unregistered controller | next |
| M4 dogfood: regenerate kchat's skeleton and diff | |
| M5 publish: version from the git tag, Homebrew tap, first release, launch | |
| M6 `app:update`: three-way merge against the version in `bogie.toml` | after the first tag |

## What works today

Install: `cd bogie && go install .` (until there is a tagged release; `@latest`
through the module proxy can lag behind main).

    bogie new NAME [--module=PATH] [--jobs]      a service that builds, migrates, seeds, reads
                                                 its credentials, serves /healthz and /posts,
                                                 and passes its own bin/ci
    bogie s | server, t | test, lint, ci, worker
    bogie db:prepare db:migrate db:rollback db:migrate:status db:migrate:redo
          db:reset db:seed db:seed:rollback db:create db:drop db:version
    bogie credentials:edit [-e production] | credentials:show | credentials:get
    bogie g model comment body:text post:references   migration, queries, domain type,
                                                      store methods, store test; runs sqlc
    bogie g migration add_slug_to_posts slug:string:uniq
    bogie g controller comments index show create     package, one file per action, a test,
                                                      and the three registration lines
    bogie g service publish_post
    bogie g job send_welcome                          apps made with --jobs
    bogie d <generator> NAME                          reverses any of the above
    bogie doctor                                      markers once each, tools pinned, layout
                                                      files, every controller constructed
                                                      and mounted

Every command is spelled as Rails spells it; the app's own binary uses spaces
(`blog db prepare`) and the tool accepts that too. `bogie help` is the
authoritative list.

Each generated app ships: the six rules in `AGENTS.md` with a recipe per
generator, `bin/ci` (drop and rebuild the test database, migrate down to zero
and back, sqlc diff, gofmt, vet, golangci-lint, build, tests, worker boot with
`--jobs`, then `gh signoff`), a development welcome page at `/`, encrypted
credentials with env-only fallback, and the `posts` example resource end to
end, which `destroy` can remove entirely.

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
  written.
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
- Tasks that destroy pass `--yes` through the tool; the binary in a container
  keeps asking.
- `domain.ErrNotFound` and `ErrInvalid` live in `domain.go`, not with a model.

## Loose ends

- **Launch essay**, drafted, needs the author's edits on four facts (city, the
  company link, "in production", file counts):
  https://claude.ai/code/artifact/7be1e8e6-8b29-4822-9627-134dc1bac8b8
- **Domain `bogie-go.com`**: confirmed available on 2026-09-29, not registered.
- `bogie-go/credentials` changelog has no 1.4.0 entry; 1.5.0 is the path move.
- `bogie version` prints `dev`; M5 wires it to the git tag and `bogie.toml`.
- kchat still imports `roonglit/credentials`; it can move whenever convenient.
- `g controller` actions are 501 stubs with no dependencies; wiring a service
  into one is by hand (the `posts` example shows the shape). A `g scaffold`
  that does model, service and controller together is worth considering in M3.
