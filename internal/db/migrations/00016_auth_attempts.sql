-- +goose Up

-- Failed sign-ins and guesses of unauthenticated endpoints, counted per
-- key ("login:<email>|<address>", "email:<email>", "ip:<address>",
-- "public:<address>") in fixed windows, shared by all instances.
CREATE TABLE auth_attempts (
  key          text PRIMARY KEY,
  count        integer NOT NULL,
  window_start timestamptz NOT NULL
);
CREATE INDEX auth_attempts_window_idx ON auth_attempts (window_start);

-- +goose Down
DROP TABLE auth_attempts;
