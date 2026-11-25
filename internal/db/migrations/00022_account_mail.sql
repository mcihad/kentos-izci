-- +goose Up

-- Links that set a new password ("parolamı unuttum"); only the token's
-- SHA-256 is stored.
CREATE TABLE password_resets (
  token_hash bytea PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
CREATE INDEX password_resets_user_idx ON password_resets (user_id);

-- Browsers (family and operating system) members signed in from, to tell
-- them about sign-ins from a new one.
CREATE TABLE user_devices (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  device     text NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  last_seen  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, device)
);

-- +goose Down
DROP TABLE user_devices;
DROP TABLE password_resets;
