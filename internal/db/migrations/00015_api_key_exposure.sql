-- +goose Up

-- When a key with the releases scope was first used by a browser (an
-- ingest request with an Origin header). Such a key is public, so it can no
-- longer upload files or register releases.
ALTER TABLE api_keys ADD COLUMN browser_seen_at timestamptz;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN browser_seen_at;
