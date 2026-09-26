-- +goose Up
-- pg_trgm powers substring search over issue titles, messages and culprits.
-- It is a trusted extension, so the database owner (the izci role) may create
-- it without superuser rights.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
