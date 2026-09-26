-- +goose Up
-- pg_trgm powers substring search over issue titles, messages and culprits.
-- It is a trusted extension, so the database owner (the izci role) may create
-- it without superuser rights. It always lives in public so that every
-- schema on the search_path (including per-test schemas) can use it.
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
