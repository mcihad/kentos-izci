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

# Fuzz targets (package:function) for the parsers of untrusted input.
FUZZ_TARGETS := \
	./internal/ingest/sentry:FuzzEnvelope ./internal/ingest/sentry:FuzzDecodeEvent \
	./internal/symbols/sourcemap:FuzzParse ./internal/symbols/native:FuzzOpen \
	./internal/symbols/native:FuzzDartSymbolMap ./internal/search:FuzzParse \
	./internal/ownership:FuzzRules ./internal/api:FuzzSearchSQL

.PHONY: help help-check setup db-setup db-reset migrate demo gen lint-proto web build docker run dev test test-go test-web lint typecheck check audit fuzz clean

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

#? DESTROYS all data in the DATABASE_URL database: drops the public schema
#? and applies every migration again. Development only (refuses in
#? production). Uses DB_ADMIN_URL.
#> make db-reset
db-reset: ## Wipe and re-create the development database
	$(GO) run ./cmd/izci db reset

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
	rm -rf gen sdk/go/proto web/src/gen docs/api
	$(BUF) generate --template buf.gen.yaml
	cd sdk/go && $(GO) mod tidy
	$(GO) mod tidy

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

#? Builds the container image: web UI and static binary in build stages, a
#? Debian slim runtime with git, running as the non-root `izci` user with
#? /var/lib/izci as data volume and `izci healthcheck` as HEALTHCHECK.
#? WITH_OPENCODE=true also installs the opencode CLI for AI analyses.
#= IMAGE          image reference (default: izci:$(VERSION))
#= WITH_OPENCODE  true to include opencode (default: false)
#> make docker
#> make docker IMAGE=registry.example.com/izci:v1.0.0 WITH_OPENCODE=true
docker: ## Build the Docker image
	docker build \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE) \
		--build-arg WITH_OPENCODE=$(or $(WITH_OPENCODE),false) \
		--tag $(or $(IMAGE),izci:$(VERSION)) .

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

#? Fills the development database with demo projects (Kalem, Platform,
#? Bildirim, Kalem Web) and realistic events spread over the last days.
#= EVENTS  number of events (default 20000)
#> make demo
#> make demo EVENTS=100000
demo: ## Seed demo projects and events
	$(GO) run ./cmd/izci demo seed --events $(or $(EVENTS),20000) --days 14

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
	$(GO) test -race -count=1 $(ARGS) ./... ./sdk/go/... ./sdk/go/izcipgx/...

#? Runs the web unit tests (vitest).
#> make test-web
test-web: ## Run web tests
	$(PNPM) -C web test --passWithNoTests

#? Static analysis: gofmt, go vet, staticcheck, Biome (web) and proto lint.
#> make lint
lint: lint-proto ## Run all linters
	@unformatted=$$(gofmt -l cmd internal docs sdk tools web/embed.go); \
	if [ -n "$$unformatted" ]; then echo "gofmt -w needed for:"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./... ./sdk/go/... ./sdk/go/izcipgx/...
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

#? Scans dependencies for known vulnerabilities: govulncheck for the Go
#? code (only vulnerabilities in code izci calls) and pnpm audit for the
#? web UI's production dependencies. Needs network access. CI also scans
#? the container image (Trivy) and records its SBOM.
#> make audit
audit: ## Scan dependencies for known vulnerabilities
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	$(PNPM) -C web audit --prod --audit-level high

#? Fuzzes the parsers of untrusted input: Sentry envelopes and events,
#? source maps, ELF, Mach-O, DWARF and Dart obfuscation maps, the search
#? language and the SQL it becomes, ownership rules and CODEOWNERS. Each
#? target runs FUZZTIME; failing inputs are saved under the package's
#? testdata/fuzz and run with every `go test` from then on.
#= FUZZTIME  time per target (default: 30s)
#> make fuzz
#> make fuzz FUZZTIME=5m
fuzz: ## Fuzz the parsers of untrusted input
	@for t in $(FUZZ_TARGETS); do \
		echo "fuzz $$t"; \
		$(GO) test $${t%%:*} -run '^$$' -fuzz "^$${t##*:}$$" -fuzztime $(or $(FUZZTIME),30s); \
	done

#? Removes build outputs (bin/, web/dist contents).
#> make clean
clean: ## Remove build outputs
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
