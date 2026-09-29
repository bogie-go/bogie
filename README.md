# Bogie

A CLI that scaffolds a Rails-shaped Go service out of tools the Go ecosystem
already ships — Gin, sqlc, goose, Kamal — and builds only the parts that have
no equivalent: a `rails new`-style generator, and generators that also *wire*
new code in, not just write it.

A **bogie** is the wheeled frame under a rail car that carries it along the
track. That's the idea: the frame that lets a Go service run on Rails-shaped
conventions. The car body — your application code — is yours; Bogie never
ships a runtime dependency into it.

    go install github.com/bogie-go/bogie@latest
    bogie new blog
    cd blog && bogie server

Status: **design phase**. Nothing here is built yet. See
[`docs/DESIGN.md`](docs/DESIGN.md) for the full design, the reasoning behind
each choice, and the open questions.

## Why

Rails developers reaching for Go to write something that touches a network —
a webhook receiver, a socket gateway, a worker — lose the conventions and
generators that make Rails fast to work in. Bogie is not a runtime framework;
it never gets imported by the app it generates. It's glue over the existing Go
ecosystem, plus the small number of things that ecosystem doesn't provide.

## Status

Pre-alpha. Design only — see [`docs/DESIGN.md`](docs/DESIGN.md).

## License

MIT — see [`LICENSE`](LICENSE).
