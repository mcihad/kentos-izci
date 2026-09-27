-- +goose Up

-- Pieces of files uploaded with Sentry's chunked upload protocol
-- (sentry-cli, the Sentry bundler plugins) until they are assembled.
-- Assembled chunks are deleted; stray ones after an hour.
CREATE TABLE upload_chunks (
  org_id     uuid NOT NULL,
  -- SHA-1 of the (uncompressed) chunk in hex.
  checksum   text NOT NULL,
  data       bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, checksum)
);
ALTER TABLE upload_chunks ALTER COLUMN data SET COMPRESSION lz4;
CREATE INDEX upload_chunks_created_idx ON upload_chunks (created_at);
SELECT izci_enable_tenant_rls('upload_chunks');

-- Artifact bundles: minified JavaScript and its source maps, uploaded for a
-- project, a release or both (files carry debug ids when sentry-cli
-- injected them).
CREATE TABLE artifact_bundles (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id       uuid NOT NULL,
  project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  -- SHA-1 of the bundle archive; the same archive is stored once.
  checksum     text NOT NULL,
  -- The bundle's own id from its manifest, if any.
  bundle_id    uuid,
  release      text NOT NULL DEFAULT '',
  dist         text NOT NULL DEFAULT '',
  file_count   integer NOT NULL,
  size_bytes   bigint NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  -- When a file of the bundle last resolved a stack frame (at most hourly).
  last_used_at timestamptz,
  UNIQUE (project_id, checksum)
);
CREATE INDEX artifact_bundles_project_idx ON artifact_bundles (project_id, created_at DESC);
SELECT izci_enable_tenant_rls('artifact_bundles');

-- Files of artifact bundles.
CREATE TABLE artifact_files (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id     uuid NOT NULL,
  project_id uuid NOT NULL,
  bundle_id  uuid NOT NULL REFERENCES artifact_bundles (id) ON DELETE CASCADE,
  -- Release and dist of the bundle, repeated for lookups.
  release    text NOT NULL DEFAULT '',
  dist       text NOT NULL DEFAULT '',
  -- URL the file is served under, usually without host: "~/assets/app.js".
  url        text NOT NULL,
  -- 1 minified source, 2 source map, 3 original source.
  kind       smallint NOT NULL,
  debug_id   uuid,
  -- The source map a minified file refers to, relative to the file.
  sourcemap  text NOT NULL DEFAULT '',
  size_bytes integer NOT NULL,
  data       bytea NOT NULL
);
ALTER TABLE artifact_files ALTER COLUMN data SET COMPRESSION lz4;
CREATE INDEX artifact_files_debug_idx ON artifact_files (project_id, debug_id, kind) WHERE debug_id IS NOT NULL;
CREATE INDEX artifact_files_url_idx ON artifact_files (project_id, url, release);
CREATE INDEX artifact_files_bundle_idx ON artifact_files (bundle_id);
SELECT izci_enable_tenant_rls('artifact_files');

-- Debug information files of natively compiled code (the split debug info
-- of Flutter and Dart AOT builds) and Dart obfuscation maps.
CREATE TABLE debug_files (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  org_id       uuid NOT NULL,
  project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
  debug_id     uuid NOT NULL,
  code_id      text NOT NULL DEFAULT '',
  -- "elf", "macho" or "dartsymbolmap".
  kind         text NOT NULL,
  -- File name as uploaded, e.g. "app.android-arm64.symbols".
  name         text NOT NULL,
  arch         text NOT NULL DEFAULT '',
  -- "debug" (DWARF), "symtab"; "mapping" for obfuscation maps.
  features     text[] NOT NULL DEFAULT '{}',
  -- SHA-1 of the file; the same file is stored once per project.
  checksum     text NOT NULL,
  size_bytes   bigint NOT NULL,
  data         bytea NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  UNIQUE (project_id, checksum)
);
ALTER TABLE debug_files ALTER COLUMN data SET COMPRESSION lz4;
CREATE INDEX debug_files_debug_idx ON debug_files (project_id, debug_id, kind);
CREATE INDEX debug_files_project_idx ON debug_files (project_id, created_at DESC);
SELECT izci_enable_tenant_rls('debug_files');

-- +goose Down
DROP TABLE debug_files;
DROP TABLE artifact_files;
DROP TABLE artifact_bundles;
DROP TABLE upload_chunks;
