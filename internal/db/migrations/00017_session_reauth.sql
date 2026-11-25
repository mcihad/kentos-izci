-- +goose Up

-- When the session last proved who its user is: at sign-in, or later with
-- the password, a two-factor code or the identity provider. Sensitive
-- changes (passwords, access tokens, two-factor settings) need it recent.
ALTER TABLE sessions ADD COLUMN reauthenticated_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE sessions DROP COLUMN reauthenticated_at;
