-- +goose Up

CREATE TABLE notification_channels (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id           uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  project_id       uuid REFERENCES projects (id) ON DELETE SET NULL,
  -- izci.api.v1.ChannelKind: 1 Telegram.
  kind             smallint NOT NULL,
  name             text NOT NULL,
  -- Telegram: {"chat_id":…, "thread_id":…, "title":…, "type":…, "username":…}
  config           jsonb NOT NULL,
  muted            boolean NOT NULL DEFAULT false,
  created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  last_delivery_at timestamptz,
  last_error       text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX notification_channels_telegram_key
  ON notification_channels (org_id, (config ->> 'chat_id'), (coalesce(config ->> 'thread_id', '0')))
  WHERE kind = 1;
SELECT izci_enable_tenant_rls('notification_channels');

CREATE TABLE alert_rules (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id            uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  -- NULL: all projects of the organization.
  project_id        uuid REFERENCES projects (id) ON DELETE CASCADE,
  name              text NOT NULL,
  enabled           boolean NOT NULL DEFAULT true,
  -- {"kind":1,"threshold":0,"window_seconds":0} (izci.api.v1.TriggerKind)
  trigger           jsonb NOT NULL,
  -- {"min_level":4,"environments":[…],"customers":[…],"tags":{…},"unhandled_only":false}
  filters           jsonb NOT NULL DEFAULT '{}',
  channel_ids       uuid[] NOT NULL,
  throttle_seconds  integer NOT NULL DEFAULT 1800,
  created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  last_triggered_at timestamptz
);
CREATE INDEX alert_rules_org_idx ON alert_rules (org_id);
SELECT izci_enable_tenant_rls('alert_rules');

-- Transactional outbox: rows are written in the same transaction as the
-- events that triggered them and delivered by the dispatcher with retries.
CREATE TABLE notification_outbox (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id          uuid NOT NULL,
  channel_id      uuid NOT NULL,
  rule_id         uuid,
  issue_id        uuid,
  -- 1 new issue, 2 regression, 3 frequency, 4 test.
  kind            smallint NOT NULL,
  payload         jsonb NOT NULL DEFAULT '{}',
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now(),
  sent_at         timestamptz,
  error           text NOT NULL DEFAULT ''
);
CREATE INDEX notification_outbox_pending_idx ON notification_outbox (next_attempt_at) WHERE sent_at IS NULL;
SELECT izci_enable_tenant_rls('notification_outbox');

-- The message posted per (channel, issue), so repeated triggers edit it in
-- place instead of posting again.
CREATE TABLE notification_threads (
  channel_id   uuid NOT NULL REFERENCES notification_channels (id) ON DELETE CASCADE,
  issue_id     uuid NOT NULL,
  org_id       uuid NOT NULL,
  message_id   bigint NOT NULL,
  last_sent_at timestamptz NOT NULL,
  last_count   bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (channel_id, issue_id)
);
SELECT izci_enable_tenant_rls('notification_threads');

-- +goose Down
DROP TABLE notification_threads;
DROP TABLE notification_outbox;
DROP TABLE alert_rules;
DROP TABLE notification_channels;
