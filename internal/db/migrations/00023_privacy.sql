-- +goose Up

-- Organizations are deleted in the background: marked here and hidden at
-- once, then purged with their projects.
ALTER TABLE organizations ADD COLUMN deleted_at timestamptz;

-- The identifiers of the person an event is about, for data subject
-- requests (KVKK, GDPR): the user id, the e-mail address in lower case, the
-- username and the client IP address, each prefixed with its kind.
-- ingest.SubjectKeys computes the same keys.
-- +goose StatementBegin
CREATE FUNCTION izci_subject_keys(u jsonb, ip inet) RETURNS text[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT array_remove(ARRAY[
    'id:' || nullif(u ->> 'id', ''),
    'email:' || lower(nullif(u ->> 'email', '')),
    'username:' || nullif(u ->> 'username', ''),
    'ip:' || host(ip)
  ], NULL)
$$;
-- +goose StatementEnd

-- Which issues have events about a person: the first 8 bytes of the
-- SHA-256 of each subject key. Data subject requests find a person's issues
-- here by equality (usable under row-level security, unlike conditions on
-- the event payload) and then scan only those issues' events.
CREATE TABLE issue_subjects (
  issue_id uuid NOT NULL,
  key_hash bytea NOT NULL,
  org_id   uuid NOT NULL,
  PRIMARY KEY (issue_id, key_hash)
);
CREATE INDEX issue_subjects_key_idx ON issue_subjects (org_id, key_hash);
SELECT izci_enable_tenant_rls('issue_subjects');

SELECT set_config('izci.system', 'on', true);
INSERT INTO issue_subjects (issue_id, key_hash, org_id)
SELECT DISTINCT e.issue_id, substring(sha256(convert_to(k, 'UTF8')) FROM 1 FOR 8), e.org_id
FROM events e CROSS JOIN LATERAL unnest(izci_subject_keys(e.payload -> 'user', e.client_ip)) AS k
ON CONFLICT DO NOTHING;

-- Issues members deleted. The rows that carry no foreign key to issues
-- (events, counters, facets, affected users) are removed in the background.
CREATE TABLE deleted_issues (
  issue_id   uuid PRIMARY KEY,
  org_id     uuid NOT NULL,
  project_id uuid NOT NULL,
  deleted_at timestamptz NOT NULL DEFAULT now()
);
SELECT izci_enable_tenant_rls('deleted_issues');

-- +goose Down
DROP TABLE deleted_issues;
DROP TABLE issue_subjects;
DROP FUNCTION izci_subject_keys(jsonb, inet);
ALTER TABLE organizations DROP COLUMN deleted_at;
