// Package sentryapi serves the part of Sentry's web API that sentry-cli, the
// Sentry bundler plugins (Vite, webpack, Rollup, esbuild) and
// sentry_dart_plugin call to upload source maps and debug files and to
// register releases and deploys, so that they work with izci unchanged:
//
//	sentry-cli --url https://izci.example.com --auth-token izk_… \
//	  sourcemaps upload --org kentos --project kalem dist
//
// Callers authenticate with a project ingest key that has the releases
// scope (for CI) or with a personal access token with the write scope of a
// member. Cookie sessions are not accepted: these endpoints have no CSRF
// protection.
package sentryapi

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols"
)

// maxRequestBytes bounds a chunk upload request (clients are told 32 MiB).
const maxRequestBytes = 34 << 20

// Handler serves the endpoints.
type Handler struct {
	db      *db.DB
	keys    *ingest.KeyResolver
	auth    *auth.Service
	symbols *symbols.Service
	log     *slog.Logger
}

// New creates the handler.
func New(database *db.DB, keys *ingest.KeyResolver, authSvc *auth.Service, sym *symbols.Service, log *slog.Logger) *Handler {
	return &Handler{db: database, keys: keys, auth: authSvc, symbols: sym, log: log}
}

// Register mounts the endpoints.
func (h *Handler) Register(handle func(pattern string, h http.Handler)) {
	route := func(pattern string, f func(http.ResponseWriter, *http.Request)) { handle(pattern, http.HandlerFunc(f)) }
	route("GET /api/0/{$}", h.info)
	route("GET /api/0/organizations/{org}/chunk-upload/{$}", h.chunkOptions)
	route("POST /api/0/organizations/{org}/chunk-upload/{$}", h.uploadChunks)
	route("POST /api/0/organizations/{org}/artifactbundle/assemble/{$}", h.assembleBundle)
	route("POST /api/0/projects/{org}/{project}/files/difs/assemble/{$}", h.assembleDebugFiles)
	route("POST /api/0/organizations/{org}/releases/{$}", h.createRelease)
	route("POST /api/0/projects/{org}/{project}/releases/{$}", h.createRelease)
	route("GET /api/0/organizations/{org}/releases/{version}/{$}", h.getRelease)
	route("GET /api/0/projects/{org}/{project}/releases/{version}/{$}", h.getRelease)
	route("PUT /api/0/organizations/{org}/releases/{version}/{$}", h.updateRelease)
	route("PUT /api/0/projects/{org}/{project}/releases/{version}/{$}", h.updateRelease)
	route("POST /api/0/organizations/{org}/releases/{version}/deploys/{$}", h.createDeploy)
	route("GET /api/0/organizations/{org}/repos/{$}", h.listRepos)
}

// caller is an authenticated client acting in one organization.
type caller struct {
	orgID   uuid.UUID
	orgSlug string
	// keyProject is the only project an ingest key may use.
	keyProject     uuid.UUID
	keyProjectSlug string
	user           *store.User
	scopes         []string
}

// apiError is a Sentry API error: {"detail": "…"}.
type apiError struct {
	status int
	detail string
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, e *apiError) {
	writeJSON(w, e.status, map[string]string{"detail": e.detail})
}

func (h *Handler) fail(w http.ResponseWriter, what string, err error) {
	h.log.Error("sentry api: "+what, slog.Any("err", err))
	writeError(w, &apiError{http.StatusInternalServerError, "internal error, try again"})
}

func bearer(r *http.Request) string {
	a := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(a) > 7 && strings.EqualFold(a[:7], "bearer ") {
		return strings.TrimSpace(a[7:])
	}
	return ""
}

// authenticate checks the token and the organization named in the URL.
func (h *Handler) authenticate(r *http.Request, orgSlug string) (*caller, *apiError) {
	ctx := r.Context()
	token := bearer(r)
	orgSlug = strings.ToLower(strings.TrimSpace(orgSlug))
	unauthorized := &apiError{http.StatusUnauthorized, "Invalid token: use a project API key with the releases scope or a personal access token"}
	switch {
	case secret.HasPrefix(token, secret.PrefixAPIKey):
		// Fresh: another instance may have just found the key in a browser.
		rk, err := h.keys.ResolveFresh(ctx, token)
		if err != nil {
			return nil, unauthorized
		}
		if !rk.Key.HasScope(apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES) {
			return nil, &apiError{http.StatusForbidden, "this API key lacks the releases scope"}
		}
		if !rk.Key.CanUpload() {
			return nil, &apiError{http.StatusForbidden, ingest.ErrKeyExposed}
		}
		if orgSlug != "" && orgSlug != rk.OrgSlug {
			return nil, &apiError{http.StatusNotFound, "organization not found"}
		}
		return &caller{orgID: rk.Key.OrgID, orgSlug: rk.OrgSlug, keyProject: rk.Key.ProjectID, keyProjectSlug: rk.ProjectSlug,
			scopes: []string{"project:releases", "project:write"}}, nil
	case secret.HasPrefix(token, secret.PrefixAccessToken) && h.auth != nil:
		p, err := h.auth.Authenticate(ctx, http.Header{"Authorization": {"Bearer " + token}})
		if err != nil {
			return nil, &apiError{http.StatusInternalServerError, "internal error, try again"}
		}
		if p == nil || p.Token == nil {
			return nil, unauthorized
		}
		if !p.HasScope(apiv1.TokenScope_TOKEN_SCOPE_WRITE) {
			return nil, &apiError{http.StatusForbidden, "this access token lacks the write scope"}
		}
		c := &caller{user: p.User, scopes: []string{"org:read", "project:read", "project:releases", "project:write"}}
		if orgSlug == "" {
			return c, nil
		}
		t, err := h.auth.ResolveTenant(ctx, p, orgSlug)
		if errors.Is(err, auth.ErrTwoFactorRequired) {
			return nil, &apiError{http.StatusForbidden, "the organization requires two-factor authentication; turn it on for your izci account"}
		}
		if errors.Is(err, auth.ErrSuperadminTwoFactor) {
			return nil, &apiError{http.StatusForbidden, "instance administrators need two-factor authentication on this server; turn it on for your izci account"}
		}
		if err != nil {
			return nil, &apiError{http.StatusNotFound, "organization not found"}
		}
		if t.Role < apiv1.Role_ROLE_MEMBER {
			return nil, &apiError{http.StatusForbidden, "viewers cannot upload; ask for the member role"}
		}
		c.orgID, c.orgSlug = t.Org.ID, t.Org.Slug
		return c, nil
	}
	return nil, unauthorized
}

// project resolves a project slug the caller may use.
func (h *Handler) project(ctx context.Context, c *caller, slug string) (*store.Project, *apiError) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	notFound := &apiError{http.StatusNotFound, "project " + slug + " not found"}
	if c.keyProject != uuid.Nil && slug != c.keyProjectSlug {
		return nil, &apiError{http.StatusForbidden, "this API key belongs to project " + c.keyProjectSlug}
	}
	var p *store.Project
	err := h.db.InOrg(ctx, c.orgID, func(tx pgx.Tx) error {
		var err error
		p, err = store.GetProjectBySlug(ctx, tx, c.orgID, slug)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, notFound
	}
	if err != nil {
		return nil, &apiError{http.StatusInternalServerError, "internal error, try again"}
	}
	return p, nil
}

func decodeBody(r *http.Request, v any) *apiError {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return &apiError{http.StatusBadRequest, "invalid JSON body: " + err.Error()}
	}
	return nil
}

// info answers `sentry-cli info`.
func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, "")
	if e != nil {
		writeError(w, e)
		return
	}
	out := map[string]any{"version": "0", "auth": map[string]any{"scopes": c.scopes}, "user": nil}
	if c.user != nil {
		out["user"] = map[string]any{"id": c.user.ID.String(), "name": c.user.Name, "username": c.user.Email, "email": c.user.Email}
	}
	writeJSON(w, http.StatusOK, out)
}

// chunkOptions tells sentry-cli how to upload: chunk size, hashing,
// compression and which kinds of uploads izci accepts.
func (h *Handler) chunkOptions(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		// Relative: sentry-cli resolves it against the URL it was given.
		"url":              "/api/0/organizations/" + c.orgSlug + "/chunk-upload/",
		"chunkSize":        symbols.ChunkBytes,
		"chunksPerRequest": 4,
		// sentry-cli refuses larger files before uploading them.
		"maxFileSize":    h.symbols.MaxFileBytes(),
		"maxRequestSize": 32 << 20,
		"concurrency":    4,
		"hashAlgorithm":  "sha1",
		"compression":    []string{"gzip"},
		// release_files makes sentry-cli use chunked uploads when a release
		// is given; artifact_bundles(_v2) make those uploads bundles.
		"accept": []string{"debug_files", "release_files", "artifact_bundles", "artifact_bundles_v2", "dartsymbolmap"},
	})
}

// uploadChunks stores chunks sent as multipart form files named after their
// SHA-1 ("file", or "file_gzip" when compressed).
func (h *Handler) uploadChunks(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		writeError(w, &apiError{http.StatusBadRequest, "expected multipart/form-data"})
		return
	}
	mr := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxRequestBytes), params["boundary"])
	var chunks []store.Chunk
	total := 0
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, &apiError{http.StatusBadRequest, "invalid multipart body: " + err.Error()})
			return
		}
		name := part.FormName()
		if name != "file" && name != "file_gzip" {
			continue
		}
		var rd io.Reader = part
		if name == "file_gzip" {
			zr, err := gzip.NewReader(part)
			if err != nil {
				writeError(w, &apiError{http.StatusBadRequest, "invalid gzip chunk"})
				return
			}
			rd = zr
		}
		data, err := io.ReadAll(io.LimitReader(rd, symbols.ChunkBytes+1))
		if err != nil || len(data) > symbols.ChunkBytes {
			writeError(w, &apiError{http.StatusBadRequest, "a chunk is larger than the chunk size"})
			return
		}
		total += len(data)
		if total > 4*symbols.ChunkBytes+1 {
			writeError(w, &apiError{http.StatusRequestEntityTooLarge, "too many chunks in one request"})
			return
		}
		chunks = append(chunks, store.Chunk{Checksum: strings.ToLower(part.FileName()), Data: data})
	}
	if len(chunks) == 0 {
		writeError(w, &apiError{http.StatusBadRequest, "no chunks in the request"})
		return
	}
	if err := h.symbols.PutChunks(r.Context(), c.orgID, chunks); err != nil {
		if errors.Is(err, symbols.ErrChunkQuota) {
			w.Header().Set("Retry-After", "600")
			writeError(w, &apiError{http.StatusTooManyRequests, err.Error()})
			return
		}
		if strings.Contains(err.Error(), "checksum") {
			writeError(w, &apiError{http.StatusBadRequest, err.Error()})
			return
		}
		h.fail(w, "store chunks", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// assembleBundle assembles an artifact bundle (source maps).
func (h *Handler) assembleBundle(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	var body struct {
		Checksum string   `json:"checksum"`
		Chunks   []string `json:"chunks"`
		Projects []string `json:"projects"`
		Version  string   `json:"version"`
		Dist     string   `json:"dist"`
	}
	if e := decodeBody(r, &body); e != nil {
		writeError(w, e)
		return
	}
	if len(body.Projects) == 0 {
		writeError(w, &apiError{http.StatusBadRequest, "projects is required"})
		return
	}
	var ids []uuid.UUID
	for _, slug := range body.Projects {
		p, e := h.project(r.Context(), c, slug)
		if e != nil {
			writeError(w, e)
			return
		}
		ids = append(ids, p.ID)
	}
	res, err := h.symbols.AssembleBundle(r.Context(), c.orgID, ids, body.Checksum, body.Chunks, strings.TrimSpace(body.Version),
		strings.TrimSpace(body.Dist))
	if err != nil {
		h.fail(w, "assemble bundle", err)
		return
	}
	writeJSON(w, http.StatusOK, assembleResponse(res))
}

func assembleResponse(a symbols.Assembly) map[string]any {
	out := map[string]any{"state": a.State, "missingChunks": nonNil(a.Missing), "detail": nil}
	if a.Detail != "" {
		out["detail"] = a.Detail
	}
	return out
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// assembleDebugFiles assembles debug files and Dart obfuscation maps; the
// body maps each file's SHA-1 to its name, debug id and chunks.
func (h *Handler) assembleDebugFiles(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	p, e := h.project(r.Context(), c, r.PathValue("project"))
	if e != nil {
		writeError(w, e)
		return
	}
	var body map[string]struct {
		Name    string   `json:"name"`
		DebugID string   `json:"debug_id"`
		Chunks  []string `json:"chunks"`
	}
	if e := decodeBody(r, &body); e != nil {
		writeError(w, e)
		return
	}
	if len(body) > 256 {
		writeError(w, &apiError{http.StatusBadRequest, "too many files in one request"})
		return
	}
	out := map[string]any{}
	for checksum, f := range body {
		res, err := h.symbols.AssembleDebugFile(r.Context(), c.orgID, p.ID, checksum, f.Name, f.DebugID, f.Chunks)
		if err != nil {
			h.fail(w, "assemble debug file", err)
			return
		}
		item := assembleResponse(res)
		if res.File != nil {
			item["dif"] = debugFileJSON(res.File)
		}
		out[checksum] = item
	}
	writeJSON(w, http.StatusOK, out)
}

// debugFileJSON is a stored debug file the way sentry-cli prints it.
func debugFileJSON(f *store.DebugFile) map[string]any {
	cpu := f.Arch
	if cpu == "" {
		cpu = "any"
	}
	return map[string]any{
		"id": f.ID.String(), "uuid": f.DebugID.String(), "debugId": f.DebugID.String(), "codeId": f.CodeID,
		"objectName": f.Name, "cpuName": cpu, "sha1": f.Checksum, "size": f.SizeBytes,
		"dateCreated": f.CreatedAt.UTC().Format(time.RFC3339),
		"data":        map[string]any{"type": "dbg", "features": f.Features},
		"symbolType":  f.Kind,
	}
}
