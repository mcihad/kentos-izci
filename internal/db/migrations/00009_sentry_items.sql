-- +goose Up

-- Files sent with events: screenshots, log files, view hierarchies. They are
-- linked through the client event id because Sentry SDKs may send an
-- attachment in the same envelope as its event or later.
CREATE TABLE event_attachments (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id          uuid NOT NULL,
  project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  -- Client-side event id (events.event_id).
  event_id        uuid NOT NULL,
  name            text NOT NULL,
  content_type    text NOT NULL DEFAULT 'application/octet-stream',
  -- Sentry attachment type, e.g. "event.attachment", "event.view_hierarchy".
  attachment_type text NOT NULL DEFAULT 'event.attachment',
  size_bytes      integer NOT NULL,
  data            bytea NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE event_attachments ALTER COLUMN data SET COMPRESSION lz4;
CREATE INDEX event_attachments_event_idx ON event_attachments (project_id, event_id);
CREATE INDEX event_attachments_created_idx ON event_attachments (created_at);
SELECT izci_enable_tenant_rls('event_attachments');

-- What end users wrote about a problem (Sentry user feedback), optionally
-- tied to the event they saw.
CREATE TABLE user_feedback (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id       uuid NOT NULL,
  project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  -- Id the SDK gave the feedback; a retried submission is stored once.
  feedback_id  uuid NOT NULL,
  -- Client-side id of the event the feedback is about, when known.
  event_id     uuid,
  -- Issue of that event, resolved when both are stored.
  issue_id     uuid REFERENCES issues (id) ON DELETE SET NULL,
  name         text NOT NULL DEFAULT '',
  email        text NOT NULL DEFAULT '',
  message      text NOT NULL,
  -- Page or screen the feedback was sent from.
  url          text NOT NULL DEFAULT '',
  environment  text NOT NULL DEFAULT '',
  release      text NOT NULL DEFAULT '',
  -- "widget", "api", "user_report" (legacy SDK call) …
  source       text NOT NULL DEFAULT '',
  customer_key text NOT NULL DEFAULT '',
  user_label   text NOT NULL DEFAULT '',
  -- The normalized feedback event (contexts, tags, user) as protojson.
  payload      jsonb,
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, feedback_id)
);
ALTER TABLE user_feedback ALTER COLUMN payload SET COMPRESSION lz4;
CREATE INDEX user_feedback_org_idx ON user_feedback (org_id, created_at DESC);
CREATE INDEX user_feedback_issue_idx ON user_feedback (issue_id, created_at DESC) WHERE issue_id IS NOT NULL;
CREATE INDEX user_feedback_event_idx ON user_feedback (project_id, event_id) WHERE event_id IS NOT NULL;
SELECT izci_enable_tenant_rls('user_feedback');

-- Release health: sessions per release, environment and hour. A session
-- counts once as started and at most once in each of errored (had an
-- error, crashes included), crashed and abnormal.
CREATE TABLE release_sessions (
  org_id      uuid NOT NULL,
  project_id  uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  release     text NOT NULL,
  environment text NOT NULL DEFAULT '',
  hour        timestamptz NOT NULL,
  started     integer NOT NULL DEFAULT 0,
  errored     integer NOT NULL DEFAULT 0,
  crashed     integer NOT NULL DEFAULT 0,
  abnormal    integer NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, release, environment, hour)
);
CREATE INDEX release_sessions_hour_idx ON release_sessions (hour);
SELECT izci_enable_tenant_rls('release_sessions');

-- Users (session distinct ids, stored as keyed hashes) seen per release,
-- environment and day, for crash-free user rates.
CREATE TABLE release_session_users (
  org_id      uuid NOT NULL,
  project_id  uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  release     text NOT NULL,
  environment text NOT NULL DEFAULT '',
  day         date NOT NULL,
  user_hash   bytea NOT NULL,
  crashed     boolean NOT NULL DEFAULT false,
  PRIMARY KEY (project_id, release, environment, day, user_hash)
);
CREATE INDEX release_session_users_day_idx ON release_session_users (day);
SELECT izci_enable_tenant_rls('release_session_users');

-- +goose Down
DROP TABLE release_session_users;
DROP TABLE release_sessions;
DROP TABLE user_feedback;
DROP TABLE event_attachments;
