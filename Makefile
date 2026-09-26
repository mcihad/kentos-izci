# izci — developer entry points.
#
# Every documented target carries three kinds of annotation directly above it:
#   #? description   #= VAR  variable help   #> example invocation
# `make help` lists targets, `make help T=<target>` shows one in detail and
# `make help-check` fails if a target is missing its description or example.

SHELL        := /bin/bash
.SHELLFLAGS  := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO     ?= go
PNPM   ?= pnpm
BUF    ?= buf
BIN    ?= bin/izci
PKG    := github.com/mcihad/kentos-izci

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --verify -q HEAD 2>/dev/null || true)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

.PHONY: help help-check setup db-setup migrate gen lint-proto web build run dev test test-go test-web lint typecheck check clean

#? Lists every target with a one-line summary. With T=<target> prints that
#? target's full description, variables and examples.
#= T  target to describe in detail
#> make help
#> make help T=build
help: ## Show available targets (T=<target> for details)
	@awk -v target="$(T)" -f scripts/make-help.awk $(MAKEFILE_LIST)

#? Verifies that every documented target has a description (#?) and an
#? example (#>). It also fails when it finds suspiciously few targets, so a
#? broken scan cannot pass silently.
#> make help-check
help-check: ## Verify that all targets are documented
	@awk -v mode=check -f scripts/make-help.awk $(MAKEFILE_LIST)

#? Installs web dependencies with pnpm and downloads Go modules. Run once
#? after cloning and whenever package.json or go.mod change.
#> make setup
setup: ## Install Go and web dependencies
	$(GO) mod download
	$(PNPM) -C web install --frozen-lockfile

#? Creates the dedicated non-superuser `izci` role plus the `izci` and
#? `izci_test` databases (idempotent). Uses DB_ADMIN_URL (a superuser
#? connection) and the role/password from DATABASE_URL in .env.
#> make db-setup
db-setup: ## Create the izci role and databases (uses DB_ADMIN_URL)
	$(GO) run ./cmd/izci db bootstrap

#? Applies pending schema migrations to DATABASE_URL. `izci serve` does the
#? same on startup unless DB_AUTO_MIGRATE=false.
#> make migrate
migrate: ## Apply database migrations
	$(GO) run ./cmd/izci migrate up

#? Regenerates Go (server + SDK), TypeScript (web/src/gen) and the English
#? API reference (docs/api) from proto/. Lints the protos first; every
#? message, field, enum value, service and RPC must carry a comment.
#> make gen
gen: lint-proto ## Generate code and API docs from proto
	$(BUF) generate --template buf.gen.yaml
	$(GO) run ./tools/protodoc -out docs/api

#? Lints proto files (STANDARD + COMMENTS rules) and checks formatting.
#> make lint-proto
lint-proto: ## Lint and format-check proto files
	$(BUF) lint
	$(BUF) format --diff --exit-code

#? Builds the web UI into web/dist (precompressed .br/.gz assets). The Go
#? binary embeds this directory.
#> make web
web: ## Build the web UI
	$(PNPM) -C web build

#? Builds the single static binary (web UI embedded) into $(BIN).
#= VERSION  version string baked into the binary (default: git describe)
#= BIN      output path (default: bin/izci)
#> make build
#> make build VERSION=v1.0.0 BIN=/tmp/izci
build: web ## Build bin/izci with the UI embedded
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/izci

#? Builds and starts the server with the local .env.
#> make run
run: build ## Build and run the server
	./$(BIN) serve

#? Runs the Go server (:5300) and the Vite dev server with hot reload
#? (:5301, proxies API calls to :5300). Open http://localhost:5301.
#> make dev
dev: ## Run Go server + Vite dev server (HMR)
	@trap 'kill 0' INT TERM EXIT; \
	$(GO) run ./cmd/izci serve & \
	$(PNPM) -C web dev; \
	wait

#? Runs Go unit and integration tests plus web unit tests. Integration tests
#? use IZCI_TEST_DATABASE_URL (from the environment or .env) and are skipped
#? when it is unset.
#> make test
test: test-go test-web ## Run all tests

#? Runs the Go test suite with the race detector.
#= ARGS  extra arguments for go test (e.g. -run TestIngest)
#> make test-go
#> make test-go ARGS="-run TestSPA -v"
test-go: ## Run Go tests
	$(GO) test -race -count=1 $(ARGS) ./... ./sdk/go/...

#? Runs the web unit tests (vitest).
#> make test-web
test-web: ## Run web tests
	$(PNPM) -C web test --passWithNoTests

#? Static analysis: go vet, staticcheck, Biome (web) and proto lint.
#> make lint
lint: lint-proto ## Run all linters
	$(GO) vet ./... ./sdk/go/...
	$(GO) tool staticcheck ./... ./sdk/go/...
	$(PNPM) -C web lint

#? Type-checks the web app with TypeScript.
#> make typecheck
typecheck: ## Type-check the web app
	$(PNPM) -C web typecheck

#? The full quality gate run before every commit: documentation check,
#? linters, type-check and all tests.
#> make check
check: help-check lint typecheck test ## Run every quality gate

#? Removes build outputs (bin/, web/dist contents).
#> make clean
clean: ## Remove build outputs
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
