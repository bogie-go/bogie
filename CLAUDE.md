# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Bogie is "the Go service next to your Rails app": a CLI (`bogie`) that scaffolds
an API-only, Postgres-only Go service out of existing ecosystem tools (Gin,
sqlc + pgx/v5, goose, River, Kamal 2) and builds only what has no Go
equivalent: a `rails new`-style project generator and generators that also
*wire* new code in. The generated app never imports bogie. It is deliberately
NOT a Rails-like Go framework; Andurel holds that slot.

`docs/DESIGN.md` is the source of truth; read it before changing the
architecture. `docs/STATUS.md` is where things stand, how to set up a machine,
and what is next; read it first in a new session. The README summarises both.

## Commands

    bin/ci              every check, locally: gofmt, vet, golangci-lint, the
                        tool's tests, then `bogie new` into a temp dir, the
                        generated app's own bin/ci, and a /healthz check.
                        Green on a clean tree ends with `gh signoff`. There
                        is no hosted CI; run this before pushing.
    make test           go test -race -cover ./...   (the tool only)
    make scaffold       generate an app in a temp dir and build/vet/test it
    go run . new NAME   try the generator; add --pretend to see the plan
    bin/ci              also generates a model, migration, controller and
                        service into the fresh app, runs its pipeline,
                        destroys them, and runs it again
    go test ./internal/generate -update   rewrite the generators' golden files
                        after an intended output change; review the diff

Templates live in `templates/app`. A `.tmpl` suffix means text/template with
`scaffold.Vars`; a `dot_` path segment becomes a dotfile; files under `bin/`
are written executable. The rendered Go must be gofmt-clean and pass the
generated app's `.golangci.yml`, which bin/ci enforces.

## Decisions already made (do not re-litigate; change `docs/DESIGN.md` first if they must change)

- **As close to Rails as possible, for the Rails developer's convenience.**
  Every command is spelled exactly as Rails spells it, colons included:
  `db:migrate`, `db:rollback`, `credentials:edit`, `app:update`; `server`,
  `generate`, `new` with spaces, because that is Rails's own mix. Vocabulary,
  layout and file names follow Rails wherever Rails has a word for the thing.
- **Glue, then gaps.** For every Rails concept, use the ecosystem tool listed in
  DESIGN.md §2 and wire it once. Only build what is missing. New features must
  pass: is there an ecosystem tool? If yes, wire it; if no, is the gap big enough
  to own?
- **Not a runtime framework.** No `bogie.Application`, no base classes, no DSL.
  Bogie renders `go:embed`ded `text/template` templates and shells out.
- **Rails layout on purpose.** Generated apps use `app/controllers`,
  `app/services`, `app/models`, `db/migrate` etc. Do not "fix" this into
  idiomatic Go layout. No `spec/` directory: Go tests sit beside the code. The
  reference implementation is kchat at `/home/mac/developments/klangtech/kchat`,
  pinned to commit 485b80f; extraction order and the findings it fixes are in
  DESIGN.md §9. kchat is reference code, never edited from this repo.
- **Two CLIs.** The installed `bogie` tool knows templates and layout (`new`,
  `g`, `d`, `doctor`, `app:update`). The generated app's own binary knows config
  and the database (`serve`, `worker`, `db …`, `migrate …`) because it must also
  run inside the deployed container. `bogie db migrate` is a thin proxy for
  `go run . db migrate`; development runs the code production runs.
- **Wiring via marker comments (v1).** Generators insert lines above
  `// bogie:controllers` and `// bogie:routes` markers, idempotently. The root
  `Server` uses named fields, never positional constructor args. A missing marker
  is a named error with no partial write. `bogie doctor` checks every marker
  exists exactly once. AST editing and a generated registry file were considered
  and rejected for v1 (§5).
- **Generator output follows Rails:** `create` / `insert` / `skip` / `conflict`
  per file, `--pretend` for dry runs, never overwrite without `--force`.
- **Postgres only** in v1. SQLite was considered and declined on 2026-10-03
  (§12.4): River's SQLite driver is a preview, so `--jobs` would break, and
  the database is wired in nine places. Attribute type mapping (`string`→`text`,
  `datetime`→`timestamptz`, `references`→`uuid` + FK, etc.) is in §5.
- **API-only, no views.** Decided 2026-09-30.
- **Jobs are River, opt-in via `--jobs`.** Not asynq: no Redis, transactional
  enqueue, typed args. Reasoning in §12.2. River's schema is applied by the
  app's `migrate` through rivermigrate, not copied into db/migrate. Seeds and
  migrations both go through the app binary (`db seed`, `db migrate`), never
  the goose CLI directly.
- **Templates are conditional with `{{if .Jobs}}`.** A template that renders
  to only whitespace produces no file. Mind the newlines around `{{end}}`:
  the rendered Go must stay gofmt-clean, and bin/ci checks both the plain and
  the --jobs app.
- **Agent-first.** Generated apps ship an `AGENTS.md` (with `CLAUDE.md` as
  symlink/include) containing the six layout rules, commands, and per-task
  recipes that each start with `bogie g …`. Template comments should say what a
  line is *for*.
- **Live reload is Air, development only.** `bogie server` and `bogie worker`
  run the app under Air (pinned as a `tool` dep, settings in the app's
  `.air.toml`); `make server` and `make worker` never reload, and nothing
  reloads outside development. Deleting `.air.toml` turns it off — §12.12.
- **`bin/dev` is hivemind on a `Procfile.dev`, `--jobs` only.** Both files are
  `{{if .Jobs}}`: without a worker there is one process and `bogie server` is
  the loop. Foreman stays declined (Ruby); overmind is documented as a
  drop-in on the same Procfile.dev rather than used, because it needs tmux
  and that cannot come from `go get -tool` — §12.13.
- **No telemetry.** MIT, single maintainer.

## The six rules the generated app enforces

These come from DESIGN.md §3 and are baked into templates and `.golangci.yml`:

1. `ctx context.Context` first for anything doing I/O.
2. Interfaces are declared by the consumer.
3. `app/domain` imports nothing third-party.
4. Constructors return `(T, error)`; only `main` exits.
5. `*gin.Context` never travels below `app/controllers`.
6. No `util/`, `helpers/`, `common/`.

## Testing strategy for the tool (DESIGN.md §8)

When implementation starts, tests are expected to take three forms:
golden-file tests per generator, a CI scaffold job that runs `bogie new` then
builds/vets/lints/tests the result against a real Postgres, and marker-removal
tests that assert a named error and no partial write.

## Open questions (DESIGN.md §12)

Whether migrate-at-boot stays the default (§12.11). Views, jobs, credentials,
live reload (§12.12), `bin/dev` (§12.13) and the domain package name
(`app/domain/`, fixed) are decided. `bin/setup` and an actionable hint when
Postgres is unreachable are the open half of issue #11.

## Milestones

M0 decide → M1 `bogie new` producing an app that builds, migrates and serves →
M2 generators + markers + doctor + golden tests → M3 agent layer → M4 dogfood
against kchat → M5 publish → M6 `app:update`. All built; v0.1.0 tagged
2026-10-03. `docs/STATUS.md` has what is next.
