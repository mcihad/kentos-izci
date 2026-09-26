# izci.ingest.v1

> Generated from `izci/ingest/v1/event.proto`, `izci/ingest/v1/ingest.proto` — do not edit by hand.

Package izci.ingest.v1 is the public contract applications use to report
errors and messages to izci.

The central type is Event: one occurrence of an error (or a notable message)
captured by an application, together with everything needed to understand
it — the exception chain with stack traces, breadcrumbs that led up to it, the
HTTP request being served, the affected user, customer and tenant, and the
state of the device, runtime and process (CPU, memory, …).

izci groups events that share a fingerprint into issues, counts how often
each issue repeats, and notifies developers.

Conventions used throughout this package:

  - Every field is optional unless documented otherwise. Leave a field unset
    rather than sending placeholder values such as "unknown" or "N/A".
  - Strings are UTF-8. Oversized values are truncated by the server (never
    rejected); the documented limits are the maximum sizes that are kept.
  - Byte counts are in bytes, durations in milliseconds unless the type is
    google.protobuf.Duration, percentages are 0–100.
  - Stack frames are ordered from the OLDEST call to the NEWEST call: the
    frame that raised the error is the LAST element (same as Sentry).
  - Chained exceptions are ordered from the ROOT CAUSE to the exception that
    was finally caught: the primary exception is the LAST element.
  - Sensitive data (passwords, tokens, cookies, card numbers, Turkish ID
    numbers, …) is scrubbed again on the server, but SDKs should avoid
    sending it at all.

## Contents

**Services:** [IngestService](#izci-ingest-v1-ingestservice)

**Messages:** [AppContext](#izci-ingest-v1-appcontext) · [Breadcrumb](#izci-ingest-v1-breadcrumb) · [BrowserContext](#izci-ingest-v1-browsercontext) · [CloudContext](#izci-ingest-v1-cloudcontext) · [Contexts](#izci-ingest-v1-contexts) · [CpuInfo](#izci-ingest-v1-cpuinfo) · [CultureContext](#izci-ingest-v1-culturecontext) · [Customer](#izci-ingest-v1-customer) · [DatabaseContext](#izci-ingest-v1-databasecontext) · [DeviceContext](#izci-ingest-v1-devicecontext) · [Event](#izci-ingest-v1-event) · [EventResult](#izci-ingest-v1-eventresult) · [Exception](#izci-ingest-v1-exception) · [Frame](#izci-ingest-v1-frame) · [Geo](#izci-ingest-v1-geo) · [GetClientConfigRequest](#izci-ingest-v1-getclientconfigrequest) · [GetClientConfigResponse](#izci-ingest-v1-getclientconfigresponse) · [GpuContext](#izci-ingest-v1-gpucontext) · [Header](#izci-ingest-v1-header) · [Mechanism](#izci-ingest-v1-mechanism) · [MemoryInfo](#izci-ingest-v1-memoryinfo) · [Message](#izci-ingest-v1-message) · [OsContext](#izci-ingest-v1-oscontext) · [ProcessContext](#izci-ingest-v1-processcontext) · [RegisterReleaseRequest](#izci-ingest-v1-registerreleaserequest) · [RegisterReleaseResponse](#izci-ingest-v1-registerreleaseresponse) · [Request](#izci-ingest-v1-request) · [Response](#izci-ingest-v1-response) · [RuntimeContext](#izci-ingest-v1-runtimecontext) · [ScreenInfo](#izci-ingest-v1-screeninfo) · [Sdk](#izci-ingest-v1-sdk) · [SdkPackage](#izci-ingest-v1-sdkpackage) · [SendEventRequest](#izci-ingest-v1-sendeventrequest) · [SendEventResponse](#izci-ingest-v1-sendeventresponse) · [SendEventsRequest](#izci-ingest-v1-sendeventsrequest) · [SendEventsResponse](#izci-ingest-v1-sendeventsresponse) · [Stacktrace](#izci-ingest-v1-stacktrace) · [StorageInfo](#izci-ingest-v1-storageinfo) · [Tenant](#izci-ingest-v1-tenant) · [Thread](#izci-ingest-v1-thread) · [TraceContext](#izci-ingest-v1-tracecontext) · [User](#izci-ingest-v1-user)

**Enums:** [EventOutcome](#izci-ingest-v1-eventoutcome) · [Level](#izci-ingest-v1-level)

## Services

<a id="izci-ingest-v1-ingestservice"></a>

### IngestService

IngestService receives events and release metadata from applications.

**Transports**

The service is served by connect-go on the izci listener, so one endpoint
speaks three protocols:

  - native gRPC over HTTP/2 (TLS, or cleartext h2c with prior knowledge),
    for server-side applications (Go, .NET, Java, Rust, Dart, …);
  - gRPC-Web, for browsers through gRPC-Web clients;
  - the Connect protocol over HTTP/1.1 or HTTP/2 with binary or JSON
    bodies, which also makes the service reachable with plain HTTP:

      curl -X POST https://izci.example.com/izci.ingest.v1.IngestService/SendEvent \
        -H 'Content-Type: application/json' \
        -H 'Authorization: Bearer izk_…' \
        -d '{"event":{"level":"LEVEL_ERROR","exceptions":[{"type":"TimeoutError","value":"upstream timed out"}]}}'

All methods are unary. Requests may be compressed with gzip
(grpc-encoding / Content-Encoding: gzip).

**Authentication**

Every call carries a project API key (prefix "izk_") in one of these
headers (gRPC metadata keys):

  - authorization: Bearer izk_…
  - x-izci-key: izk_…

A key belongs to exactly one project, which determines where events are
stored. Keys used from browsers should list the allowed origins in izci;
requests from other origins are rejected.

**Errors**

| Code               | Meaning                                              | Retry?                 |
|--------------------|------------------------------------------------------|------------------------|
| UNAUTHENTICATED    | missing, malformed, unknown or revoked key           | no                     |
| PERMISSION_DENIED  | key lacks the ingest scope or the origin is not allowed | no                  |
| INVALID_ARGUMENT   | request cannot be decoded or has no usable content   | no                     |
| RESOURCE_EXHAUSTED | rate limit or monthly quota exceeded                 | yes, after RetryInfo   |
| UNAVAILABLE        | server overloaded or temporarily unable to persist   | yes, with backoff      |

RESOURCE_EXHAUSTED errors carry a google.rpc.RetryInfo detail with the
delay after which the client may retry. SDKs must honor it and should drop
(not queue indefinitely) events while rate limited.

**Delivery semantics**

By default the server answers after the event was durably stored, and the
response names the issue it was grouped into. Retrying with the same
Event.event_id is safe: duplicates are acknowledged with
EVENT_OUTCOME_DUPLICATE and not stored twice.

| Method | Request | Response | Summary |
|---|---|---|---|
| [SendEvent](#izci-ingest-v1-ingestservice-sendevent) | [SendEventRequest](#izci-ingest-v1-sendeventrequest) | [SendEventResponse](#izci-ingest-v1-sendeventresponse) | SendEvent stores one event and returns the issue it was grouped into. |
| [SendEvents](#izci-ingest-v1-ingestservice-sendevents) | [SendEventsRequest](#izci-ingest-v1-sendeventsrequest) | [SendEventsResponse](#izci-ingest-v1-sendeventsresponse) | SendEvents stores up to 100 events in one call. |
| [RegisterRelease](#izci-ingest-v1-ingestservice-registerrelease) | [RegisterReleaseRequest](#izci-ingest-v1-registerreleaserequest) | [RegisterReleaseResponse](#izci-ingest-v1-registerreleaseresponse) | RegisterRelease records metadata about a release (commit, repository, deployment), typically called from CI after a build or deploy. |
| [GetClientConfig](#izci-ingest-v1-ingestservice-getclientconfig) | [GetClientConfigRequest](#izci-ingest-v1-getclientconfigrequest) | [GetClientConfigResponse](#izci-ingest-v1-getclientconfigresponse) | GetClientConfig returns project settings an SDK should apply locally: sampling, limits and extra fields to scrub. |

<a id="izci-ingest-v1-ingestservice-sendevent"></a>

#### IngestService.SendEvent

`rpc SendEvent(SendEventRequest) returns (SendEventResponse)`

SendEvent stores one event and returns the issue it was grouped into.

Oversized fields are truncated rather than rejected; the result lists
what was trimmed in warnings.

<a id="izci-ingest-v1-ingestservice-sendevents"></a>

#### IngestService.SendEvents

`rpc SendEvents(SendEventsRequest) returns (SendEventsResponse)`

SendEvents stores up to 100 events in one call. Results are returned in
request order. A failing event does not fail the batch: its result carries
the outcome and error instead. Rate limiting may apply to part of a batch.

<a id="izci-ingest-v1-ingestservice-registerrelease"></a>

#### IngestService.RegisterRelease

`rpc RegisterRelease(RegisterReleaseRequest) returns (RegisterReleaseResponse)`

RegisterRelease records metadata about a release (commit, repository,
deployment), typically called from CI after a build or deploy. izci uses
it to link issues to commits, to check out the exact code for AI
analysis and to detect regressions in new releases. Calling it again for
the same version updates the stored metadata.

<a id="izci-ingest-v1-ingestservice-getclientconfig"></a>

#### IngestService.GetClientConfig

`rpc GetClientConfig(GetClientConfigRequest) returns (GetClientConfigResponse)`

GetClientConfig returns project settings an SDK should apply locally:
sampling, limits and extra fields to scrub. SDKs call it at startup and
may cache the result for cache_ttl.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

## Messages

<a id="izci-ingest-v1-appcontext"></a>

### AppContext

AppContext describes the application.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Application identifier, e.g. bundle id "tr.gov.sivas.mobil". |
| `name` | `string` | Human-readable application name, e.g. "KentOS Kalem". |
| `version` | `string` | User-visible version, e.g. "1.4.2". |
| `build` | `string` | Build number, e.g. "241". |
| `start_time` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the application instance started. |
| `in_foreground` | optional `bool` | Whether the application was in the foreground (mobile/desktop). |
| `memory_bytes` | `uint64` | Memory used by the application in bytes. |

<a id="izci-ingest-v1-breadcrumb"></a>

### Breadcrumb

Breadcrumb is an event that happened before the reported event.

| Field | Type | Description |
|---|---|---|
| `timestamp` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the breadcrumb was recorded. |
| `type` | `string` | Kind of breadcrumb, which selects the icon and rendering in the UI: "default", "debug", "info", "error", "http", "navigation", "query" (SQL or other database call), "ui" (click, input), "user" (user action), "transaction". Unknown values render as "default". |
| `category` | `string` | Dotted category, e.g. "http.client", "db.sql", "ui.click", "auth", "console". Max 128 characters. |
| `level` | [Level](#izci-ingest-v1-level) | Severity of the breadcrumb. |
| `message` | `string` | Human-readable description, e.g. "GET https://api.example.com/users -> 200" or the SQL statement. Max 8 KiB. |
| `data` | [google.protobuf.Struct](https://protobuf.dev/reference/protobuf/google.protobuf/#struct) | Structured data. Conventional keys by type: http → "method", "url", "status_code", "duration_ms"; query → "db.system", "db.statement", "duration_ms", "rows"; navigation → "from", "to". Max 8 KiB once encoded as JSON. |

<a id="izci-ingest-v1-browsercontext"></a>

### BrowserContext

BrowserContext describes the web browser.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Browser name, e.g. "Chrome", "Firefox", "Safari", "Edge". |
| `version` | `string` | Browser version, e.g. "129.0.6668.58". |
| `user_agent` | `string` | Raw User-Agent string, when it is not sent as a request header. The server parses it into browser, os and device information. |

<a id="izci-ingest-v1-cloudcontext"></a>

### CloudContext

CloudContext describes the hosting environment.

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Provider, e.g. "aws", "azure", "gcp", "hetzner", "on-premise". |
| `region` | `string` | Region, e.g. "eu-central-1". |
| `availability_zone` | `string` | Availability zone, e.g. "eu-central-1a". |
| `account_id` | `string` | Account or subscription id. |
| `instance_id` | `string` | Virtual machine or instance id. |
| `platform` | `string` | Orchestrator or platform, e.g. "kubernetes", "docker", "dokploy", "iis". |
| `cluster` | `string` | Cluster name. |
| `namespace` | `string` | Kubernetes namespace. |
| `pod` | `string` | Pod name. |
| `container` | `string` | Container name or id. |
| `node` | `string` | Node (host) name. |

<a id="izci-ingest-v1-contexts"></a>

### Contexts

Contexts groups structured, well-known context blocks.

All blocks are optional; send what the platform can observe cheaply.
Server-side SDKs should at least send runtime, os, process (CPU and memory)
and device.memory. Browser SDKs usually rely on the server parsing the
User-Agent header into browser, os and device.

| Field | Type | Description |
|---|---|---|
| `device` | [DeviceContext](#izci-ingest-v1-devicecontext) | Physical or virtual device: model, CPU and memory state, storage, screen. |
| `os` | [OsContext](#izci-ingest-v1-oscontext) | Operating system. |
| `runtime` | [RuntimeContext](#izci-ingest-v1-runtimecontext) | Language runtime, e.g. Go, .NET, Node.js, Dart VM. |
| `browser` | [BrowserContext](#izci-ingest-v1-browsercontext) | Web browser (browser applications). |
| `app` | [AppContext](#izci-ingest-v1-appcontext) | Application metadata: name, version, start time, memory usage. |
| `process` | [ProcessContext](#izci-ingest-v1-processcontext) | The reporting process: pid, CPU usage, memory, threads, garbage collection. |
| `gpu` | [GpuContext](#izci-ingest-v1-gpucontext) | Graphics processor (mobile and desktop applications). |
| `culture` | [CultureContext](#izci-ingest-v1-culturecontext) | Locale and timezone settings. |
| `trace` | [TraceContext](#izci-ingest-v1-tracecontext) | Distributed tracing identifiers linking the event to a trace. |
| `database` | [DatabaseContext](#izci-ingest-v1-databasecontext) | Database operation related to the event (e.g. the failing SQL query). |
| `cloud` | [CloudContext](#izci-ingest-v1-cloudcontext) | Cloud, container or orchestrator environment. |
| `custom` | map&lt;`string`, [google.protobuf.Struct](https://protobuf.dev/reference/protobuf/google.protobuf/#struct)&gt; | Additional named context blocks, e.g. {"payment": {"provider": "iyzico", "attempt": 2}}. Max 20 blocks, 8 KiB each once encoded as JSON. |

<a id="izci-ingest-v1-cpuinfo"></a>

### CpuInfo

CpuInfo describes the processors and their load.

| Field | Type | Description |
|---|---|---|
| `count` | `uint32` | Number of logical CPUs available to the process (after container limits when known), e.g. runtime.NumCPU() in Go. |
| `physical_count` | `uint32` | Number of physical cores, when known. |
| `model` | `string` | Processor model, e.g. "Apple M3 Pro", "Intel(R) Xeon(R) Gold 6338". |
| `frequency_mhz` | `double` | Nominal clock frequency in MHz. |
| `usage_percent` | optional `double` | System-wide CPU utilization in percent (0–100) sampled around the time of the event. |
| `load_average` | repeated `double` | Load averages over 1, 5 and 15 minutes (Unix-like systems), in that order. |

<a id="izci-ingest-v1-culturecontext"></a>

### CultureContext

CultureContext describes locale settings.

| Field | Type | Description |
|---|---|---|
| `locale` | `string` | BCP 47 locale, e.g. "tr-TR". |
| `timezone` | `string` | IANA timezone, e.g. "Europe/Istanbul". |
| `calendar` | `string` | Calendar system, e.g. "gregorian". |
| `is_24_hour_format` | optional `bool` | Whether the user prefers a 24-hour clock. |

<a id="izci-ingest-v1-customer"></a>

### Customer

Customer identifies the organization on whose behalf the application was
running (for KentOS applications: the municipality or institution).

Customers are shared across all projects of an izci organization: the same
customer id reported by two applications refers to the same customer.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Stable identifier of the customer, e.g. "sivas-belediyesi" or "C-10042". Used for grouping and filtering, so it must not change over time. Compared case-insensitively. Max 64 characters. |
| `name` | `string` | Display name, e.g. "Sivas Belediyesi". The most recently reported name wins and can be edited in the izci UI. Max 200 characters. |
| `attributes` | map&lt;`string`, `string`&gt; | Additional descriptive attributes, e.g. {"plan": "premium", "region": "iç-anadolu"}. Max 20 entries; keys max 64, values max 200 characters. |

<a id="izci-ingest-v1-databasecontext"></a>

### DatabaseContext

DatabaseContext describes a database operation related to the event —
typically the query that failed.

| Field | Type | Description |
|---|---|---|
| `system` | `string` | Database system: "postgresql", "mssql", "oracle", "mysql", "sqlite", "mongodb", "redis", … |
| `name` | `string` | Database (catalog) name. |
| `statement` | `string` | Statement text, e.g. "SELECT * FROM orders WHERE id = $1". Prefer parameterized statements; literals are scrubbed for known patterns but the SDK should not inline sensitive values. Max 8 KiB. |
| `operation` | `string` | Operation, e.g. "SELECT", "INSERT", "findOne". |
| `duration_ms` | `double` | Duration of the operation in milliseconds. |
| `parameters` | repeated `string` | Bound parameter values as text, in order. Scrubbed by the server; send only when useful and safe. Max 50 values of 1 KiB. |
| `host` | `string` | Host (and port) of the database server, e.g. "db-primary:5432". |
| `rows` | optional `int64` | Number of rows returned or affected, when known. |
| `error_code` | `string` | Database-specific error code, e.g. PostgreSQL SQLSTATE "23505" or SQL Server error number "2627". |

<a id="izci-ingest-v1-devicecontext"></a>

### DeviceContext

DeviceContext describes the device the application runs on.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of the device, e.g. "Pixel 8", "MacBook Pro" or the host name. |
| `family` | `string` | Device family, e.g. "iPhone", "Android", "Desktop", "Server". |
| `model` | `string` | Model name, e.g. "iPhone15,2", "SM-S918B". |
| `model_id` | `string` | Internal model identifier. |
| `manufacturer` | `string` | Manufacturer, e.g. "Samsung", "Apple", "Dell". |
| `brand` | `string` | Brand, e.g. "Galaxy". |
| `arch` | `string` | CPU architecture, e.g. "amd64", "arm64", "x86". |
| `device_type` | `string` | Form factor: "desktop", "mobile", "tablet", "server", "tv", "embedded". |
| `simulator` | `bool` | True when running in a simulator or emulator. |
| `orientation` | `string` | Screen orientation: "portrait" or "landscape". |
| `battery_level` | optional `float` | Battery level, 0–100. |
| `charging` | optional `bool` | Whether the device is charging. |
| `online` | optional `bool` | Whether the device had network connectivity. |
| `cpu` | [CpuInfo](#izci-ingest-v1-cpuinfo) | CPU information and load. |
| `memory` | [MemoryInfo](#izci-ingest-v1-memoryinfo) | System memory state. |
| `storage` | [StorageInfo](#izci-ingest-v1-storageinfo) | Storage state of the main volume. |
| `screen` | [ScreenInfo](#izci-ingest-v1-screeninfo) | Primary screen. |
| `boot_time` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the device booted. |

<a id="izci-ingest-v1-event"></a>

### Event

Event is a single occurrence of an error or message reported by an
application.

The smallest useful event carries a timestamp, a level and either one
exception (type + value) or a message. Everything else enriches the event and
makes it easier to diagnose.

Sentry equivalent: the error event payload.

| Field | Type | Description |
|---|---|---|
| `event_id` | `string` | Client-generated unique identifier of this event: a UUID in canonical textual form (e.g. "6f1c1b5e-2c3d-4f6a-9b8e-0d1e2f3a4b5c"). UUIDv7 or UUIDv4 is recommended.<br><br>The id is the idempotency key: when an SDK retries a request after a timeout, events whose event_id was already stored for the project are acknowledged as duplicates and not stored twice. When empty, the server assigns a random UUID (and retries can then create duplicates).<br><br>Applications may show this id to end users ("error code") so that support staff can find the exact event later. |
| `timestamp` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Moment the event happened, measured by the client clock.<br><br>When unset, the server uses the time it received the event. Timestamps more than 5 minutes in the future or older than the project's retention window are replaced by the receive time (the original value is kept in the event warnings). |
| `level` | [Level](#izci-ingest-v1-level) | Severity of the event. Defaults to ERROR for events with exceptions and INFO for message-only events. |
| `platform` | `string` | Technology that produced the event, used to pick stack-trace heuristics and syntax highlighting. Well-known values: "go", "dotnet", "javascript" (browser), "node", "dart" (Flutter), "python", "java", "kotlin", "swift", "php", "rust", "ruby", "elixir", "native", "other". Max 32 characters. |
| `logger` | `string` | Name of the logger that produced the event, e.g. "KentOS.Kalem.Services.OrderService" or "app.billing". Max 128 characters. |
| `transaction` | `string` | Name of the operation in progress when the event happened: an HTTP route ("GET /api/orders/{id}"), a job name ("nightly-sync"), a gRPC method ("/izci.api.v1.IssueService/ListIssues") or a UI screen ("CheckoutPage"). Prefer route templates over concrete URLs so that events aggregate well. Max 256 characters. |
| `server_name` | `string` | Host name of the machine, container or pod that produced the event, e.g. "api-7d9c8f-x2kqp". Max 128 characters. |
| `release` | `string` | Version of the application, e.g. "1.4.2", "kalem@1.4.2+241" or a git commit SHA. Releases let izci tell which version introduced an issue and detect regressions after an issue was resolved. Register richer metadata (commit, repository) with IngestService.RegisterRelease. Max 200 characters. |
| `dist` | `string` | Build variant of the release (e.g. "arm64", "google-play"). Only meaningful together with release. Max 64 characters. |
| `environment` | `string` | Deployment environment, e.g. "production", "staging", "development". Lower-cased by the server. Defaults to "production" when unset. Max 64 characters. |
| `customer` | [Customer](#izci-ingest-v1-customer) | Customer (organization using the application) on whose behalf the application was running — for KentOS typically a municipality. izci lets you filter and alert per customer and counts how many customers an issue affects. |
| `tenant` | [Tenant](#izci-ingest-v1-tenant) | Tenant inside the application, for multi-tenant applications that host several logical tenants in one deployment. Independent of customer: a customer may own several tenants. |
| `message` | [Message](#izci-ingest-v1-message) | Human-readable message. Required when the event has no exceptions; for events with exceptions it adds context (e.g. the log line that reported the error). |
| `exceptions` | repeated [Exception](#izci-ingest-v1-exception) | Exception chain, ordered from the ROOT CAUSE to the PRIMARY exception (the one finally caught or reported), so the last element is the most relevant one. A single exception is a one-element list. Max 10 exceptions; when longer, the first and last ones are kept.<br><br>Sentry equivalent: exception.values (same ordering). |
| `threads` | repeated [Thread](#izci-ingest-v1-thread) | Threads (or goroutines, isolates) of the process at the time of the event, for crash reports. Usually only the crashed thread carries a stack trace. Max 64 threads. |
| `breadcrumbs` | repeated [Breadcrumb](#izci-ingest-v1-breadcrumb) | Trail of events that happened before this event (log lines, HTTP calls, SQL queries, UI clicks, navigations), OLDEST first. Max 100 breadcrumbs; when more are sent the most recent 100 are kept. |
| `request` | [Request](#izci-ingest-v1-request) | HTTP (or RPC) request that was being handled when the event occurred. |
| `response` | [Response](#izci-ingest-v1-response) | Response that was sent (or was about to be sent) for request. |
| `user` | [User](#izci-ingest-v1-user) | End user affected by the event. |
| `contexts` | [Contexts](#izci-ingest-v1-contexts) | Structured information about the device, operating system, runtime, browser, application, process (CPU and memory), culture, trace, database query and cloud environment. |
| `tags` | map&lt;`string`, `string`&gt; | Indexed key/value pairs used for search, filtering and the tag distribution charts of an issue, e.g. {"module": "billing", "payment_method": "card"}.<br><br>Keys: max 64 characters, [A-Za-z0-9_.:-]. Values: max 200 characters. Max 50 tags. Keys starting with "izci." are reserved. Prefer low-cardinality values; put unique values such as order ids into extra. |
| `extra` | [google.protobuf.Struct](https://protobuf.dev/reference/protobuf/google.protobuf/#struct) | Arbitrary additional data that is shown with the event but not indexed, e.g. {"order_id": 91512, "cart": {"items": 3}}. Max 64 KiB once encoded as JSON; larger payloads are dropped with a warning. |
| `fingerprint` | repeated `string` | Custom grouping fingerprint. When set, events with an identical fingerprint are grouped into the same issue regardless of their stack traces. The special element "{{ default }}" stands for the fingerprint izci would have computed, so ["{{ default }}", "tenant-a"] splits the default group per tenant. Max 16 elements of 256 characters each.<br><br>Sentry equivalent: fingerprint. |
| `sdk` | [Sdk](#izci-ingest-v1-sdk) | SDK that produced the event. |
| `modules` | map&lt;`string`, `string`&gt; | Versions of the libraries loaded by the application, name → version, e.g. {"github.com/jackc/pgx/v5": "v5.11.0"}. Helps to spot issues caused by a dependency upgrade. Max 500 entries. |

<a id="izci-ingest-v1-eventresult"></a>

### EventResult

EventResult reports the outcome of one event.

| Field | Type | Description |
|---|---|---|
| `event_id` | `string` | Event id as stored: the client-provided Event.event_id or the id the server assigned when it was empty. |
| `outcome` | [EventOutcome](#izci-ingest-v1-eventoutcome) | What happened to the event. |
| `issue_id` | `string` | Identifier (UUID) of the issue the event was grouped into. Empty unless outcome is ACCEPTED or DUPLICATE. |
| `issue_short_id` | `string` | Short, human-friendly issue identifier, e.g. "KALEM-1A". Empty unless outcome is ACCEPTED or DUPLICATE. |
| `issue_url` | `string` | Link to the issue in the izci web UI. Empty unless outcome is ACCEPTED or DUPLICATE. |
| `issue_is_new` | `bool` | True when this event created a new issue. |
| `issue_regressed` | `bool` | True when this event reopened a resolved issue (regression). |
| `error` | `string` | Why the event was not accepted, for outcomes other than ACCEPTED and DUPLICATE, e.g. "rate limit exceeded" or "event has neither a message nor an exception". |
| `warnings` | repeated `string` | Non-fatal adjustments the server made, e.g. "exceptions[0].stacktrace: 412 frames trimmed to 250" or "timestamp in the future replaced by receive time". |

<a id="izci-ingest-v1-exception"></a>

### Exception

Exception is one error in an exception chain.

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Type (class) of the error, e.g. "System.NullReferenceException", "TypeError", "*fs.PathError", "PSQLException". The most important grouping input together with the stack trace. Max 256 characters. |
| `value` | `string` | Error message, e.g. "Object reference not set to an instance of an object." Max 8 KiB. |
| `module` | `string` | Namespace, package or module that defines the type, e.g. "System" or "github.com/lib/pq". Max 256 characters. |
| `thread_id` | `string` | Identifier of the thread that raised the exception; matches Thread.id. |
| `mechanism` | [Mechanism](#izci-ingest-v1-mechanism) | How the exception was captured (handled or not, by which mechanism). |
| `stacktrace` | [Stacktrace](#izci-ingest-v1-stacktrace) | Stack trace at the point the exception was raised. |

<a id="izci-ingest-v1-frame"></a>

### Frame

Frame is a single stack frame.

Provide at least a function or a filename. Frames of the application's own
code should be marked in_app = true; they are highlighted in the UI and are
the only frames used for grouping when present.

| Field | Type | Description |
|---|---|---|
| `function` | `string` | Function or method name, e.g. "OrderService.CreateOrder", "(*Server).handleRequest" or "handleSubmit". Max 256 characters. |
| `raw_function` | `string` | Original (mangled or minified) function name before symbolication or demangling. Max 256 characters. |
| `module` | `string` | Module, namespace or class the function belongs to, e.g. "KentOS.Kalem.Services" or "github.com/mcihad/app/internal/orders". Max 256 characters. |
| `package` | `string` | Library or assembly that contains the frame, e.g. "KentOS.Kalem.Application.dll" or "libsystem_kernel.dylib". Max 256 characters. |
| `filename` | `string` | Path of the source file relative to the project root when known, e.g. "src/Services/OrderService.cs". Used for display, grouping and linking to the repository. Max 512 characters. |
| `abs_path` | `string` | Absolute path or URL of the source file on the machine that produced the event, e.g. "/app/src/Services/OrderService.cs" or "https://app.example.com/assets/index-3f2a1c.js". Max 1 KiB. |
| `lineno` | `uint32` | 1-based line number. 0 means unknown. |
| `colno` | `uint32` | 1-based column number. 0 means unknown. |
| `context_line` | `string` | Source line at lineno, without the trailing newline. Max 1 KiB. |
| `pre_context` | repeated `string` | Up to 5 source lines immediately before context_line, oldest first. |
| `post_context` | repeated `string` | Up to 5 source lines immediately after context_line. |
| `in_app` | optional `bool` | Whether the frame belongs to the application's own code (as opposed to a framework, the standard library or a dependency). Unset lets the server decide using the project's in-app rules and platform heuristics (e.g. node_modules, vendor, System.* are not in-app). |
| `vars` | map&lt;`string`, `string`&gt; | Local variables at the time of the error, name → printed value. Values are scrubbed by the server like any other data. Max 50 entries of 1 KiB. |
| `instruction_addr` | `string` | Instruction address for native frames, as hexadecimal, e.g. "0x0000000100a4c2f8". |
| `symbol` | `string` | Symbol name for native frames when different from function. |
| `platform` | `string` | Platform of this frame when it differs from Event.platform (mixed stacks, e.g. native frames under a Flutter app). |

<a id="izci-ingest-v1-geo"></a>

### Geo

Geo is an approximate geographic location.

| Field | Type | Description |
|---|---|---|
| `country_code` | `string` | ISO 3166-1 alpha-2 country code, e.g. "TR". |
| `region` | `string` | Region or province, e.g. "Sivas". |
| `city` | `string` | City or district, e.g. "Merkez". |

<a id="izci-ingest-v1-getclientconfigrequest"></a>

### GetClientConfigRequest

GetClientConfigRequest identifies the calling SDK.

| Field | Type | Description |
|---|---|---|
| `sdk` | [Sdk](#izci-ingest-v1-sdk) | The calling SDK, for compatibility decisions and statistics. |
| `environment` | `string` | Environment the SDK runs in, e.g. "production". |

<a id="izci-ingest-v1-getclientconfigresponse"></a>

### GetClientConfigResponse

GetClientConfigResponse holds settings an SDK should apply.

| Field | Type | Description |
|---|---|---|
| `organization_slug` | `string` | Organization slug of the project, e.g. "kentos". |
| `project_slug` | `string` | Project slug, e.g. "kalem". |
| `project_name` | `string` | Project display name, e.g. "KentOS Kalem". |
| `sample_rate` | `double` | Fraction of events the SDK should send, 0.0–1.0 (1.0 sends everything). |
| `max_breadcrumbs` | `uint32` | Maximum number of breadcrumbs to keep per event. |
| `max_event_bytes` | `uint32` | Maximum encoded size of one event in bytes; larger events are trimmed by the server. |
| `max_batch_size` | `uint32` | Maximum number of events per SendEvents call. |
| `max_events_per_minute` | `uint32` | Suggested upper bound of events per minute for this SDK instance. 0 means no specific limit. |
| `scrub_fields` | repeated `string` | Additional field names (case-insensitive substrings) the SDK should scrub before sending, on top of the built-in list (password, secret, token, authorization, cookie, …). |
| `collect_ip` | `bool` | Whether the SDK should send the end user's IP address (User.ip_address). |
| `server_time` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Current server time, for clock skew detection. |
| `cache_ttl_seconds` | `uint32` | How long (in seconds) the SDK may cache this configuration. |

<a id="izci-ingest-v1-gpucontext"></a>

### GpuContext

GpuContext describes the graphics processor.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | GPU name, e.g. "Apple M3 Pro", "Adreno 740". |
| `vendor_name` | `string` | Vendor, e.g. "Apple", "Qualcomm", "NVIDIA". |
| `memory_bytes` | `uint64` | Video memory in bytes. |
| `api_type` | `string` | Graphics API, e.g. "Metal", "Vulkan", "WebGL 2". |
| `driver_version` | `string` | Driver version. |

<a id="izci-ingest-v1-header"></a>

### Header

Header is one HTTP header.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Header name as received, e.g. "User-Agent". Compared case-insensitively. |
| `value` | `string` | Header value. Max 2 KiB. |

<a id="izci-ingest-v1-mechanism"></a>

### Mechanism

Mechanism describes how an exception was captured.

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Capture mechanism, e.g. "generic" (captured manually), "panic" (Go), "onerror" and "onunhandledrejection" (browser), "middleware" (HTTP middleware), "logger" (captured from a log call), "AppDomain.UnhandledException" (.NET), "FlutterError" (Flutter). Max 64 characters. |
| `handled` | optional `bool` | Whether the application handled the exception. False means the exception crashed the process or the request (unhandled); such events are highlighted and default to at least ERROR level. Unset means unknown. |
| `synthetic` | `bool` | True when the exception type was synthesized by the SDK (e.g. a non-exception value was thrown in JavaScript), which tells izci to ignore the type for grouping. |
| `description` | `string` | Free-form description of the mechanism. Max 1 KiB. |
| `help_link` | `string` | Link to documentation about this kind of error. Max 1 KiB. |
| `data` | map&lt;`string`, `string`&gt; | Mechanism-specific data, e.g. {"signal": "SIGSEGV"}. Max 20 entries. |
| `exception_id` | `int32` | Identifier of this exception inside an exception group (e.g. Python ExceptionGroup, .NET AggregateException). Unique within the event. |
| `parent_id` | optional `int32` | exception_id of the parent exception inside an exception group. |
| `is_exception_group` | `bool` | True when this exception is a group that aggregates child exceptions. |
| `source` | `string` | Attribute of the parent through which this exception was found, e.g. "__cause__", "InnerException" or "exceptions[2]". |

<a id="izci-ingest-v1-memoryinfo"></a>

### MemoryInfo

MemoryInfo describes system memory.

| Field | Type | Description |
|---|---|---|
| `total_bytes` | `uint64` | Total physical memory in bytes (container limit when known). |
| `free_bytes` | `uint64` | Free (unused) memory in bytes. |
| `available_bytes` | `uint64` | Memory available to new allocations without swapping, in bytes (Linux MemAvailable). Usually larger than free_bytes. |
| `used_bytes` | `uint64` | Used memory in bytes. |
| `used_percent` | optional `double` | Used memory in percent (0–100). |
| `swap_total_bytes` | `uint64` | Total swap space in bytes. |
| `swap_free_bytes` | `uint64` | Free swap space in bytes. |
| `low_memory` | `bool` | True when the operating system reported a low-memory condition. |

<a id="izci-ingest-v1-message"></a>

### Message

Message is a human-readable description of the event.

| Field | Type | Description |
|---|---|---|
| `formatted` | `string` | Final text as shown to humans, e.g. "User 42 not found". Max 8 KiB. |
| `template` | `string` | Parameterized template the text was produced from, e.g. "User %s not found" or "User {UserId} not found". When present, izci groups message-only events by template instead of by formatted text, so "User 42 not found" and "User 43 not found" become one issue. Max 1 KiB. |
| `params` | repeated `string` | Values substituted into template, in order. Max 32 values of 1 KiB. |

<a id="izci-ingest-v1-oscontext"></a>

### OsContext

OsContext describes the operating system.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name, e.g. "Linux", "Windows", "macOS", "Android", "iOS". |
| `version` | `string` | Version, e.g. "24.04", "10.0.26100", "15.3". |
| `build` | `string` | Build identifier, e.g. "24D60". |
| `kernel_version` | `string` | Kernel version, e.g. "6.8.0-45-generic". |
| `rooted` | optional `bool` | Whether the device is rooted or jailbroken. |
| `raw_description` | `string` | Unparsed description when name and version are not available separately, e.g. "Ubuntu 24.04.1 LTS". |
| `distribution` | `string` | Distribution for Linux, e.g. "ubuntu", "alpine", "debian". |

<a id="izci-ingest-v1-processcontext"></a>

### ProcessContext

ProcessContext describes the reporting process and its resource usage.

| Field | Type | Description |
|---|---|---|
| `pid` | `uint32` | Operating system process id. |
| `name` | `string` | Executable name, e.g. "izci", "dotnet", "node". |
| `command_line` | `string` | Command line. Scrubbed by the server; avoid sending secrets passed as arguments. Max 2 KiB. |
| `start_time` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the process started. |
| `uptime` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Time since the process started. |
| `cpu_percent` | optional `double` | CPU used by this process in percent of one core over the recent sampling interval (can exceed 100 on multi-core systems). |
| `rss_bytes` | `uint64` | Resident set size (physical memory) in bytes. |
| `heap_alloc_bytes` | `uint64` | Bytes of allocated heap objects (Go HeapAlloc, .NET GC heap size, V8 usedHeapSize). |
| `heap_sys_bytes` | `uint64` | Heap memory obtained from the operating system in bytes (Go HeapSys, V8 totalHeapSize). |
| `gc_count` | `uint32` | Number of completed garbage collection cycles. |
| `gc_pause_total` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Cumulative garbage collection pause time. |
| `threads` | `uint32` | Number of operating system threads. |
| `goroutines` | `uint32` | Number of goroutines (Go) or green threads. |
| `open_fds` | `uint32` | Number of open file descriptors or handles. |
| `cpu_time_user` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | User-mode CPU time consumed by the process. |
| `cpu_time_system` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Kernel-mode CPU time consumed by the process. |
| `working_directory` | `string` | Working directory of the process. |

<a id="izci-ingest-v1-registerreleaserequest"></a>

### RegisterReleaseRequest

RegisterReleaseRequest describes a release of the project's application.

| Field | Type | Description |
|---|---|---|
| `version` | `string` | Release version exactly as reported in Event.release, e.g. "1.4.2". Required. Max 200 characters. |
| `commit_sha` | `string` | Full git commit SHA the release was built from (40 hexadecimal characters). |
| `repository` | `string` | Repository of the commit, as "owner/name" or a URL, e.g. "mcihad/kentos-kalem". Defaults to the repository linked to the project. |
| `ref` | `string` | Branch or tag the release was built from, e.g. "main" or "v1.4.2". |
| `url` | `string` | Link to the release page or CI run. |
| `environment` | `string` | When set, also records a deployment of this release to the environment, e.g. "production". |
| `released_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the release was deployed (or built, when environment is empty). Defaults to the time of the call. |
| `released_by` | `string` | Who or what released it, e.g. "github-actions" or an email address. |

<a id="izci-ingest-v1-registerreleaseresponse"></a>

### RegisterReleaseResponse

RegisterReleaseResponse confirms the stored release.

| Field | Type | Description |
|---|---|---|
| `release_id` | `string` | Identifier (UUID) of the release in izci. |
| `created` | `bool` | True when the release did not exist before this call. |

<a id="izci-ingest-v1-request"></a>

### Request

Request describes the HTTP or RPC request being processed.

| Field | Type | Description |
|---|---|---|
| `method` | `string` | HTTP method in upper case, e.g. "GET", "POST". For gRPC use "POST". |
| `url` | `string` | Absolute URL of the request, e.g. "https://kalem.example.com/api/orders/42". The query string may be included here or sent separately in query_string. Max 4 KiB. |
| `route` | `string` | Route template that matched the request, e.g. "/api/orders/{id}". Makes requests to different ids comparable. Max 512 characters. |
| `query_string` | `string` | Raw query string without the leading "?", e.g. "page=2&sort=desc". Sensitive parameters (token, password, …) are scrubbed by the server. Max 4 KiB. |
| `headers` | repeated [Header](#izci-ingest-v1-header) | Request headers in the order they were received; repeated headers appear multiple times. Authorization, Cookie, Proxy-Authorization, X-Api-Key and similar credentials are always scrubbed by the server. Browser headers such as User-Agent, Accept-Language and Referer are parsed into the browser, os and device contexts. Max 100 headers; values max 2 KiB. |
| `cookies` | map&lt;`string`, `string`&gt; | Cookies, name → value. Values are scrubbed by default; only the names are kept unless the project allows cookie values. Max 50 cookies. |
| `body` | `string` | Request body as text (JSON, form data, …). Max 16 KiB; larger bodies are truncated. Sensitive fields are scrubbed by the server. |
| `body_size` | `int64` | Size of the full request body in bytes, even when body was truncated or omitted. |
| `protocol` | `string` | Protocol of the request, e.g. "HTTP/1.1", "HTTP/2", "gRPC". |
| `remote_addr` | `string` | Network address of the peer as seen by the application (may be a proxy), e.g. "10.0.4.17". The end user's address belongs in User.ip_address. |
| `env` | map&lt;`string`, `string`&gt; | Selected server environment variables for the request (CGI style), e.g. {"SERVER_NAME": "kalem.example.com"}. Max 50 entries. |
| `fragment` | `string` | URL fragment without the leading "#", for browser applications. |

<a id="izci-ingest-v1-response"></a>

### Response

Response describes the response to Request.

| Field | Type | Description |
|---|---|---|
| `status_code` | `uint32` | HTTP status code, e.g. 500. For gRPC, the HTTP status of the transport. |
| `headers` | repeated [Header](#izci-ingest-v1-header) | Response headers (same rules as Request.headers). |
| `body_size` | `int64` | Size of the response body in bytes. |

<a id="izci-ingest-v1-runtimecontext"></a>

### RuntimeContext

RuntimeContext describes the language runtime.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name, e.g. "go", ".NET", "node", "dart", "CPython", "JVM". |
| `version` | `string` | Version, e.g. "1.26.1", "10.0.0", "24.14.0". |
| `build` | `string` | Build or flavor, e.g. "release", "aot". |
| `raw_description` | `string` | Unparsed description, e.g. ".NET 10.0.0-rc.1". |

<a id="izci-ingest-v1-screeninfo"></a>

### ScreenInfo

ScreenInfo describes the primary screen.

| Field | Type | Description |
|---|---|---|
| `width_pixels` | `uint32` | Width in physical pixels. |
| `height_pixels` | `uint32` | Height in physical pixels. |
| `density` | `double` | Pixel density (device pixel ratio), e.g. 2.0 or 3.0. |
| `dpi` | `uint32` | Dots per inch. |

<a id="izci-ingest-v1-sdk"></a>

### Sdk

Sdk identifies the SDK that produced the event.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | SDK name, e.g. "izci.go", "sentry.dotnet", "sentry.javascript.browser". |
| `version` | `string` | SDK version, e.g. "0.3.0". |
| `integrations` | repeated `string` | Enabled integrations, e.g. ["http", "slog", "pgx"]. |
| `packages` | repeated [SdkPackage](#izci-ingest-v1-sdkpackage) | Packages that make up the SDK. |

<a id="izci-ingest-v1-sdkpackage"></a>

### SdkPackage

SdkPackage is one package of an SDK.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Package name, e.g. "go:github.com/mcihad/kentos-izci/sdk/go". |
| `version` | `string` | Package version. |

<a id="izci-ingest-v1-sendeventrequest"></a>

### SendEventRequest

SendEventRequest carries one event.

| Field | Type | Description |
|---|---|---|
| `event` | [Event](#izci-ingest-v1-event) | The event to store. Required. |

<a id="izci-ingest-v1-sendeventresponse"></a>

### SendEventResponse

SendEventResponse reports what happened to the event.

| Field | Type | Description |
|---|---|---|
| `result` | [EventResult](#izci-ingest-v1-eventresult) | Outcome of the event, including the issue it was grouped into. |

<a id="izci-ingest-v1-sendeventsrequest"></a>

### SendEventsRequest

SendEventsRequest carries a batch of events.

| Field | Type | Description |
|---|---|---|
| `events` | repeated [Event](#izci-ingest-v1-event) | Events to store, 1 to 100 per call. Keep a batch under 5 MiB. |

<a id="izci-ingest-v1-sendeventsresponse"></a>

### SendEventsResponse

SendEventsResponse reports the outcome of every event of the batch.

| Field | Type | Description |
|---|---|---|
| `results` | repeated [EventResult](#izci-ingest-v1-eventresult) | One result per request event, in request order. |

<a id="izci-ingest-v1-stacktrace"></a>

### Stacktrace

Stacktrace is a list of stack frames.

| Field | Type | Description |
|---|---|---|
| `frames` | repeated [Frame](#izci-ingest-v1-frame) | Frames ordered from the OLDEST call (e.g. main or the thread entry point) to the NEWEST call (the function that raised the error), so the crashing frame is the LAST element. Max 250 frames per stack trace; when longer, the oldest 50 and the newest 200 are kept. |
| `registers` | map&lt;`string`, `string`&gt; | CPU register values for native crashes, name → hexadecimal value, e.g. {"rip": "0x00007ff8a1b2c3d4"}. |

<a id="izci-ingest-v1-storageinfo"></a>

### StorageInfo

StorageInfo describes the main storage volume.

| Field | Type | Description |
|---|---|---|
| `total_bytes` | `uint64` | Total capacity in bytes. |
| `free_bytes` | `uint64` | Free space in bytes. |

<a id="izci-ingest-v1-tenant"></a>

### Tenant

Tenant identifies a logical tenant inside a multi-tenant application.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Stable identifier of the tenant within the application, e.g. "t-1932". Max 64 characters. |
| `name` | `string` | Display name of the tenant. Max 200 characters. |

<a id="izci-ingest-v1-thread"></a>

### Thread

Thread describes one thread (or goroutine, isolate) of the process.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Identifier of the thread, unique within the event, e.g. "1" or "main". |
| `name` | `string` | Name of the thread, e.g. "main" or "ThreadPool Worker #3". |
| `crashed` | `bool` | True for the thread that crashed or raised the reported exception. |
| `current` | `bool` | True for the thread that was running when the event was captured. |
| `main` | `bool` | True for the main (UI) thread. |
| `state` | `string` | Scheduler state, e.g. "Runnable", "Blocked", "Waiting". |
| `stacktrace` | [Stacktrace](#izci-ingest-v1-stacktrace) | Stack trace of the thread (same ordering as Exception.stacktrace). |

<a id="izci-ingest-v1-tracecontext"></a>

### TraceContext

TraceContext links the event to a distributed trace.

| Field | Type | Description |
|---|---|---|
| `trace_id` | `string` | W3C trace id: 32 lowercase hexadecimal characters. |
| `span_id` | `string` | Span id of the active span: 16 lowercase hexadecimal characters. |
| `parent_span_id` | `string` | Span id of the parent span. |
| `op` | `string` | Operation name of the active span, e.g. "http.server". |
| `status` | `string` | Status of the span, e.g. "ok", "internal_error". |

<a id="izci-ingest-v1-user"></a>

### User

User identifies the end user affected by the event.

Provide at least one of id, username, email or ip_address; izci counts
distinct users per issue using the first one that is present.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Stable identifier of the user in the application, e.g. "42" or a UUID. Max 128 characters. |
| `username` | `string` | Login name. Max 128 characters. |
| `email` | `string` | Email address. Max 256 characters. |
| `name` | `string` | Display name. Max 200 characters. |
| `ip_address` | `string` | IPv4 or IPv6 address of the user. The special value "{{auto}}" asks the server to use the client address of the ingest request (resolved through trusted proxies). Depending on project privacy settings the address is stored as is, anonymized (last octet removed) or not at all. |
| `segment` | `string` | Role, plan or group of the user, e.g. "admin", "citizen", "staff". Max 64 characters. |
| `attributes` | map&lt;`string`, `string`&gt; | Additional attributes, e.g. {"department": "imar"}. Max 20 entries. |
| `geo` | [Geo](#izci-ingest-v1-geo) | Geographic location of the user, when known to the application. |

## Enums

<a id="izci-ingest-v1-eventoutcome"></a>

### EventOutcome

EventOutcome is the fate of a submitted event.

| Name | Number | Description |
|---|---|---|
| `EVENT_OUTCOME_UNSPECIFIED` | 0 | Not set; never returned by the server. |
| `EVENT_OUTCOME_ACCEPTED` | 1 | Stored (or, in asynchronous acknowledgement mode, queued for storage) and grouped into an issue. |
| `EVENT_OUTCOME_DUPLICATE` | 2 | An event with the same event_id was already stored for this project; it was not stored again. The issue fields refer to the original event. |
| `EVENT_OUTCOME_FILTERED` | 3 | Dropped by a project filter (e.g. an ignored environment or a discarded issue). |
| `EVENT_OUTCOME_RATE_LIMITED` | 4 | Dropped because the key, project or organization exceeded its rate limit or monthly quota (batch calls only; single calls fail with RESOURCE_EXHAUSTED instead). |
| `EVENT_OUTCOME_INVALID` | 5 | Rejected because it has no usable content (batch calls only; single calls fail with INVALID_ARGUMENT instead). |

<a id="izci-ingest-v1-level"></a>

### Level

Level is the severity of an event, from least to most severe.

| Name | Number | Description |
|---|---|---|
| `LEVEL_UNSPECIFIED` | 0 | Level not set. The server uses ERROR for events with exceptions and INFO for message-only events. |
| `LEVEL_DEBUG` | 1 | Diagnostic information useful only while debugging. |
| `LEVEL_INFO` | 2 | Informational message about normal operation. |
| `LEVEL_WARNING` | 3 | Something unexpected happened but the operation continued. |
| `LEVEL_ERROR` | 4 | An operation failed. The default for exceptions. |
| `LEVEL_FATAL` | 5 | The application (or a critical part of it) crashed or became unusable. |

