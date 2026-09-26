-- +goose Up
-- A GitHub App installation belongs to one organization: linking it gives
-- access to the installation's repositories.
CREATE UNIQUE INDEX integrations_installation_key ON integrations (provider, installation_id) WHERE installation_id IS NOT NULL;

-- +goose Down
DROP INDEX integrations_installation_key;
