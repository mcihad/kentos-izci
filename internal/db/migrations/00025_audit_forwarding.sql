-- +goose Up

-- The audit log is read in time order across organizations: by server
-- administrators (instance entries) and by the SIEM forwarder.
CREATE INDEX audit_log_created_idx ON audit_log (created_at, id);

-- +goose Down
DROP INDEX audit_log_created_idx;
