-- +goose Up

-- Fingerprints of issues merged into another one: new events with them
-- join the target issue, and the merged issue's id and short id lead to it.
CREATE TABLE merged_hashes (
  project_id       uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  hash             bytea NOT NULL,
  issue_id         uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
  org_id           uuid NOT NULL,
  -- The merged issue, deleted by the merge: its id and short id, and what
  -- an unmerge restores (title, culprit, type, value, platform, grouping,
  -- metadata).
  source_issue_id  uuid NOT NULL,
  source_short_seq integer,
  source           jsonb NOT NULL DEFAULT '{}',
  merged_by        uuid REFERENCES users (id) ON DELETE SET NULL,
  merged_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, hash)
);
CREATE INDEX merged_hashes_issue_idx ON merged_hashes (issue_id);
CREATE INDEX merged_hashes_source_idx ON merged_hashes (source_issue_id);
CREATE INDEX merged_hashes_short_idx ON merged_hashes (project_id, source_short_seq);
SELECT izci_enable_tenant_rls('merged_hashes');

-- The grouping fingerprint of an event in an issue with another
-- fingerprint (a merged one); NULL means the issue's own fingerprint.
ALTER TABLE events ADD COLUMN grouping_hash bytea;

-- Events waiting to move between issues after a merge or an unmerge; the
-- issue-purge job moves them a batch at a time, oldest request first.
CREATE TABLE issue_moves (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id           uuid NOT NULL,
  project_id       uuid NOT NULL,
  from_issue       uuid NOT NULL,
  to_issue         uuid NOT NULL,
  -- Merge: the fingerprint of the merged issue, kept on moved events that
  -- have none. Unmerge: only events with this fingerprint move.
  hash             bytea NOT NULL,
  unmerge          boolean NOT NULL DEFAULT false,
  created_at       timestamptz NOT NULL DEFAULT now()
);
SELECT izci_enable_tenant_rls('issue_moves');

-- +goose Down
DROP TABLE issue_moves;
ALTER TABLE events DROP COLUMN grouping_hash;
DROP TABLE merged_hashes;
