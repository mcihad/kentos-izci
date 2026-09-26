-- +goose Up

-- Alert throttling looks up the latest notification a rule produced for an
-- issue.
CREATE INDEX notification_outbox_rule_issue_idx
  ON notification_outbox (rule_id, issue_id, created_at DESC)
  WHERE rule_id IS NOT NULL;

-- A Telegram message is re-rendered after button presses: keep what it
-- announced (new issue, regression, frequency) and the triggering event.
ALTER TABLE notification_threads
  ADD COLUMN kind smallint NOT NULL DEFAULT 1,
  ADD COLUMN payload jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE notification_threads DROP COLUMN payload, DROP COLUMN kind;
DROP INDEX notification_outbox_rule_issue_idx;
