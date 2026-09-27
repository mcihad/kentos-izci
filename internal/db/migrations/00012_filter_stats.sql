-- +goose Up

-- Events dropped by a project's inbound filters, per day and reason
-- (izci.api.v1.FilterReason), for the filter statistics.
CREATE TABLE project_filtered_daily (
  org_id     uuid NOT NULL,
  project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  day        date NOT NULL,
  reason     smallint NOT NULL,
  count      bigint NOT NULL,
  PRIMARY KEY (project_id, day, reason)
);
SELECT izci_enable_tenant_rls('project_filtered_daily');

-- +goose Down
DROP TABLE project_filtered_daily;
