# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Bogie is "the Go service next to your Rails app": a CLI (`bogie`) that scaffolds
an API-only, Postgres-only Go service out of existing ecosystem tools (Gin,
sqlc + pgx/v5, goose, River, Kamal 2) and builds only what has no Go
equivalent: a `rails new`-style project generator and generators that also
*wire* new code in. The generated app never imports bogie. It is deliberately
NOT a Rails-like Go framework; Andurel holds that slot.

**Status: design phase. There is no Go code, `go.mod`, Makefile, or test suite
yet.** `docs/DESIGN.md` is the source of truth; read it before proposing or
writing any code. The README summarises it. This file will need a Commands
section once the module exists.

## Decisions already made (do not re-litigate; change `docs/DESIGN.md` first if they must change)

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
  `g`, `d`, `doctor`, `upgrade`). The generated app's own binary knows config
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
- **Postgres only** in v1. Attribute type mapping (`string`→`text`,
  `datetime`→`timestamptz`, `references`→`uuid` + FK, etc.) is in §5.
- **API-only, no views.** Decided 2026-09-30.
- **Jobs are River, opt-in via `--jobs`.** Not asynq: no Redis, transactional
  enqueue, typed args. Reasoning in §12.2. Seeds and migrations both go through
  the app binary (`db seed`, `db migrate`), never the goose CLI directly.
- **Agent-first.** Generated apps ship an `AGENTS.md` (with `CLAUDE.md` as
  symlink/include) containing the six layout rules, commands, and per-task
  recipes that each start with `bogie g …`. Template comments should say what a
  line is *for*.
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

Whether migrate-at-boot stays the default (§12.11). Views, jobs, credentials
and the domain package name (`app/domain/`, fixed) are decided.

## Milestones

M0 decide → M1 `bogie new` producing an app that builds, migrates and serves →
M2 generators + markers + doctor + golden tests → M3 agent layer → M4 dogfood
against kchat → M5 publish. Current position: M0.
