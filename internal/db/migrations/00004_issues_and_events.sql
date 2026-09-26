-- +goose Up

-- Issues group events that share a fingerprint. The high-volume tables below
-- (events, *_hourly, issue_tags, issue_users) deliberately carry no foreign
-- keys: referential checks on every inserted event would cost throughput,
-- and their rows are removed by retention and project purge jobs.
CREATE TABLE issues (
  id                       uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id                   uuid NOT NULL,
  project_id               uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  short_seq                integer,
  -- First 16 bytes of the SHA-256 grouping hash.
  fingerprint              bytea NOT NULL,
  -- {"version":1,"strategy":"stacktrace","components":[…]}
  grouping                 jsonb NOT NULL DEFAULT '{}',
  title                    text NOT NULL,
  culprit                  text NOT NULL DEFAULT '',
  type                     text NOT NULL DEFAULT '',
  value                    text NOT NULL DEFAULT '',
  -- izci.ingest.v1.Level (highest seen).
  level                    smallint NOT NULL,
  platform                 text NOT NULL DEFAULT '',
  -- izci.api.v1.IssueStatus / IssueSubstatus / Priority / AiState values.
  status                   smallint NOT NULL DEFAULT 1,
  substatus                smallint NOT NULL DEFAULT 1,
  priority                 smallint NOT NULL DEFAULT 2,
  ai_state                 smallint NOT NULL DEFAULT 1,
  unhandled                boolean NOT NULL DEFAULT false,
  first_seen               timestamptz NOT NULL,
  last_seen                timestamptz NOT NULL,
  times_seen               bigint NOT NULL DEFAULT 0,
  users_count              integer NOT NULL DEFAULT 0,
  customers_count          integer NOT NULL DEFAULT 0,
  first_release            text NOT NULL DEFAULT '',
  last_release             text NOT NULL DEFAULT '',
  last_event_id            uuid,
  -- Top in-app frame: {"filename","function","module","lineno"}.
  metadata                 jsonb NOT NULL DEFAULT '{}',
  resolved_at              timestamptz,
  resolved_by              uuid,
  resolved_in_release      text NOT NULL DEFAULT '',
  resolved_in_next_release boolean NOT NULL DEFAULT false,
  snooze_until             timestamptz,
  snooze_count             bigint,
  snooze_base              bigint,
  snooze_until_escalating  boolean NOT NULL DEFAULT false,
  assignee_id              uuid,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, fingerprint)
);
CREATE UNIQUE INDEX issues_short_seq_key ON issues (project_id, short_seq);
CREATE INDEX issues_org_last_seen_idx ON issues (org_id, last_seen DESC, id DESC);
CREATE INDEX issues_org_first_seen_idx ON issues (org_id, first_seen DESC, id DESC);
CREATE INDEX issues_org_status_idx ON issues (org_id, status, last_seen DESC);
CREATE INDEX issues_project_last_seen_idx ON issues (project_id, last_seen DESC);
CREATE INDEX issues_assignee_idx ON issues (assignee_id) WHERE assignee_id IS NOT NULL;
CREATE INDEX issues_search_trgm_idx ON issues USING gin ((title || ' ' || culprit) gin_trgm_ops);
SELECT izci_enable_tenant_rls('issues');

-- Events are partitioned by day on the server receive time. Partitions are
-- created ahead of time by the partition manager and dropped by retention;
-- the default partition only catches rows if maintenance fell behind.
CREATE TABLE events (
  -- Server-side UUIDv7 generated at receive time; its timestamp equals
  -- received_at (to the millisecond), which lets lookups by id prune
  -- partitions.
  id             uuid NOT NULL,
  received_at    timestamptz NOT NULL,
  org_id         uuid NOT NULL,
  project_id     uuid NOT NULL,
  issue_id       uuid NOT NULL,
  -- Client-side Event.event_id.
  event_id       uuid NOT NULL,
  occurred_at    timestamptz NOT NULL,
  level          smallint NOT NULL,
  environment    text NOT NULL DEFAULT '',
  release        text NOT NULL DEFAULT '',
  customer_key   text NOT NULL DEFAULT '',
  tenant_key     text NOT NULL DEFAULT '',
  -- Display label of the user (email, username or id).
  user_label     text NOT NULL DEFAULT '',
  client_ip      inet,
  platform       text NOT NULL DEFAULT '',
  server_name    text NOT NULL DEFAULT '',
  transaction    text NOT NULL DEFAULT '',
  url            text NOT NULL DEFAULT '',
  browser        text NOT NULL DEFAULT '',
  os             text NOT NULL DEFAULT '',
  title          text NOT NULL DEFAULT '',
  -- Informative events (in-app stack trace, request, user) are preferred as
  -- an issue's "recommended" event.
  score          smallint NOT NULL DEFAULT 0,
  unhandled      boolean NOT NULL DEFAULT false,
  size_bytes     integer NOT NULL DEFAULT 0,
  warnings       text[] NOT NULL DEFAULT '{}',
  -- The normalized izci.ingest.v1.Event as protojson.
  payload        jsonb NOT NULL,
  PRIMARY KEY (id, received_at)
) PARTITION BY RANGE (received_at);
ALTER TABLE events ALTER COLUMN payload SET COMPRESSION lz4;
CREATE INDEX events_issue_idx ON events (issue_id, received_at DESC);
CREATE INDEX events_project_idx ON events (project_id, received_at DESC);
CREATE INDEX events_customer_idx ON events (org_id, customer_key, received_at DESC) WHERE customer_key <> '';
CREATE INDEX events_client_event_idx ON events (event_id);
CREATE TABLE events_default PARTITION OF events DEFAULT;
SELECT izci_enable_tenant_rls('events');

-- Hourly event counts per issue (issue charts, sparklines, frequency rules).
CREATE TABLE issue_hourly (
  issue_id   uuid NOT NULL,
  hour       timestamptz NOT NULL,
  org_id     uuid NOT NULL,
  project_id uuid NOT NULL,
  count      bigint NOT NULL,
  PRIMARY KEY (issue_id, hour)
);
CREATE INDEX issue_hourly_org_hour_idx ON issue_hourly (org_id, hour);
SELECT izci_enable_tenant_rls('issue_hourly');

-- Hourly event counts per project and level (dashboard).
CREATE TABLE project_hourly (
  project_id uuid NOT NULL,
  hour       timestamptz NOT NULL,
  level      smallint NOT NULL,
  org_id     uuid NOT NULL,
  count      bigint NOT NULL,
  PRIMARY KEY (project_id, hour, level)
);
CREATE INDEX project_hourly_org_hour_idx ON project_hourly (org_id, hour);
SELECT izci_enable_tenant_rls('project_hourly');

-- Hourly event counts per customer and project (customer health).
CREATE TABLE customer_hourly (
  org_id       uuid NOT NULL,
  customer_key text NOT NULL,
  project_id   uuid NOT NULL,
  hour         timestamptz NOT NULL,
  count        bigint NOT NULL,
  PRIMARY KEY (org_id, customer_key, project_id, hour)
);
CREATE INDEX customer_hourly_org_hour_idx ON customer_hourly (org_id, hour);
SELECT izci_enable_tenant_rls('customer_hourly');

-- Value distribution of tags per issue (facets, search filters such as
-- customer:x or browser:Chrome).
CREATE TABLE issue_tags (
  issue_id   uuid NOT NULL,
  key        text NOT NULL,
  value      text NOT NULL,
  org_id     uuid NOT NULL,
  project_id uuid NOT NULL,
  count      bigint NOT NULL,
  first_seen timestamptz NOT NULL,
  last_seen  timestamptz NOT NULL,
  PRIMARY KEY (issue_id, key, value)
);
CREATE INDEX issue_tags_lookup_idx ON issue_tags (org_id, key, value, issue_id);
SELECT izci_enable_tenant_rls('issue_tags');

-- Distinct affected users per issue (8-byte hash of the user identity).
CREATE TABLE issue_users (
  issue_id   uuid NOT NULL,
  user_hash  bytea NOT NULL,
  org_id     uuid NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (issue_id, user_hash)
);
SELECT izci_enable_tenant_rls('issue_users');

CREATE TABLE issue_activity (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id     uuid NOT NULL,
  issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
  -- izci.api.v1.ActivityKind.
  kind       smallint NOT NULL,
  actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
  text       text NOT NULL DEFAULT '',
  data       jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX issue_activity_issue_idx ON issue_activity (issue_id, created_at DESC);
SELECT izci_enable_tenant_rls('issue_activity');

-- +goose Down
DROP TABLE issue_activity;
DROP TABLE issue_users;
DROP TABLE issue_tags;
DROP TABLE customer_hourly;
DROP TABLE project_hourly;
DROP TABLE issue_hourly;
DROP TABLE events;
DROP TABLE issues;
