-- +goose Up

-- When the member's last summary e-mail went out
-- (NotificationSettings.digest).
ALTER TABLE users ADD COLUMN digest_sent_at timestamptz;

-- +goose Down
ALTER TABLE users DROP COLUMN digest_sent_at;
