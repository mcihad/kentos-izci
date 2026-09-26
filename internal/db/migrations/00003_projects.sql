-- +goose Up

CREATE TABLE projects (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id        uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  -- Numeric id used in Sentry DSNs (Sentry SDKs require a numeric project).
  public_id     bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
  slug          text NOT NULL,
  name          text NOT NULL,
  platform      text NOT NULL DEFAULT '',
  -- izci.api.v1.ProjectSettings as JSON.
  settings      jsonb NOT NULL DEFAULT '{}',
  -- Last issue sequence number handed out; issue short ids are
  -- "<SLUG>-<base36(seq)>".
  issue_seq     integer NOT NULL DEFAULT 0,
  last_event_at timestamptz,
  -- Set when deletion was requested; a background job purges the data.
  deleted_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT projects_slug_format CHECK (slug ~ '^[a-z][a-z0-9-]{0,48}[a-z0-9]$')
);
CREATE UNIQUE INDEX projects_org_slug_key ON projects (org_id, slug) WHERE deleted_at IS NULL;
SELECT izci_enable_tenant_rls('projects');

CREATE TABLE api_keys (
  id                    uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id                uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  project_id            uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name                  text NOT NULL,
  -- Leading characters of the key for display ("izk_4f9c2a").
  prefix                text NOT NULL,
  -- SHA-256 of the full key.
  key_hash              bytea NOT NULL UNIQUE,
  -- izci.api.v1.ApiKeyScope values.
  scopes                smallint[] NOT NULL DEFAULT '{1}',
  allowed_origins       text[] NOT NULL DEFAULT '{}',
  rate_limit_per_minute integer NOT NULL DEFAULT 0,
  enabled               boolean NOT NULL DEFAULT true,
  created_by            uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),
  last_used_at          timestamptz,
  revoked_at            timestamptz
);
CREATE INDEX api_keys_project_idx ON api_keys (project_id, created_at DESC);
SELECT izci_enable_tenant_rls('api_keys');

-- Customers are shared by all projects of an organization and created on
-- first sight during ingest.
CREATE TABLE customers (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  -- Lower-cased Event.customer.id.
  key         text NOT NULL,
  name        text NOT NULL DEFAULT '',
  -- True once the name was edited in izci: ingest no longer overwrites it.
  name_locked boolean NOT NULL DEFAULT false,
  attributes  jsonb NOT NULL DEFAULT '{}',
  notes       text NOT NULL DEFAULT '',
  first_seen  timestamptz NOT NULL DEFAULT now(),
  last_seen   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, key)
);
CREATE INDEX customers_name_trgm_idx ON customers USING gin ((key || ' ' || name) gin_trgm_ops);
SELECT izci_enable_tenant_rls('customers');

CREATE TABLE environments (
  project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  name       text NOT NULL,
  org_id     uuid NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  last_seen  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, name)
);
SELECT izci_enable_tenant_rls('environments');

CREATE TABLE releases (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id     uuid NOT NULL,
  project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  version    text NOT NULL,
  commit_sha text NOT NULL DEFAULT '',
  repository text NOT NULL DEFAULT '',
  ref        text NOT NULL DEFAULT '',
  url        text NOT NULL DEFAULT '',
  -- Order of releases ("newer than") follows first_seen.
  first_seen timestamptz NOT NULL DEFAULT now(),
  last_seen  timestamptz NOT NULL DEFAULT now(),
  new_issues integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, version)
);
CREATE INDEX releases_project_seen_idx ON releases (project_id, first_seen DESC);
SELECT izci_enable_tenant_rls('releases');

CREATE TABLE deploys (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id      uuid NOT NULL,
  release_id  uuid NOT NULL REFERENCES releases (id) ON DELETE CASCADE,
  environment text NOT NULL,
  deployed_at timestamptz NOT NULL,
  deployed_by text NOT NULL DEFAULT ''
);
CREATE INDEX deploys_release_idx ON deploys (release_id, deployed_at DESC);
SELECT izci_enable_tenant_rls('deploys');

-- +goose Down
DROP TABLE deploys;
DROP TABLE releases;
DROP TABLE environments;
DROP TABLE customers;
DROP TABLE api_keys;
DROP TABLE projects;
