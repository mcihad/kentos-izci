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
| `internal/cli` | cobra commands (`serve`, `migrate`, `db bootstrap`, `healthcheck`, `version`, …) |
| `internal/config` | `.env` + environment → typed, validated `Config` |
| `internal/server` | the single `http.Server` (h2c/TLS), middleware, SPA handler, health |
| `internal/app` | wires everything (DB, ingest pipeline, services, jobs, HTTP server) |
| `internal/db` | pgx pool, `InOrg` / `InSystem`, goose migrations, partitions, retention, bootstrap |
| `internal/store` | hand-written SQL per aggregate; takes the scoped `pgx.Tx` |
| `internal/auth` | argon2id passwords, sessions, personal access tokens, OIDC (PKCE), two-factor authentication (TOTP, recovery codes), tenant resolution |
| `internal/api` | izci.api.v1 Connect services (one `*_service.go` per service); `authz.go` holds the per-procedure authorization table, `issue_filter.go` turns search queries into SQL, `series.go` builds stats-period buckets |
| `internal/search` | issue search language parser (`is:unresolved level:>=error customer:x "text"`) |
| `internal/realtime` | in-process hub fanning committed ingest batches and triage out to `WatchLive` streams |
| `internal/alert` | alert rules evaluated inside the ingest transaction (savepoint) → transactional notification outbox |
| `internal/ownership` | issue ownership: Sentry-syntax rules and CODEOWNERS (`rules.go`, last match wins, CODEOWNERS first), owner aliases, automatic assignment of new issues inside the ingest transaction (savepoint, before alerts) or by suspect commit in a background worker, suspect commits by GitHub GraphQL blame of the innermost in-app frames (`suspect.go`) |
| `internal/monitor` | cron monitoring: schedules (crontab/interval in a time zone), check-in processing (`Process`), the `monitor-sweep` job that records missed and timed-out runs; incidents are synthetic events pushed through the ingest pipeline (`ingest.NewIncidentEmitter`), so grouping, alert rules and Telegram apply unchanged |
| `internal/symbols` | stack trace resolution before grouping: source maps (`sourcemap/`, VLQ, index maps, UTF-16 columns) and DWARF of ELF/Mach-O debug files (`native/`, Flutter/Dart AOT, inlined frames, Dart obfuscation maps); size-bounded caches; assembly of chunked uploads (artifact bundles, debug files) |
| `internal/sentryapi` | the subset of Sentry's web API that sentry-cli, the bundler plugins and sentry_dart_plugin use (`/api/0/…`: chunk upload, artifact bundle and debug file assembly, releases, deploys); authenticated by ingest keys with the releases scope or PATs with write scope — verify changes with the real tools (see `docs/kaynak-eslemeleri.md`) |
| `internal/notify` | notification delivery: the `Dispatcher` claims outbox rows (SKIP LOCKED) and routes them by channel kind to a `Sender` (Telegram bot, e-mail/SMTP, Slack, Teams, signed webhooks, Jira); senders report `RetryError`/`PermanentError`; channel secrets are sealed per channel id; outgoing HTTP refuses private networks unless `NOTIFY_ALLOW_PRIVATE_NETWORKS` |
| `internal/telegram` | Bot API client, the Telegram `notify.Sender` (edit-in-place, flood control), chat/account linking, signed triage buttons, polling or webhook |
| `internal/github` | GitHub REST client (no go-github), GitHub App JWT + cached installation tokens, OAuth check of who installed the app, sealed personal access tokens (`Access`); file contents, repository trees and GraphQL blame (`code.go`) |
| `internal/mcp` | MCP server (official Go SDK): tools, resources and prompts as a thin adapter over izci.api.v1 — called in-process with the caller's personal access token (`/mcp`, `izci mcp`) or over HTTPS (`izci mcp --url`), so authorization, scopes and RLS are the API's |
| `internal/ai` | AI runs: bare mirror + per-run worktree, context files and prompt, the opencode process (JSON events → run log), results and diff, publishing branches/pull requests, `Doctor` |
| `internal/ingest` | inbound filters (`filters.go`, before limits) → normalize → scrub → in-app → grouping → group-commit writer; `IngestService`; attachments, user feedback and release-health sessions (`items.go`, `sessions.go`); cron check-ins (`checkins.go`) |
| `internal/ingest/sentry` | Sentry envelope/store endpoints and the Sentry → izci mapping; `testdata/sdk` holds envelopes recorded from real Sentry SDKs — when changing the mapping, re-run them (`go test ./internal/ingest/sentry`) rather than hand-writing payloads |
| `internal/clientip`, `internal/secret`, `internal/slug` | client address, tokens/AES-GCM/HMAC, slugs |
| `internal/jobs` | periodic background tasks (usage flush, partitions, retention, project purge, monitor sweep) |
| `internal/sample` | realistic sample events (demo, bench, test events) |
| `internal/testutil` | per-test PostgreSQL schemas (`NewDB`) |
| `internal/metrics` | Prometheus metrics (VictoriaMetrics/metrics): process-wide counters updated on hot paths, per-instance gauges (queue, pool, live streams), `/metrics` with its access rule; labels never carry tenant data |
| `internal/selfmon` | self-monitoring: the Go SDK with `izcislog` wraps the server logger when `IZCI_SELF_*` is set, rate limited against feedback loops |
| `internal/logging`, `internal/version` | slog setup, build metadata |
| `tools/protoc-gen-izcidoc` | protoc plugin rendering `docs/api` from proto comments |
| `proto/izci/ingest/v1` | public ingest contract (Event model, IngestService) |
| `proto/izci/api/v1` | UI / external API contract |
| `sdk/go` | separate Go module: Go SDK + generated ingest code (the server imports it) |
| `web` | Vite + React + TypeScript UI; `web/embed.go` embeds `web/dist` |
| `docs` | Turkish user guides (embedded by `docs/embed.go`: in-app docs, `/llms.txt`, MCP resources; `go test ./docs` checks every link and heading anchor); `docs/api` English reference generated from proto |
| `examples` | Dependency-free clients (curl, .NET, Node, Python, Dart) and Sentry SDK samples; each must keep working against a local server |
| `scripts` | build helpers (`make-help.awk`) |
| `Dockerfile`, `.github/workflows/ci.yml` | multi-stage image (`make docker`, optional opencode, non-root, `izci healthcheck`); CI: generated code current, `make check` on PostgreSQL 18, binary and image build |
| `.github/workflows/security.yml`, `.github/dependabot.yml` | govulncheck, `pnpm audit`, Trivy image scan and SBOM on pushes, pull requests and weekly; weekly fuzzing of the parsers; weekly Dependabot updates |

## Commands

```bash
make help                 # every target; `make help T=<target>` for details
make setup                # install Go + web dependencies
make db-setup             # create the izci role + izci/izci_test databases
make dev                  # Go server :5300 + Vite HMR :5301 (open :5301)
make build && make run    # static binary with the UI embedded
make docker               # container image (WITH_OPENCODE=true adds opencode)
make check                # the full gate: help-check, lint, typecheck, tests
make audit                # govulncheck + pnpm audit (needs network)
make fuzz                 # fuzz the parsers of untrusted input (FUZZTIME=30s each)
```

Local PostgreSQL is the `postgis_db` container from
`/Users/cihad/Projects/database/docker-compose.yml` (PostgreSQL 18, :5432).

## AI runs (security model)

An AI run executes an agent on code and on error data that end users can
influence (prompt injection), so these invariants hold — keep them when
changing `internal/ai`:

- **The checkout is untrusted once the agent ran.** Git never discovers the
  repository from the checkout: commands get explicit `GIT_DIR` /
  `GIT_WORK_TREE`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1` and
  `-c core.fsmonitor=false -c core.hooksPath=/dev/null …` (see `gitCmd`), so a
  rewritten `.git` file cannot make izci execute code. Result files are read
  through `os.Root`, so symlinks cannot leak server files into the UI.
- **Publishing never uses the agent's checkout.** The commit is built in the
  mirror from the stored diff with a temporary index (`commitPatch`); what is
  pushed is exactly what reviewers saw. Pushes never force.
- **The agent gets no secrets and no reach.** Environment allowlist (no izci
  secrets, no GitHub token), isolated `XDG_CONFIG_HOME`,
  `OPENCODE_DISABLE_PROJECT_CONFIG` (repositories cannot load plugins, MCP
  servers or permissions), pinned permissions: edits inside the checkout, no
  shell unless the project allowlists commands (an allowlisted command is code
  execution — say so in the UI), no web, no LSP, no formatters.
- **Personal data stays home.** `shareable` strips user email/IP/name,
  cookies and forwarding headers before the event goes to the model provider.
- Remote URLs must be `https://` (`AllowInsecureRemotes` is for tests only).
  GitHub App installations are linked only after the installing GitHub user is
  verified through OAuth (`App.VerifyInstaller`).
- Tests use a fake `opencode` shell script; never call a real model in tests.
  `izci ai doctor` checks a real setup without calling a model.

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
- **Web:** the KentOS component kit `@kentos/components` (shadcn/ui new-york
  primitives, design tokens, themes; see below), Tailwind v4 tokens (never
  hardcode colors or sizes), `cn()` for class merging, `cva` for variants,
  mobile-first layouts.

## Web UI

- **Component kit:** generic components live in the sibling repository
  `../kentos-components` (package `@kentos/components`, its own `AGENTS.md`
  and showcase). `web/package.json` depends on it with
  `file:../../kentos-components/packages/components`, so pnpm hard-links its
  files into `node_modules`: edits to existing kit files show up at once; after
  adding or deleting kit files run `pnpm -C web components:sync`. Import
  primitives from `@kentos/components/ui/<name>` (button, dialog, select, …),
  generic pieces from `@kentos/components/common/<name>` (field, kbd,
  segmented, stat-strip, states, copy-button, user-avatar, table,
  appearance-panel), charts from `@kentos/components/charts/<name>` and
  `cn`/theme/format helpers from `@kentos/components/lib/<name>`. Anything
  reusable by other KentOS applications belongs in the kit (add a showcase
  demo there); izci-specific components stay in `web/src/components`. The
  Docker image gets the kit as a named build context (`make docker`,
  `KENTOS_COMPONENTS=…`) and CI clones it next to the checkout.
- **Layout of `web/src`:** `routes/` (TanStack Router, file based, English
  paths: `/o/$org/issues/$issueId`), `lib/` (transports, errors, formatting,
  labels, session, triage, highlighting), `components/` by area (`app`,
  `issue`, `event`, `customer`, `project`, `settings`, `account`, `charts`,
  `common`, `brand`). `components/common/states.tsx` and
  `components/charts/sparkline.tsx` wrap kit components for API types (errors,
  `Series`).
- **Data:** Connect-Query hooks only. Organization-scoped calls use the
  transport of `useOrg()` (it sends `Izci-Org`); each organization has its own
  transport, so caches never mix tenants. Invalidate with
  `createConnectQueryKey({ schema: Service, transport: org.transport, cardinality: undefined })`.
- **Copy:** Turkish, sentence case, active verbs ("Çöz", "Anahtar oluştur").
  Errors say what happened and what to do, without apologizing; API errors
  are mapped by `ErrorReason` in `lib/errors.ts`.
- **Design language:** the KentOS design system (made in Claude Design, the
  "KentOS Yönetim" kit): navy brand, cool greys, a dark navy rail, a grey
  page with white cards (`card` utility), 6px controls and 10px cards,
  navy-tinted shadows. Tokens live in the kit
  (`@kentos/components/styles/tokens.css`) in three layers: palette ramps
  (never used directly), semantic aliases (`--surface-*`, `--text-*`,
  `--brand-*`, `--status-*`) and the shadcn bridge; `web/src/index.css`
  imports them and adds izci's own (levels, trail, code, syntax). Level colors
  stay for severity only. Stack traces and breadcrumbs share the "trail"
  (`components/event/stack-trail.tsx`). Monospace is reserved for code and
  identifiers.
- **Pages:** every route renders `Page` from `components/app/page.tsx`, the
  kit's `PageWrapper` plus the tab title: `icon` (the page's sidebar icon),
  `title`, `description`, `breadcrumb` (`PageBreadcrumb`) on detail pages,
  `filters` for selects that change what is shown (they stay visible on
  phones) and `actions` built from `PageAction` / `PageActionMenu` (one
  `variant="default"` primary action; on phones they move into a floating
  button and bottom sheet). The body is unpadded: wrap sections in `gutter`
  so lists can run edge to edge on phones. The footer is off unless
  `showFooter`. The issue page keeps its own header (level strip, triage bar)
  inside a bare `PageWrapper`.
- **Dialogs and toasts:** results of actions are toasts (`toast` from
  `sonner`, shown with the tinted tone: success, error, warning and info on
  their status colours); questions use the kit's dialogs: `dialogs.confirm/prompt/alert`
  from `@kentos/components/common/dialog-host` in handlers (`<DialogHost />`
  is mounted in `routes/__root.tsx`) or `ConfirmDialog`, `InputDialog`,
  `ProgressDialog`… from `common/dialogs`. Pass `onConfirm` a promise whose
  rejection is an `Error` with `errorMessage(err)`, so the dialog shows it.
  Never `window.confirm`/`prompt`/`alert`.
- **Frame:** `components/app/app-shell.tsx` builds the frame from the kit's
  `layout/app-shell`: `AppShellSidebar` (in `sidebar.tsx`), `AppShellHeader`
  with the menu button, brand (phones), search and actions, and
  `AppShellMain` around the routes, and on phones the `BottomNav` tab bar
  (`bottom-nav.tsx`: overview, issues, live, dashboards and a Menü tab that
  opens the sidebar drawer). New frame elements (a banner, a status line)
  are added as AppShell regions, not hand-written.
- **Navigation:** `components/app/sidebar.tsx` builds the left menu from the
  kit's `layout/sidebar` (switcher, search, groups, account; icon rail on
  desktop, drawer on phones). Pages come from `components/app/nav.ts`: a new
  page gets an icon, a `group` and search `keywords` there, and the command
  palette lists it too.
- **Appearance:** users customize the theme per browser from the palette
  button (`components/app/appearance-sheet.tsx`, the kit's
  `AppearancePanel` in a sheet): light/dark/system, font
  (Plus Jakarta Sans, Inter, IBM Plex Sans), code font (JetBrains Mono, Fira
  Code, IBM Plex Mono, system), density, radius, accent and card elevation.
  Each choice is one scope class on `<html>` (`.font-*`, `.mono-*`,
  `.density-*`, `.radius-*`, `.accent-*`, `.elevation-*`); the kit's
  `lib/theme` and `public/theme-init.js` apply them and must stay in step
  (`main.tsx` calls `configureAppearance` and `index.html` passes the same
  keys to the kit's `theme-init.js` as data attributes). Components read
  control heights, gutters and radii from those tokens, never fixed values.
- **Readability:** fonts are Plus Jakarta Sans / Inter / IBM Plex Sans and
  JetBrains Mono for code. Font sizes come only from the kit's scale
  (`text-2xs` 12px … `text-3xl`); no `text-[Npx]`, nothing under
  12px. Text in a level or health color uses the `*-ink` token; text in the
  brand color uses `text-brand-ink` or `text-link`, never `text-primary`
  (a fill colour, too dark on dark surfaces). Syntax colors are the `--syn-*`
  variables (one Shiki css-variables theme for both modes).
- **Phones:** every table has a `md:hidden` list beside it; list and main
  cards run edge to edge below `sm`; tab strips use `TabStrip`/`TabNav`
  over the kit's `TabsNav` (items are `TabsNavItem asChild` around a
  `Link`, with `badge` for counts; the row scrolls by touch and wheel and
  keeps the current tab in view); in-card tabs use the kit's `Tabs`; controls never drop under 36–40px
  below `md`, and inputs use 16px text so iOS does not zoom.
- **Budget:** `pnpm build` fails when first-load JavaScript exceeds 200 KB
  gzip (`web/scripts/check-bundle.mjs`). Route definitions (`validateSearch`,
  `beforeLoad`) stay in the entry chunk, so they must not reference generated
  message code; syntax grammars and pages load on demand.
- **Generated code:** `buf/validate/validate_pb` is aliased to a small stub in
  `vite.config.ts`; the UI never evaluates validation rules.
- **Commits:** Conventional Commits in English (`feat(ingest): …`).
