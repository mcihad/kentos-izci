package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Artifact file kinds (artifact_files.kind).
const (
	ArtifactMinified  int16 = 1
	ArtifactSourceMap int16 = 2
	ArtifactSource    int16 = 3
)

// Chunk is a piece of an uploaded file.
type Chunk struct {
	Checksum string
	Data     []byte
}

// PutChunks stores uploaded chunks; chunks already present are kept.
func PutChunks(ctx context.Context, q Querier, orgID uuid.UUID, chunks []Chunk) error {
	b := &pgx.Batch{}
	for _, c := range chunks {
		b.Queue(`INSERT INTO upload_chunks (org_id, checksum, data) VALUES ($1, $2, $3)
			ON CONFLICT (org_id, checksum) DO UPDATE SET created_at = now()`, orgID, c.Checksum, c.Data)
	}
	return q.SendBatch(ctx, b).Close()
}

// ChunkSizes returns the sizes of the uploaded chunks among checksums;
// chunks not uploaded yet are absent. The data itself is not loaded.
func ChunkSizes(ctx context.Context, q Querier, orgID uuid.UUID, checksums []string) (map[string]int64, error) {
	rows, err := q.Query(ctx, `SELECT checksum, octet_length(data) FROM upload_chunks WHERE org_id = $1 AND checksum = ANY($2)`,
		orgID, checksums)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sizes := map[string]int64{}
	for rows.Next() {
		var c string
		var n int64
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		sizes[c] = n
	}
	return sizes, rows.Err()
}

// ChunkData returns one uploaded chunk.
func ChunkData(ctx context.Context, q Querier, orgID uuid.UUID, checksum string) ([]byte, error) {
	var b []byte
	err := q.QueryRow(ctx, `SELECT data FROM upload_chunks WHERE org_id = $1 AND checksum = $2`, orgID, checksum).Scan(&b)
	return b, notFound(err)
}

// DeleteChunks removes assembled chunks.
func DeleteChunks(ctx context.Context, q Querier, orgID uuid.UUID, checksums []string) error {
	_, err := q.Exec(ctx, `DELETE FROM upload_chunks WHERE org_id = $1 AND checksum = ANY($2)`, orgID, checksums)
	return err
}

// ArtifactBundle is an uploaded set of JavaScript files and source maps.
type ArtifactBundle struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	Checksum   string
	BundleID   *uuid.UUID
	Release    string
	Dist       string
	FileCount  int
	SizeBytes  int64
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// ArtifactFile is a file of a bundle; Data is only loaded on request.
type ArtifactFile struct {
	ID        uuid.UUID
	BundleID  uuid.UUID
	Release   string
	Dist      string
	URL       string
	Kind      int16
	DebugID   *uuid.UUID
	SourceMap string
	SizeBytes int
	Data      []byte
}

// InsertArtifactBundle stores a bundle with its files unless the project
// already has the same archive; it reports whether it was stored.
func InsertArtifactBundle(ctx context.Context, q Querier, b *ArtifactBundle, files []ArtifactFile) (bool, error) {
	created, err := CreateArtifactBundle(ctx, q, b)
	if err != nil || !created {
		return false, err
	}
	for i := range files {
		if err := InsertArtifactFile(ctx, q, b, &files[i]); err != nil {
			return false, err
		}
	}
	return true, nil
}

// CreateArtifactBundle stores a bundle's row unless the project already has
// the same archive; it reports whether it was stored. Its files follow with
// InsertArtifactFile.
func CreateArtifactBundle(ctx context.Context, q Querier, b *ArtifactBundle) (bool, error) {
	err := q.QueryRow(ctx, `
		INSERT INTO artifact_bundles (org_id, project_id, checksum, bundle_id, release, dist, file_count, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (project_id, checksum) DO NOTHING
		RETURNING id, created_at`,
		b.OrgID, b.ProjectID, b.Checksum, b.BundleID, b.Release, b.Dist, b.FileCount, b.SizeBytes).Scan(&b.ID, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// InsertArtifactFile stores one file of a bundle.
func InsertArtifactFile(ctx context.Context, q Querier, b *ArtifactBundle, f *ArtifactFile) error {
	_, err := q.Exec(ctx, `
		INSERT INTO artifact_files (org_id, project_id, bundle_id, release, dist, url, kind, debug_id, sourcemap, size_bytes, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		b.OrgID, b.ProjectID, b.ID, b.Release, b.Dist, f.URL, f.Kind, f.DebugID, f.SourceMap, len(f.Data), f.Data)
	return err
}

// SetArtifactBundleFileCount records how many files a bundle has.
func SetArtifactBundleFileCount(ctx context.Context, q Querier, id uuid.UUID, n int) error {
	_, err := q.Exec(ctx, `UPDATE artifact_bundles SET file_count = $2 WHERE id = $1`, id, n)
	return err
}

// HasArtifactBundle reports whether the project has an archive stored.
func HasArtifactBundle(ctx context.Context, q Querier, projectID uuid.UUID, checksum string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM artifact_bundles WHERE project_id = $1 AND checksum = $2)`,
		projectID, checksum).Scan(&ok)
	return ok, err
}

const artifactFileColumns = `f.id, f.bundle_id, f.release, f.dist, f.url, f.kind, f.debug_id, f.sourcemap, f.size_bytes`

func scanArtifactFile(row pgx.Row) (*ArtifactFile, error) {
	var f ArtifactFile
	if err := row.Scan(&f.ID, &f.BundleID, &f.Release, &f.Dist, &f.URL, &f.Kind, &f.DebugID, &f.SourceMap, &f.SizeBytes); err != nil {
		return nil, notFound(err)
	}
	return &f, nil
}

// ArtifactFileByDebugID returns the newest file of a kind with a debug id.
func ArtifactFileByDebugID(ctx context.Context, q Querier, projectID, debugID uuid.UUID, kind int16) (*ArtifactFile, error) {
	return scanArtifactFile(q.QueryRow(ctx, `SELECT `+artifactFileColumns+` FROM artifact_files f
		WHERE f.project_id = $1 AND f.debug_id = $2 AND f.kind = $3 ORDER BY f.id DESC LIMIT 1`, projectID, debugID, kind))
}

// ArtifactFileByURL returns the newest file of a kind stored under one of
// the URLs for a release, preferring the given dist.
func ArtifactFileByURL(ctx context.Context, q Querier, projectID uuid.UUID, release, dist string, urls []string, kind int16) (*ArtifactFile, error) {
	return scanArtifactFile(q.QueryRow(ctx, `SELECT `+artifactFileColumns+` FROM artifact_files f
		WHERE f.project_id = $1 AND f.url = ANY($2) AND f.release = $3 AND f.kind = $4 AND (f.dist = $5 OR f.dist = '')
		ORDER BY f.dist = $5 DESC, f.id DESC LIMIT 1`, projectID, urls, release, kind, dist))
}

// ArtifactFileData returns the content of a file.
func ArtifactFileData(ctx context.Context, q Querier, id uuid.UUID) ([]byte, error) {
	var b []byte
	err := q.QueryRow(ctx, `SELECT data FROM artifact_files WHERE id = $1`, id).Scan(&b)
	return b, notFound(err)
}

// TouchArtifactBundles records that bundles resolved frames.
func TouchArtifactBundles(ctx context.Context, q Querier, ids []uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE artifact_bundles SET last_used_at = now() WHERE id = ANY($1)`, ids)
	return err
}

// ListArtifactBundles lists a project's bundles, newest first, before a
// cursor (the id of the last bundle of the previous page).
func ListArtifactBundles(ctx context.Context, q Querier, projectID uuid.UUID, before *uuid.UUID, limit int) ([]*ArtifactBundle, error) {
	rows, err := q.Query(ctx, `SELECT id, org_id, project_id, checksum, bundle_id, release, dist, file_count, size_bytes, created_at, last_used_at
		FROM artifact_bundles WHERE project_id = $1 AND ($2::uuid IS NULL OR id < $2) ORDER BY id DESC LIMIT $3`, projectID, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*ArtifactBundle, error) {
		var b ArtifactBundle
		err := r.Scan(&b.ID, &b.OrgID, &b.ProjectID, &b.Checksum, &b.BundleID, &b.Release, &b.Dist, &b.FileCount, &b.SizeBytes,
			&b.CreatedAt, &b.LastUsedAt)
		return &b, err
	})
}

// ListArtifactFiles lists the files of bundles (without content).
func ListArtifactFiles(ctx context.Context, q Querier, bundleIDs []uuid.UUID) ([]*ArtifactFile, error) {
	rows, err := q.Query(ctx, `SELECT `+artifactFileColumns+` FROM artifact_files f WHERE f.bundle_id = ANY($1)
		ORDER BY f.bundle_id, f.kind, f.url`, bundleIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*ArtifactFile, error) { return scanArtifactFile(r) })
}

// DeleteArtifactBundle removes a bundle of a project with its files.
func DeleteArtifactBundle(ctx context.Context, q Querier, projectID, id uuid.UUID) (*ArtifactBundle, error) {
	var b ArtifactBundle
	err := q.QueryRow(ctx, `DELETE FROM artifact_bundles WHERE id = $1 AND project_id = $2
		RETURNING id, org_id, project_id, checksum, release, dist, file_count, size_bytes`, id, projectID).
		Scan(&b.ID, &b.OrgID, &b.ProjectID, &b.Checksum, &b.Release, &b.Dist, &b.FileCount, &b.SizeBytes)
	if err != nil {
		return nil, notFound(err)
	}
	return &b, nil
}

// DebugFile is an uploaded debug information file or obfuscation map; Data
// is only loaded on request.
type DebugFile struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	DebugID    uuid.UUID
	CodeID     string
	Kind       string
	Name       string
	Arch       string
	Features   []string
	Checksum   string
	SizeBytes  int64
	CreatedAt  time.Time
	LastUsedAt *time.Time
	Data       []byte
}

const debugFileColumns = `id, org_id, project_id, debug_id, code_id, kind, name, arch, features, checksum, size_bytes, created_at, last_used_at`

func scanDebugFile(row pgx.Row) (*DebugFile, error) {
	var f DebugFile
	err := row.Scan(&f.ID, &f.OrgID, &f.ProjectID, &f.DebugID, &f.CodeID, &f.Kind, &f.Name, &f.Arch, &f.Features, &f.Checksum,
		&f.SizeBytes, &f.CreatedAt, &f.LastUsedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &f, nil
}

// InsertDebugFile stores a debug file unless the project has the same file;
// it returns the stored (or existing) file and whether it was new.
func InsertDebugFile(ctx context.Context, q Querier, f *DebugFile) (*DebugFile, bool, error) {
	stored, err := scanDebugFile(q.QueryRow(ctx, `
		INSERT INTO debug_files (org_id, project_id, debug_id, code_id, kind, name, arch, features, checksum, size_bytes, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (project_id, checksum) DO NOTHING
		RETURNING `+debugFileColumns,
		f.OrgID, f.ProjectID, f.DebugID, f.CodeID, f.Kind, f.Name, f.Arch, f.Features, f.Checksum, len(f.Data), f.Data))
	if errors.Is(err, ErrNotFound) {
		existing, err := DebugFileByChecksum(ctx, q, f.ProjectID, f.Checksum)
		return existing, false, err
	}
	return stored, err == nil, err
}

// DebugFileByChecksum returns a project's debug file by SHA-1.
func DebugFileByChecksum(ctx context.Context, q Querier, projectID uuid.UUID, checksum string) (*DebugFile, error) {
	return scanDebugFile(q.QueryRow(ctx, `SELECT `+debugFileColumns+` FROM debug_files WHERE project_id = $1 AND checksum = $2`,
		projectID, checksum))
}

// DebugFileByDebugID returns the newest file of one of the kinds with a
// debug id.
func DebugFileByDebugID(ctx context.Context, q Querier, projectID, debugID uuid.UUID, kinds []string) (*DebugFile, error) {
	return scanDebugFile(q.QueryRow(ctx, `SELECT `+debugFileColumns+` FROM debug_files
		WHERE project_id = $1 AND debug_id = $2 AND kind = ANY($3) ORDER BY id DESC LIMIT 1`, projectID, debugID, kinds))
}

// DebugFileData returns the content of a debug file.
func DebugFileData(ctx context.Context, q Querier, id uuid.UUID) ([]byte, error) {
	var b []byte
	err := q.QueryRow(ctx, `SELECT data FROM debug_files WHERE id = $1`, id).Scan(&b)
	return b, notFound(err)
}

// TouchDebugFiles records that debug files resolved frames.
func TouchDebugFiles(ctx context.Context, q Querier, ids []uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE debug_files SET last_used_at = now() WHERE id = ANY($1)`, ids)
	return err
}

// ListDebugFiles lists a project's debug files, newest first, before a
// cursor.
func ListDebugFiles(ctx context.Context, q Querier, projectID uuid.UUID, before *uuid.UUID, limit int) ([]*DebugFile, error) {
	rows, err := q.Query(ctx, `SELECT `+debugFileColumns+` FROM debug_files
		WHERE project_id = $1 AND ($2::uuid IS NULL OR id < $2) ORDER BY id DESC LIMIT $3`, projectID, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*DebugFile, error) { return scanDebugFile(r) })
}

// DeleteDebugFile removes a debug file of a project.
func DeleteDebugFile(ctx context.Context, q Querier, projectID, id uuid.UUID) (*DebugFile, error) {
	return scanDebugFile(q.QueryRow(ctx, `DELETE FROM debug_files WHERE id = $1 AND project_id = $2 RETURNING `+debugFileColumns,
		id, projectID))
}

// SymbolStats tells whether a project has uploaded source maps or debug
// files at all, so that projects without uploads skip symbolication.
type SymbolStats struct {
	ArtifactBundles int64
	DebugFiles      int64
}

// ProjectSymbolStats counts a project's bundles and debug files.
func ProjectSymbolStats(ctx context.Context, q Querier, projectID uuid.UUID) (SymbolStats, error) {
	var s SymbolStats
	err := q.QueryRow(ctx, `SELECT (SELECT count(*) FROM artifact_bundles WHERE project_id = $1),
		(SELECT count(*) FROM debug_files WHERE project_id = $1)`, projectID).Scan(&s.ArtifactBundles, &s.DebugFiles)
	return s, err
}

// ProjectSymbolBytes returns the size of a project's stored bundles and
// debug files.
func ProjectSymbolBytes(ctx context.Context, q Querier, projectID uuid.UUID) (int64, error) {
	var n int64
	err := q.QueryRow(ctx, `SELECT coalesce((SELECT sum(size_bytes) FROM artifact_bundles WHERE project_id = $1), 0) +
		coalesce((SELECT sum(size_bytes) FROM debug_files WHERE project_id = $1), 0)`, projectID).Scan(&n)
	return n, err
}

// PendingChunkBytes returns the size of an organization's chunks not
// assembled yet.
func PendingChunkBytes(ctx context.Context, q Querier, orgID uuid.UUID) (int64, error) {
	var n int64
	err := q.QueryRow(ctx, `SELECT coalesce(sum(octet_length(data)), 0) FROM upload_chunks WHERE org_id = $1`, orgID).Scan(&n)
	return n, err
}
