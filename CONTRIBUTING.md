# Contributing to Bogie

Thanks for looking. Read this first, because a few things here are unusual:
there is **no hosted CI**, the layout is **Rails-shaped on purpose**, and the
project has **one maintainer** and is at **v0.1.0**.

## Before you write code

- **Bugs:** open an issue with `bogie version`, `go version`, the command
  you ran, and what it printed. If the problem is in a generated app, include
  that app's `bogie.toml` and the output of `bogie doctor`.
- **Features and design changes:** open an issue first. Bogie runs on a
  written design, [`docs/DESIGN.md`](docs/DESIGN.md). The decisions in it
  (Rails spelling, Rails layout, API-only, Postgres-only, River for jobs,
  marker comments for wiring, no runtime framework, no telemetry) are
  deliberate, and each one has its reasoning next to it. They can change, but
  the doc changes first, in its own discussion, before any code does.
- **The test for any new feature is "glue, then gaps"**: is there an ecosystem
  tool for this? If yes, wire it. If no, is the gap big enough for Bogie to
  own? A PR that adds a dependency or a new concept should answer that in its
  description.

Small fixes (typos, a wrong error message, a template bug with a clear
reproduction) can go straight to a PR.

## Setting up

1. **Go 1.25 or newer.** The tool's own `go.mod` says 1.25. Generated apps
   currently resolve to `go 1.26.0` after `go mod tidy` (pulled up by the
   pinned sqlc), and Go's default `GOTOOLCHAIN=auto` fetches that
   automatically.
2. **Docker with Compose v2.** The generated apps' Postgres runs there, on
   port 5440.
3. **golangci-lint v2:**
   `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.
   `bin/ci` looks for it on `PATH`, then in `$(go env GOPATH)/bin`, and fails
   loudly if it is missing.
4. Optional: **`gh`**, logged in, with `gh extension install basecamp/gh-signoff`.
   Without it, `bin/ci` still runs green and says the run was not recorded.
5. Clone and run everything:

   ```sh
   git clone https://github.com/bogie-go/bogie.git
   cd bogie
   go install .
   bin/ci
   ```

   A green run takes about a minute. It scaffolds two apps (one with
   `--jobs`), runs every generator and `destroy` against them, runs each app's
   own pipeline, builds the Docker image, and checks `/healthz`.

## The checks

| Command | What it does |
| --- | --- |
| `bin/ci` | **The check of record.** gofmt, vet, golangci-lint, the tool's tests, then `bogie new` into a temp dir, the generated app's own `bin/ci`, every generator plus `destroy`, the image build, and a `/healthz` check. A green run on a clean tree ends with `gh signoff`. |
| `make test` | `go test -race -cover ./...` for the tool only. Fast. |
| `make lint` | gofmt check, `go vet`, golangci-lint. |
| `make scaffold` | Generates an app in a temp dir and builds, vets and tests it. |
| `go run . new NAME --pretend` | Shows what `new` would write, without writing it. |
| `go test ./internal/generate -update` | Rewrites the generators' golden files after an *intended* output change. Review the diff. That diff is the change. |

There is no `.github/workflows`, by design (see DESIGN.md §8, after Rails
8.1's `bin/ci`). **Run `bin/ci` before you open a PR**, and paste its final
summary into the PR description. A failed run leaves the scaffolded app under
`/tmp/tmp.*` so you can look at it. A green run removes it.

## How the code is laid out

```
main.go                 the command table; every command spelled as Rails spells it
internal/commands/      new, generate, destroy, doctor, app:update, the proxy into the app
internal/generate/      the generators, pure (clock passed in), pinned by golden files in testdata/
internal/markers/       inserting and removing lines above // bogie:… markers
internal/scaffold/      rendering templates/app into a directory
templates/app/          the generated app, go:embed-ed
docs/                   DESIGN.md (the why), STATUS.md (the state), FROM_RAILS.md (the dictionary)
```

### Templates

- A `.tmpl` suffix means `text/template` over `scaffold.Vars` (Name, Module,
  EnvPrefix, LayoutVersion, Jobs). A `dot_` path segment becomes a dotfile.
  Files under `bin/` are written executable.
- A template that renders to only whitespace produces no file. That is how
  `{{if .Jobs}}` leaves `app/jobs` out. Watch the newlines around `{{end}}`.
- **Never leave a bare `.go` file under `templates/`.** The tool's own build
  would compile it.
- Every rendered Go file has to be gofmt-clean and pass the generated app's
  `.golangci.yml`, in **both** the plain and the `--jobs` shape. `bin/ci`
  checks both.
- Composite literals like `[]T{{...}}` look like template actions. Write them
  across separate lines.
- Template comments say what a line is *for*. The generated app is read by
  people and agents who never saw the conversation that produced it.

### Generators and markers

- Generators insert one line above a marker (`// bogie:controllers`,
  `// bogie:wire`, `// bogie:routes`, `// bogie:jobs`), idempotently, add the
  import, and format the file with `go/format`.
- **Every marker is checked before any file is written.** A missing marker is
  a named error with no partial write, and there are tests for that. Keep it so.
- Output follows Rails: `create` / `insert` / `skip` / `conflict` per file,
  `--pretend` for a dry run, and nothing is overwritten without `--force`.
- `destroy` has to undo everything `generate` did, registration included.
  `bogie doctor` has to keep passing afterwards.

### Things that look wrong and are not

Please don't "fix" these. Each one is a decision recorded in DESIGN.md or STATUS.md:

- Rails layout (`app/controllers`, `db/migrate`) and underscored package names
  (`admin_controller`). ST1003 is off for that reason, and nothing else is.
- No `spec/` directory. Go tests sit beside the code, stdlib `testing` only.
- Every generated column is `NOT NULL` with a default, so sqlc emits no pointers.
- Test never reads a credentials file.
- River's schema is applied by `rivermigrate` at `migrate`, not copied into `db/migrate`.
- Migrations run at boot by default (`<NAME>_MIGRATE_AT_BOOT=false` turns it off).
- Postgres only. SQLite was considered and declined (DESIGN.md §12.4).

### The six rules generated apps follow

Templates and generator output must keep to these (DESIGN.md §3; written
into every app's `AGENTS.md`):

1. `ctx context.Context` first for anything doing I/O.
2. Interfaces are declared by the consumer.
3. `app/domain` imports nothing third-party.
4. Constructors return `(T, error)`. Only `main` exits.
5. `*gin.Context` never travels below `app/controllers`.
6. No `util/`, `helpers/`, `common/`.

## Commits and pull requests

- **One change per commit, and the message explains why.** The project's
  `git log` is part of its documentation (STATUS.md: "the commit messages carry
  the reasoning"). Look at a few recent commits for the style:
  - The subject is an imperative sentence in plain English, capitalised, no
    trailing period, no `feat:`/`fix:` prefix. For example: *"Test that a missing
    marker writes nothing (§8.3)"*, *"Accept Post and AddSlugToPosts as Rails does"*.
  - The body says what was wrong or missing, what was considered, and what was
    decided. Reference DESIGN.md sections where they apply.
- **If generator output changed**, include the updated golden files in the
  same commit and say in the PR why the output should change.
- **If a decision changed**, the DESIGN.md edit goes in the PR too, ideally
  as the first commit.
- Keep docs in step: if you change what a command does, update `docs/STATUS.md`,
  `docs/FROM_RAILS.md` and the generated `AGENTS.md` where they mention it.
- **AI-assisted contributions are fine.** Bogie itself is developed with
  Claude Code, and its commits carry a `Co-Authored-By` trailer. You are
  responsible for every line you submit. Say in the PR if an agent wrote
  substantial parts, and add the trailer.

## What to expect

- **One maintainer.** Reviews happen when the maintainer has time. A PR that is small, comes with a green `bin/ci`
  summary, and touches one concern gets merged fastest.
- **Early.** Templates and generator output may change between versions.
  `bogie app:update` exists to carry generated apps forward, so a template
  change also needs to merge cleanly into an app generated by the previous
  version.
- **Scope is deliberately narrow.** "Bogie should also do X" will often get a
  "no, and here is why", with a pointer to DESIGN.md or to a tool that does X
  well (Andurel for a full-stack Rails-like framework, for example). That's
  not a judgement of the idea. It's the project staying small enough for one
  person to maintain.

## Security

Please don't post the details of a security problem in a public issue.
Open an issue that says you have something to report privately, without
details, and the maintainer will arrange a private channel.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE) that covers the project.
