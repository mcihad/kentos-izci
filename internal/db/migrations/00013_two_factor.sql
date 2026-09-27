-- +goose Up

-- Two-factor authentication: time-based one-time passwords (RFC 6238) from
-- an authenticator app, with single-use recovery codes.
ALTER TABLE users
  -- TOTP secret sealed with the secret box (additional data: the user id).
  ADD COLUMN totp_secret         bytea,
  -- When two-factor authentication was turned on; NULL while it is off.
  ADD COLUMN totp_enabled_at     timestamptz,
  -- Last accepted time step: every code is accepted once.
  ADD COLUMN totp_last_step      bigint NOT NULL DEFAULT 0,
  -- Secret shown during setup until a first code confirms it.
  ADD COLUMN totp_pending_secret bytea,
  ADD COLUMN totp_pending_at     timestamptz,
  -- Consecutive wrong codes, and the lockout they caused.
  ADD COLUMN totp_failures       integer NOT NULL DEFAULT 0,
  ADD COLUMN totp_locked_until   timestamptz;

-- Single-use recovery codes, stored as keyed hashes (HMAC with a key derived
-- from IZCI_SECRET_KEY, so a database copy alone cannot be brute-forced).
CREATE TABLE recovery_codes (
  user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash text NOT NULL,
  used_at   timestamptz,
  PRIMARY KEY (user_id, code_hash)
);

-- Sign-ins waiting for their second step. The browser keeps the token in
-- the izci_2fa cookie; only its SHA-256 is stored.
CREATE TABLE login_challenges (
  token_hash bytea PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
CREATE INDEX login_challenges_user_idx ON login_challenges (user_id);
CREATE INDEX login_challenges_expires_idx ON login_challenges (expires_at);

-- +goose Down
DROP TABLE login_challenges;
DROP TABLE recovery_codes;
ALTER TABLE users
  DROP COLUMN totp_secret,
  DROP COLUMN totp_enabled_at,
  DROP COLUMN totp_last_step,
  DROP COLUMN totp_pending_secret,
  DROP COLUMN totp_pending_at,
  DROP COLUMN totp_failures,
  DROP COLUMN totp_locked_until;
