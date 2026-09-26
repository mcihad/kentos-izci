-- +goose Up

CREATE TABLE integrations (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  provider           text NOT NULL DEFAULT 'github',
  -- izci.api.v1.IntegrationAuth: 1 token, 2 GitHub App.
  auth               smallint NOT NULL,
  name               text NOT NULL,
  account_login      text NOT NULL DEFAULT '',
  account_avatar_url text NOT NULL DEFAULT '',
  installation_id    bigint,
  -- AES-256-GCM sealed token (nonce || ciphertext) for auth = 1.
  secret             bytea,
  healthy            boolean NOT NULL DEFAULT true,
  status_message     text NOT NULL DEFAULT '',
  created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  verified_at        timestamptz
);
CREATE INDEX integrations_org_idx ON integrations (org_id);
SELECT izci_enable_tenant_rls('integrations');

CREATE TABLE project_repositories (
  project_id     uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
  org_id         uuid NOT NULL,
  integration_id uuid REFERENCES integrations (id) ON DELETE SET NULL,
  provider       text NOT NULL DEFAULT 'github',
  owner          text NOT NULL,
  name           text NOT NULL,
  url            text NOT NULL,
  default_branch text NOT NULL DEFAULT 'main',
  -- [{"from_prefix":"/app/","to_prefix":"src/"}, …]
  path_mappings  jsonb NOT NULL DEFAULT '[]',
  linked_at      timestamptz NOT NULL DEFAULT now()
);
SELECT izci_enable_tenant_rls('project_repositories');

CREATE TABLE ai_runs (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id       uuid NOT NULL,
  project_id   uuid NOT NULL,
  issue_id     uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
  -- Server-side id of the analyzed event.
  event_id     uuid,
  -- izci.api.v1.AiRunStatus / AiRunTrigger / AiRunMode.
  status       smallint NOT NULL DEFAULT 1,
  trigger      smallint NOT NULL,
  mode         smallint NOT NULL,
  model        text NOT NULL DEFAULT '',
  instructions text NOT NULL DEFAULT '',
  requested_by uuid REFERENCES users (id) ON DELETE SET NULL,
  base_commit  text NOT NULL DEFAULT '',
  base_branch  text NOT NULL DEFAULT '',
  -- izci.api.v1.AiResult as JSON.
  result       jsonb NOT NULL DEFAULT '{}',
  analysis     text NOT NULL DEFAULT '',
  diff         text NOT NULL DEFAULT '',
  files        jsonb NOT NULL DEFAULT '[]',
  -- izci.api.v1.PullRequestRef as JSON once a branch or PR exists.
  pull_request jsonb,
  error        text NOT NULL DEFAULT '',
  usage        jsonb NOT NULL DEFAULT '{}',
  -- Instance that claimed the run.
  worker       text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  started_at   timestamptz,
  finished_at  timestamptz
);
CREATE INDEX ai_runs_issue_idx ON ai_runs (issue_id, created_at DESC);
CREATE INDEX ai_runs_org_idx ON ai_runs (org_id, created_at DESC);
CREATE INDEX ai_runs_queue_idx ON ai_runs (created_at) WHERE status = 1;
-- At most one queued/preparing/running run per issue.
CREATE UNIQUE INDEX ai_runs_active_key ON ai_runs (issue_id) WHERE status IN (1, 2, 3);
SELECT izci_enable_tenant_rls('ai_runs');

CREATE TABLE ai_run_logs (
  run_id  uuid NOT NULL REFERENCES ai_runs (id) ON DELETE CASCADE,
  seq     integer NOT NULL,
  org_id  uuid NOT NULL,
  time    timestamptz NOT NULL DEFAULT now(),
  -- izci.api.v1.AiLogKind.
  kind    smallint NOT NULL,
  tool    text NOT NULL DEFAULT '',
  message text NOT NULL,
  PRIMARY KEY (run_id, seq)
);
SELECT izci_enable_tenant_rls('ai_run_logs');

-- +goose Down
DROP TABLE ai_run_logs;
DROP TABLE ai_runs;
DROP TABLE project_repositories;
DROP TABLE integrations;
