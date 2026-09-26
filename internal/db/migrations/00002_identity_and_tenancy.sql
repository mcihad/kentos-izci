-- +goose Up

-- Row-level security predicate shared by every tenant table. A row is
-- visible when the transaction is scoped to its organization
-- (db.InOrg sets izci.org_id) or runs in system scope (db.InSystem sets
-- izci.system). Unset settings match nothing, so forgetting to scope a
-- transaction fails closed. The body is a single expression, which lets the
-- planner inline it into every policy.
-- +goose StatementBegin
CREATE FUNCTION izci_tenant_visible(org uuid) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT coalesce(current_setting('izci.system', true), '') = 'on'
      OR org = nullif(current_setting('izci.org_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- Enables and forces row-level security with the tenant policy on a table
-- that has an org_id column. FORCE makes the policy apply to the table owner
-- (the izci role) as well.
-- +goose StatementBegin
CREATE FUNCTION izci_enable_tenant_rls(tbl regclass) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', tbl);
  EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', tbl);
  EXECUTE format(
    'CREATE POLICY tenant_isolation ON %s USING (izci_tenant_visible(org_id)) WITH CHECK (izci_tenant_visible(org_id))',
    tbl);
END
$$;
-- +goose StatementEnd

-- ── Instance-wide identity (no tenant scope) ────────────────────────────────

CREATE TABLE users (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  email              text NOT NULL,
  name               text NOT NULL DEFAULT '',
  avatar_url         text NOT NULL DEFAULT '',
  -- argon2id PHC string; NULL for accounts that only sign in through OIDC.
  password_hash      text,
  superadmin         boolean NOT NULL DEFAULT false,
  disabled           boolean NOT NULL DEFAULT false,
  telegram_user_id   bigint UNIQUE,
  telegram_username  text NOT NULL DEFAULT '',
  telegram_linked_at timestamptz,
  last_login_at      timestamptz,
  last_seen_at       timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE sessions (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  -- SHA-256 of the cookie token; the token itself is never stored.
  token_hash   bytea NOT NULL UNIQUE,
  ip_address   inet,
  user_agent   text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE oidc_identities (
  provider      text NOT NULL,
  subject       text NOT NULL,
  user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email         text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz,
  PRIMARY KEY (provider, subject)
);
CREATE INDEX oidc_identities_user_idx ON oidc_identities (user_id);

CREATE TABLE instance_settings (
  key        text PRIMARY KEY,
  value      jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- ── Organizations (the tenant registry itself is not tenant-scoped) ─────────

CREATE TABLE organizations (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  slug       text NOT NULL UNIQUE,
  name       text NOT NULL,
  plan       text NOT NULL DEFAULT 'internal',
  -- OrganizationLimits as JSON; zero or missing means unlimited.
  limits     jsonb NOT NULL DEFAULT '{}',
  -- OrganizationSettings as JSON (timezone, auto-join domains, …).
  settings   jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT organizations_slug_format CHECK (slug ~ '^[a-z][a-z0-9-]{0,48}[a-z0-9]$')
);

-- Membership is looked up by user across organizations (organization
-- switcher, authorization), so it is not tenant-scoped; access goes through
-- the store, which always filters by user or organization.
CREATE TABLE org_members (
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  -- izci.api.v1.Role: 1 viewer, 2 member, 3 admin, 4 owner.
  role       smallint NOT NULL CHECK (role BETWEEN 1 AND 4),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_members_user_idx ON org_members (user_id);

-- Personal access tokens authenticate API/CLI/MCP calls; looked up by hash
-- before any tenant is known.
CREATE TABLE personal_tokens (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  -- NULL: valid in every organization of the user.
  org_id       uuid REFERENCES organizations (id) ON DELETE CASCADE,
  name         text NOT NULL,
  prefix       text NOT NULL,
  token_hash   bytea NOT NULL UNIQUE,
  -- izci.api.v1.TokenScope values.
  scopes       smallint[] NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  expires_at   timestamptz,
  revoked_at   timestamptz
);
CREATE INDEX personal_tokens_user_idx ON personal_tokens (user_id);

-- One-time codes that link Telegram chats (kind 1) or Telegram accounts
-- (kind 2); redeemed by the bot, which knows only the code.
CREATE TABLE link_codes (
  code       text PRIMARY KEY,
  kind       smallint NOT NULL,
  org_id     uuid REFERENCES organizations (id) ON DELETE CASCADE,
  project_id uuid,
  user_id    uuid REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at    timestamptz
);

-- ── Tenant-scoped organization data ─────────────────────────────────────────

CREATE TABLE invitations (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  email       text NOT NULL,
  role        smallint NOT NULL CHECK (role BETWEEN 1 AND 4),
  token_hash  bytea NOT NULL UNIQUE,
  invited_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL,
  accepted_at timestamptz,
  accepted_by uuid REFERENCES users (id) ON DELETE SET NULL,
  revoked_at  timestamptz
);
CREATE INDEX invitations_org_idx ON invitations (org_id, created_at DESC);
SELECT izci_enable_tenant_rls('invitations');

CREATE TABLE audit_log (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  -- NULL for instance-level actions (visible in system scope only).
  org_id      uuid REFERENCES organizations (id) ON DELETE CASCADE,
  actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
  action      text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  target_id   text NOT NULL DEFAULT '',
  target_name text NOT NULL DEFAULT '',
  metadata    jsonb NOT NULL DEFAULT '{}',
  ip_address  inet,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_org_idx ON audit_log (org_id, created_at DESC);
SELECT izci_enable_tenant_rls('audit_log');

CREATE TABLE usage_daily (
  org_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  day          date NOT NULL,
  accepted     bigint NOT NULL DEFAULT 0,
  rate_limited bigint NOT NULL DEFAULT 0,
  filtered     bigint NOT NULL DEFAULT 0,
  ai_runs      integer NOT NULL DEFAULT 0,
  PRIMARY KEY (org_id, day)
);
SELECT izci_enable_tenant_rls('usage_daily');

-- +goose Down
DROP TABLE usage_daily;
DROP TABLE audit_log;
DROP TABLE invitations;
DROP TABLE link_codes;
DROP TABLE personal_tokens;
DROP TABLE org_members;
DROP TABLE organizations;
DROP TABLE instance_settings;
DROP TABLE oidc_identities;
DROP TABLE sessions;
DROP TABLE users;
DROP FUNCTION izci_enable_tenant_rls(regclass);
DROP FUNCTION izci_tenant_visible(uuid);
