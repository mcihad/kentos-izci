-- +goose Up

-- Ownership of a project's issues: rules in Sentry's syntax
-- ("path:src/billing/* ayse@kentos.com.tr"), an imported CODEOWNERS file
-- and how new issues are assigned (izci.api.v1.AutoAssign).
CREATE TABLE project_ownership (
  project_id           uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
  org_id               uuid NOT NULL,
  rules                text NOT NULL DEFAULT '',
  codeowners           text NOT NULL DEFAULT '',
  -- Where CODEOWNERS came from ("owner/name:.github/CODEOWNERS"), empty
  -- when pasted.
  codeowners_source    text NOT NULL DEFAULT '',
  codeowners_synced_at timestamptz,
  auto_assign          smallint NOT NULL DEFAULT 0,
  updated_at           timestamptz NOT NULL DEFAULT now()
);
SELECT izci_enable_tenant_rls('project_ownership');

-- Owners written as "@login", "@org/team" or "#team" in rules and
-- CODEOWNERS, mapped to organization members.
CREATE TABLE owner_aliases (
  org_id     uuid NOT NULL,
  alias      text NOT NULL,
  user_ids   uuid[] NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, alias)
);
SELECT izci_enable_tenant_rls('owner_aliases');

-- The commit that last changed the lines of an issue's stack trace (git
-- blame of the innermost in-app frames), found through GitHub.
CREATE TABLE issue_suspect_commits (
  issue_id     uuid PRIMARY KEY REFERENCES issues (id) ON DELETE CASCADE,
  org_id       uuid NOT NULL,
  -- 1 found, 2 no in-app frames with line numbers, 3 files not in the
  -- repository, 4 failed (detail says why).
  state        smallint NOT NULL,
  detail       text NOT NULL DEFAULT '',
  sha          text NOT NULL DEFAULT '',
  message      text NOT NULL DEFAULT '',
  url          text NOT NULL DEFAULT '',
  author_name  text NOT NULL DEFAULT '',
  author_email text NOT NULL DEFAULT '',
  author_login text NOT NULL DEFAULT '',
  committed_at timestamptz,
  path         text NOT NULL DEFAULT '',
  line         integer NOT NULL DEFAULT 0,
  computed_at  timestamptz NOT NULL DEFAULT now()
);
SELECT izci_enable_tenant_rls('issue_suspect_commits');

-- +goose Down
DROP TABLE issue_suspect_commits;
DROP TABLE owner_aliases;
DROP TABLE project_ownership;
