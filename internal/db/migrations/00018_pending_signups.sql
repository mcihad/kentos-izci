-- +goose Up

-- Self sign-ups waiting for their e-mail address to be confirmed. The
-- account and its organization are created only when the link sent to the
-- address is opened, so nobody can hold an account under an address they
-- cannot read. Several may wait for one address; none of them blocks its
-- owner.
CREATE TABLE pending_signups (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  email             text NOT NULL,
  name              text NOT NULL,
  password_hash     text NOT NULL,
  organization_name text NOT NULL,
  organization_slug text NOT NULL,
  -- SHA-256 of the link token; the token itself is never stored.
  token_hash        bytea NOT NULL UNIQUE,
  created_at        timestamptz NOT NULL DEFAULT now(),
  expires_at        timestamptz NOT NULL
);
CREATE INDEX pending_signups_email_idx ON pending_signups (email);
CREATE INDEX pending_signups_expires_idx ON pending_signups (expires_at);

-- +goose Down
DROP TABLE pending_signups;
