-- +goose Up

-- Members following an issue: its assignees, commenters, members named in
-- its comments and those who chose to follow it.
CREATE TABLE issue_subscriptions (
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (issue_id, user_id)
);
CREATE INDEX issue_subscriptions_user_idx ON issue_subscriptions (user_id);
SELECT izci_enable_tenant_rls('issue_subscriptions');

-- Personal notifications: a member's inbox, and the e-mail and Telegram
-- messages sent for its entries.
CREATE TABLE user_notifications (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id          uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  issue_id        uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
  -- izci.api.v1.UserNotificationKind
  kind            smallint NOT NULL,
  -- Who caused it; NULL when izci did (rules, regressions).
  actor_id        uuid REFERENCES users (id) ON DELETE SET NULL,
  -- Comment excerpt, reason of an automatic assignment.
  data            jsonb NOT NULL DEFAULT '{}',
  created_at      timestamptz NOT NULL DEFAULT now(),
  read_at         timestamptz,
  -- E-mail and Telegram delivery: pending while delivered_at is NULL.
  delivered_at    timestamptz,
  attempts        smallint NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_notifications_inbox_idx ON user_notifications (user_id, created_at DESC);
CREATE INDEX user_notifications_pending_idx ON user_notifications (next_attempt_at) WHERE delivered_at IS NULL;
SELECT izci_enable_tenant_rls('user_notifications');

-- Where a member's personal notifications go besides the inbox
-- (izci.api.v1.NotificationSettings); empty means the defaults.
ALTER TABLE users ADD COLUMN notification_settings jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE users DROP COLUMN notification_settings;
DROP TABLE user_notifications;
DROP TABLE issue_subscriptions;
