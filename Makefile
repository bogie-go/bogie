MAKEFLAGS += --no-print-directory

.PHONY: help build test vet fmt lint scaffold

help: ## every task
	@grep -E '^[a-z_%-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## compile bin/bogie
	go build -o bin/bogie .

test: ## run the tool's suite
	go test -race -cover ./...

vet: ## stdlib static analysis
	go vet ./...

fmt: ## format
	gofmt -s -w .

lint: vet ## everything CI checks on the tool
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

# The CI scaffold job, locally: generate an app into a temp dir and hold it to
# the bar every generated app must meet.
scaffold: build ## generate an app in a temp dir and build, vet, lint, test it
	@dir=$$(mktemp -d) && cd $$dir && $(CURDIR)/bin/bogie new blog --module=example.com/blog && cd blog \
	  && go build ./... && go vet ./... && (test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }) \
	  && go test -race -cover ./... && echo "scaffold ok: $$dir/blog"
