# From Rails

A dictionary for the Rails developer reading or extending a Bogie app: what
each Rails thing is called here, where it lives, and what has no equivalent.
When Rails has a word for something, Bogie uses it; this file is for the
places the word is the same but the thing underneath is not, and for the
places there is no word because there is no thing.

The one idea to carry over: **Go has no autoload and no `method_missing`.**
Nothing exists until it is written down and registered, so every "magic"
Rails step has a visible line here, and the generators write those lines so
you do not have to find them.

## Commands

| Rails | Bogie | Notes |
| --- | --- | --- |
| `rails new blog` | `bogie new blog` | `--jobs` adds River. API-only, Postgres-only; no `--api`, no `-d`. |
| `rails server`, `rails s` | `bogie server`, `bogie s` | Migrates at boot, as a deployed container does. |
| `rails g scaffold post title:string` | `bogie g scaffold post title:string` | Model, views and a wired controller; no HTML. |
| `rails g model post title:string` | `bogie g model post title:string` | Migration, queries, domain type, store; runs sqlc. |
| `rails g controller posts index show` | `bogie g controller posts index show` | Writes it *and* registers it. `new` and `edit` are refused. |
| `rails g controller admin/reports` | `bogie g controller admin/reports` | A namespace is a package, `app/controllers/admin_controller/`. |
| `rails g migration add_slug_to_posts slug:string` | `bogie g migration add_slug_to_posts slug:string` | SQL, not Ruby; `create_`, `add_x_to_`, `remove_x_from_` get their SQL written. |
| `rails g job send_welcome` | `bogie g job send_welcome` | A River worker, registered; apps made with `--jobs`. |
| `rails g authentication` | `bogie g authentication secret`, `token` or `api_key` | A middleware on a route group; the shape says who the caller is. |
| (a service object, by hand) | `bogie g service publish_post` | A package with a `Run(ctx)`, not wired anywhere. |
| `rails destroy ...`, `rails d` | `bogie destroy ...`, `bogie d` | Removes the registration lines too. |
| `rails db:create` … `db:rollback` | the same words | `db:prepare`, `db:reset`, `db:seed`, `db:migrate:status`, `db:migrate:redo`, `db:version`. |
| `rails credentials:edit` | `bogie credentials:edit` | `-e production` for that environment's file and key. |
| `rails test` | `bogie test`, `bogie t` | `go test -race -cover ./...`; store tests need Postgres up. |
| `bin/ci` | `bin/ci` | The whole pipeline, locally; green signs off with `gh signoff`. |
| `rails routes` | — | Read `app/controllers/routes.go` and each `SetupRoutes`; it is one screen. |
| `rails console` | — | No REPL. `make psql` opens the database; a throwaway `_test.go` is the Go way. |
| `rails dbconsole` | `make psql` | |
| `bundle install` | `go mod download` | `go mod tidy` is `bundle` after a Gemfile edit. |
| `rubocop` | `bogie lint` | gofmt, go vet, golangci-lint; `.golangci.yml` is the config. |
| `kamal deploy` | `kamal deploy` | Same tool, same `config/deploy.yml`. |

There is also a `bogie doctor` with no Rails equivalent: it checks that the
markers the generators insert above are intact and that every controller is
both constructed and mounted, which Rails never needed to check because it
autoloads.

## Where things live

| Rails | Bogie | Notes |
| --- | --- | --- |
| `config/routes.rb` | `app/controllers/routes.go` | The table; each controller's `SetupRoutes` adds its own routes under a group. |
| `app/controllers/posts_controller.rb` | `app/controllers/posts_controller.go` | A `PostsController` type: its dependencies, routes and actions. |
| `app/controllers/admin/reports_controller.rb` | `app/controllers/admin_controller/reports_controller.go` | The package holds a `Server` with one field per controller and a `routes.go` for the group's middleware. |
| `app/models/post.rb` | `app/domain/post.go` + `app/models/posts.go` + `db/queries/posts.sql` | Split in three: the type and its validations; the store methods; the SQL. See below. |
| `app/views/posts/*.json.jbuilder` | `app/views/posts.go` | Request and response structs. A domain type never doubles as a wire type. |
| `app/jobs/send_welcome_job.rb` | `app/jobs/send_welcome.go` | An `Args` struct and a `Worker`, registered in `app/jobs/jobs.go`. |
| `app/services/` | `app/services/<name>/` | One package per service, `ctx` first, knows nothing about HTTP. |
| `app/helpers/`, `lib/utils.rb` | — | Rule 6: no `util`, `helpers` or `common`. A package is named for what it provides. |
| `app/controllers/concerns/` | — | Compose with plain functions and small interfaces. |
| `db/migrate/*.rb` | `db/migrate/*.sql` | goose SQL with `-- +goose Up` and `Down`, embedded in the binary. |
| `db/schema.rb` | — | The migrations are the schema; sqlc compiles the queries against them. |
| `db/seeds.rb` | `db/seeds/*.sql` | Versioned like migrations, in their own table. |
| `config/database.yml` | `DATABASE_URL` in credentials or the environment | One URL per environment; test never reads a credentials file. |
| `config/credentials.yml.enc`, `master.key` | the same files | Same pattern, same names, same `-e production`. |
| `config/application.rb`, initializers | `app/application.go` | Where everything is constructed and wired, in order, with the markers. |
| `config/puma.rb` | — | `net/http` with timeouts set in `app/controllers/application.go`. |
| `config/deploy.yml` | `config/deploy.yml` | Kamal 2, unchanged. |
| `Gemfile`, `Gemfile.lock` | `go.mod`, `go.sum` | Tools (sqlc, goose) are pinned there too, under `tool`. |
| `.rubocop.yml` | `.golangci.yml` | |
| `test/`, `spec/` | beside the code, `*_test.go` | Stdlib `testing`, no factories, no fixtures. |
| `Procfile.dev`, `bin/dev` | `make server`, `make worker` | |
| `AGENTS.md`, `CLAUDE.md` | the same | The rules, the commands, a recipe per task. |

## A model, in three places

Rails's `Post` is one class that is a table, a type and a query builder.
Here that is three files, each with one job:

- **`app/domain/post.go`** is the type and its `Validate`. It imports nothing
  third-party (rule 3). Ids are strings here; the store turns them into uuids.
- **`db/queries/posts.sql`** is the SQL, by name: `GetPost`, `ListPosts`,
  `CreatePost`. sqlc compiles it into `app/models/posts.sql.go`, and a
  query that names a column that does not exist fails at `make sqlc`, not
  in production.
- **`app/models/posts.go`** is the store: `CreatePost`, `GetPost` and so on,
  converting between the domain type and sqlc's row type, mapping "no rows"
  to `domain.ErrNotFound`, and running `Validate` before create and update.

So `Post.find(id)` is `store.GetPost(ctx, id)`, `Post.create(attrs)` is
`store.CreatePost(ctx, domain.Post{...})`, and a new query is a new named
statement in the `.sql` file plus a store method that calls it. There is no
`where` builder; the SQL is written out, which is the point.

Associations are a foreign key and a query. `post:references` on a model
writes the column, the constraint and the index; `has_many :comments` is
`ListCommentsForPost` in `db/queries/comments.sql` when you need it.

## In the code

| Rails | Bogie |
| --- | --- |
| `params[:id]` | `c.Param("id")` |
| `params.require(:post).permit(:title)` | `var req views.PostRequest; c.ShouldBindJSON(&req)`; only the struct's fields come through |
| `render json: post` | `c.JSON(http.StatusOK, postView(post))` |
| `head :no_content` | `c.Status(http.StatusNoContent)` |
| `validates :title, presence: true` | `func (p Post) Validate() error { ... return fmt.Errorf("%w: title is required", ErrInvalid) }` in `app/domain`; runs on save |
| `rescue_from ActiveRecord::RecordNotFound` | `ctl.fail(c, err)`: `ErrNotFound` is 404, `ErrInvalid` is 422, anything else is a logged 500 |
| `before_action :authenticate` | middleware on the group, in `routes.go`, so the exposure boundary is visible in one place; `bogie g authentication` writes it and a `WithUser` group helper |
| `Current.user` | `middlewares.Subject(c)`, the verified token's subject; load the record if you need it |
| `Post.find(id)` | `ctl.Store.GetPost(ctx, id)` |
| `post.save` | `store.CreatePost(ctx, post)` or `store.UpdatePost(ctx, post)`; validation runs inside |
| `SendWelcomeJob.perform_later(id)` | `app.Jobs.Insert(ctx, jobs.SendWelcomeArgs{ID: id}, nil)`; `InsertTx` inside a transaction |
| `Rails.logger.info` | `log.InfoContext(ctx, ...)`, a `*slog.Logger` passed in, never global |
| `Rails.env.production?` | `cfg.Env == config.EnvProduction` |
| `ENV["X"]`, `Rails.application.credentials.x` | a field on the `config.Config` struct, validated at boot |
| `request.request_id` | `c.GetString("request_id")`, set by the middleware |
| `Rails.cache` | — |
| `ActiveSupport::Notifications` | the request log line; OpenTelemetry rides in `ctx` when you add it |

## What is deliberately different

These have no Rails counterpart because Rails does them implicitly. In Go they
are explicit, and the six rules in `AGENTS.md` keep them that way.

1. **Dependencies are passed in, in `app/application.go`.** Nothing reaches
   for a global. A controller gets its store on its wire line; a test hands
   it a fake.
2. **Interfaces are declared by the consumer** (rule 2), not by the thing
   implemented. `postsStore` lives beside `PostsController` and lists only
   the five methods it calls. That is why a controller test needs no
   database, and why there is no mocking library.
3. **`ctx context.Context` is the first parameter** of anything that does
   I/O (rule 1). Cancellation, deadlines and tracing travel in it; a request
   that is abandoned stops its query.
4. **Constructors return `(T, error)`** (rule 4); only `main` exits. A bad
   configuration fails at boot with a message, never with a panic in a
   handler.
5. **`*gin.Context` stops at `app/controllers`** (rule 5). A handler converts
   HTTP into domain values, calls one function, and converts the result
   back. Below that, the code can be called from a job, a CLI or a test.
6. **Registration is written down.** A controller exists once it is held,
   constructed and mounted, three lines above three `// bogie:` markers. The
   generators write them; `bogie doctor` checks them.

## What has no equivalent

Honest list. Some are API-only by design; some are Rails features Go does
not need; some are gaps.

- Views, templates, assets, Turbo, sessions, flash, cookies, CSRF: the Rails
  app renders; this service is JSON.
- `rails console` and `rails routes`.
- `ActiveRecord` associations, callbacks, scopes, `where` chains, N+1
  helpers. SQL is written out and compiled.
- `db/schema.rb` and `db:schema:load`; the migrations are the schema.
- Fixtures, factories, `test/test_helper.rb`. Store tests truncate the test
  database and insert what they need.
- Concerns, helpers, `lib/tasks`. Plain packages and the Makefile.
- Mailers, Action Cable, Active Storage, I18n, caching. Wire a library when a
  service needs one; see the "glue, then gaps" rule in `docs/DESIGN.md`.
- `rails g resource`, `rails g channel`, `rails g mailer`.
