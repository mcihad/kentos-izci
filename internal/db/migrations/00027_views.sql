-- +goose Up

-- Named issue searches and dashboards of an organization; personal unless
-- shared. The search or the widgets are the ViewService messages as
-- protojson.
CREATE TABLE saved_searches (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  owner_id   uuid REFERENCES users (id) ON DELETE CASCADE,
  name       text NOT NULL,
  search     jsonb NOT NULL DEFAULT '{}',
  shared     boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saved_searches_org_idx ON saved_searches (org_id, owner_id);
SELECT izci_enable_tenant_rls('saved_searches');

CREATE TABLE dashboards (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  owner_id   uuid REFERENCES users (id) ON DELETE CASCADE,
  name       text NOT NULL,
  widgets    jsonb NOT NULL DEFAULT '[]',
  shared     boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dashboards_org_idx ON dashboards (org_id, owner_id);
SELECT izci_enable_tenant_rls('dashboards');

-- +goose Down
DROP TABLE dashboards;
DROP TABLE saved_searches;
