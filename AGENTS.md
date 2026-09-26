# AGENTS.md — izci

Guide for coding agents and humans working **on** this repository. User-facing
guides live in `docs/` (Turkish); this file is about building izci itself.

## What izci is

izci is a Sentry-style error tracking and aggregation platform for KentOS
applications, shipped as **one static Go binary** that serves everything on
**one port**:

- **Ingest** over gRPC / gRPC-Web / Connect (`proto/izci/ingest/v1`) with
  per-project API keys (`izk_…`), plus a **Sentry-compatible** envelope
  endpoint so existing Sentry SDKs can report to izci.
- **Grouping** of events into issues by a documented fingerprint algorithm,
  with customer / tenant / project breakdowns.
- **Web UI** (React, embedded with `go:embed`) talking Connect
  (`proto/izci/api/v1`), **Telegram** alerts, **AI analysis** with opencode that
  can open fix branches / pull requests on GitHub, and an **MCP** server.
- **Multi-tenant** from day one (PostgreSQL row-level security) for a future
  SaaS offering.

The approved design and phase plan: see the plan referenced in the first
commit message and the phase table in `docs/mimari.md` (once written).

## Absolute rules

1. **Language.** The web UI is **Turkish only** (no i18n library). **All code
   is English**: identifiers, comments/godoc, SQL schema, proto, log messages,
   CLI commands and help text, commit messages, this file. Telegram messages and
   AI analysis reports are Turkish (user-facing). API errors carry an English
   message plus a machine-readable `reason`; the UI maps `reason` to Turkish.
2. **One listener.** `net/http` + connect-go on a single port (HTTP/1.1 +
   h2c, or TLS). No web framework — Fiber/fasthttp cannot do HTTP/2, which native
   gRPC needs. New endpoints are mounted with `server.Handle`.
3. **Tenant isolation.** Tenant-owned tables are touched **only** inside
   `db.InOrg(ctx, orgID, …)` or, for trusted server-side paths,
   `db.InSystem(ctx, …)`. izci connects as the non-superuser `izci` role that
   owns the tables (FORCE ROW LEVEL SECURITY). Never grant it SUPERUSER or
   BYPASSRLS; `izci serve` refuses such roles in production.
4. **Contracts are documented.** Every proto message, field, enum value,
   service and RPC has a comment — `buf lint` (COMMENTS) enforces it. Comments
   state meaning, units, limits, defaults and examples. Generated code
   (`gen/`, `sdk/go/proto/`, `web/src/gen/`, `docs/api/`) is never edited by hand.
5. **SQL is hand-written with pgx** (no ORM, no sqlc — sqlc cannot parse PG18
   `RETURNING old/new`). PostgreSQL **18+** is required.
6. **Static binary.** `CGO_ENABLED=0` must keep building. The UI is embedded.
7. **Secrets.** `.env` is never committed; secrets are never logged (types like
   `config.SecretKey` redact themselves). Stored third-party tokens are
   encrypted with AES-GCM using `IZCI_SECRET_KEY`.
8. **Migrations are append-only once released.** Never edit a migration that
   shipped in a release; add a new goose file in `internal/db/migrations` with
   both `Up` and `Down`. Before the first release the schema may still be
   reshaped in place (reset local databases with `make db-reset`).

## Map

| Path | What lives there |
|---|---|
| `cmd/izci` | `main` — calls `internal/cli` |
| `internal/cli` | cobra commands (`serve`, `migrate`, `db bootstrap`, `version`, …) |
| `internal/config` | `.env` + environment → typed, validated `Config` |
| `internal/server` | the single `http.Server` (h2c/TLS), middleware, SPA handler, health |
| `internal/app` | wires everything (DB, ingest pipeline, services, jobs, HTTP server) |
| `internal/db` | pgx pool, `InOrg` / `InSystem`, goose migrations, partitions, retention, bootstrap |
| `internal/store` | hand-written SQL per aggregate; takes the scoped `pgx.Tx` |
| `internal/auth` | argon2id passwords, sessions, personal access tokens, OIDC (PKCE), tenant resolution |
| `internal/api` | izci.api.v1 Connect services (one `*_service.go` per service); `authz.go` holds the per-procedure authorization table, `issue_filter.go` turns search queries into SQL, `series.go` builds stats-period buckets |
| `internal/search` | issue search language parser (`is:unresolved level:>=error customer:x "text"`) |
| `internal/realtime` | in-process hub fanning committed ingest batches and triage out to `WatchLive` streams |
| `internal/alert` | alert rules evaluated inside the ingest transaction (savepoint) → transactional notification outbox |
| `internal/telegram` | Bot API client, outbox dispatcher (edit-in-place, flood control), chat/account linking, signed triage buttons, polling or webhook |
| `internal/ingest` | normalize → scrub → in-app → grouping → group-commit writer; `IngestService` |
| `internal/clientip`, `internal/secret`, `internal/slug` | client address, tokens/AES-GCM/HMAC, slugs |
| `internal/jobs` | periodic background tasks (usage flush, partitions, retention, project purge) |
| `internal/sample` | realistic sample events (demo, bench, test events) |
| `internal/testutil` | per-test PostgreSQL schemas (`NewDB`) |
| `internal/logging`, `internal/version` | slog setup, build metadata |
| `tools/protoc-gen-izcidoc` | protoc plugin rendering `docs/api` from proto comments |
| `proto/izci/ingest/v1` | public ingest contract (Event model, IngestService) |
| `proto/izci/api/v1` | UI / external API contract |
| `sdk/go` | separate Go module: Go SDK + generated ingest code (the server imports it) |
| `web` | Vite + React + TypeScript UI; `web/embed.go` embeds `web/dist` |
| `docs` | Turkish user guides; `docs/api` English reference generated from proto |
| `scripts` | build helpers (`make-help.awk`) |

## Commands

```bash
make help                 # every target; `make help T=<target>` for details
make setup                # install Go + web dependencies
make db-setup             # create the izci role + izci/izci_test databases
make dev                  # Go server :5300 + Vite HMR :5301 (open :5301)
make build && make run    # static binary with the UI embedded
make check                # the full gate: help-check, lint, typecheck, tests
```

Local PostgreSQL is the `postgis_db` container from
`/Users/cihad/Projects/database/docker-compose.yml` (PostgreSQL 18, :5432).

## Definition of done

A change is done when:

- `make check` is green (Go tests run with `-race`; integration tests use
  `IZCI_TEST_DATABASE_URL` and must not be skipped locally);
- new behaviour has tests (table-driven for pure logic, real PostgreSQL for
  anything touching SQL or RLS);
- new environment variables are in `.env.example` **and** `docs/yapilandirma.md`;
- proto changes are commented and regenerated (`make gen`);
- UI copy is Turkish and the screen works at 375px and on desktop, light and dark;
- the relevant Turkish guide in `docs/` is updated in the same commit.

## Conventions

- **Go:** errors are wrapped with context (`fmt.Errorf("create pool: %w", err)`);
  `context.Context` is the first parameter; slog attributes use `snake_case`
  keys; no global state besides the default logger.
- **Web:** Tailwind v4 tokens from `web/src/index.css` (never hardcode colors or
  sizes), shadcn/ui (new-york) + ReUI components, `cn()` for class merging,
  `data-slot` on primitives, `cva` for variants, mobile-first layouts.

## Web UI

- **Layout of `web/src`:** `routes/` (TanStack Router, file based, English
  paths: `/o/$org/issues/$issueId`), `lib/` (transports, errors, formatting,
  labels, session, triage, highlighting), `components/` by area (`app`,
  `issue`, `event`, `customer`, `project`, `settings`, `account`, `charts`,
  `common`, `brand`; `ui` holds generated shadcn primitives and is not linted).
- **Data:** Connect-Query hooks only. Organization-scoped calls use the
  transport of `useOrg()` (it sends `Izci-Org`); each organization has its own
  transport, so caches never mix tenants. Invalidate with
  `createConnectQueryKey({ schema: Service, transport: org.transport, cardinality: undefined })`.
- **Copy:** Turkish, sentence case, active verbs ("Çöz", "Anahtar oluştur").
  Errors say what happened and what to do, without apologizing; API errors
  are mapped by `ErrorReason` in `lib/errors.ts`.
- **Design language:** graphite neutrals, one teal accent (`primary`) for
  interaction and selection, level colors only for severity. Stack traces and
  breadcrumbs share the "trail" (`components/event/stack-trail.tsx`): in-app
  frames get teal blazes, library frames hollow dots, the crash site a mark in
  the level color. Monospace is reserved for code and identifiers.
- **Budget:** `pnpm build` fails when first-load JavaScript exceeds 200 KB
  gzip (`web/scripts/check-bundle.mjs`). Route definitions (`validateSearch`,
  `beforeLoad`) stay in the entry chunk, so they must not reference generated
  message code; syntax grammars and pages load on demand.
- **Generated code:** `buf/validate/validate_pb` is aliased to a small stub in
  `vite.config.ts`; the UI never evaluates validation rules.
- **Commits:** Conventional Commits in English (`feat(ingest): …`).
