package sentryapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/store"
)

// maxVersion bounds release names, as ingest does.
const maxVersion = 200

// releaseBody is what sentry-cli sends for `releases new`, `finalize` and
// `set-commits`.
type releaseBody struct {
	Version      string     `json:"version"`
	Projects     []string   `json:"projects"`
	Ref          string     `json:"ref"`
	URL          string     `json:"url"`
	DateReleased *time.Time `json:"dateReleased"`
	Refs         []struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
	} `json:"refs"`
	Commits []struct {
		ID         string `json:"id"`
		Repository string `json:"repository"`
	} `json:"commits"`
}

// releaseProjects returns the projects a release request is about: the one
// in the URL, or those listed in the body, or (updates) every project that
// has the version.
func (h *Handler) releaseProjects(r *http.Request, c *caller, listed []string, version string) ([]*store.Project, *apiError) {
	ctx := r.Context()
	if slug := r.PathValue("project"); slug != "" {
		listed = []string{slug}
	}
	if len(listed) == 0 && c.keyProject != uuid.Nil {
		listed = []string{c.keyProjectSlug}
	}
	if len(listed) == 0 && version != "" {
		var ids []uuid.UUID
		err := h.db.InOrg(ctx, c.orgID, func(tx pgx.Tx) error {
			var err error
			ids, err = store.ProjectsWithRelease(ctx, tx, c.orgID, version)
			return err
		})
		if err != nil {
			return nil, &apiError{http.StatusInternalServerError, "internal error, try again"}
		}
		var out []*store.Project
		for _, id := range ids {
			var p *store.Project
			if err := h.db.InOrg(ctx, c.orgID, func(tx pgx.Tx) error {
				var err error
				p, err = store.GetProject(ctx, tx, id)
				return err
			}); err == nil {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			return nil, &apiError{http.StatusNotFound, "release " + version + " not found"}
		}
		return out, nil
	}
	if len(listed) == 0 {
		return nil, &apiError{http.StatusBadRequest, "projects is required"}
	}
	var out []*store.Project
	for _, slug := range listed {
		p, e := h.project(ctx, c, slug)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}

// register creates or updates the release in each project.
func (h *Handler) register(ctx context.Context, c *caller, projects []*store.Project, in store.ReleaseInput) error {
	return h.db.InOrg(ctx, c.orgID, func(tx pgx.Tx) error {
		for _, p := range projects {
			in.OrgID, in.ProjectID = c.orgID, p.ID
			if _, _, err := store.RegisterRelease(ctx, tx, in); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *releaseBody) input(version string) store.ReleaseInput {
	in := store.ReleaseInput{Version: version, Ref: clip(b.Ref, 200), URL: clip(b.URL, 2000), ReleasedAt: time.Now().UTC()}
	for _, r := range b.Refs {
		if r.Commit != "" {
			in.CommitSHA, in.Repository = strings.ToLower(clip(r.Commit, 64)), clip(r.Repository, 300)
		}
	}
	if in.CommitSHA == "" && len(b.Commits) > 0 {
		last := b.Commits[len(b.Commits)-1]
		in.CommitSHA, in.Repository = strings.ToLower(clip(last.ID, 64)), clip(last.Repository, 300)
	}
	return in
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func validVersion(v string) *apiError {
	if v == "" || len(v) > maxVersion || strings.ContainsAny(v, "\n\r\t/") || v == "." || v == ".." || v == "latest" {
		return &apiError{http.StatusBadRequest, "invalid release version"}
	}
	return nil
}

// createRelease serves `sentry-cli releases new`.
func (h *Handler) createRelease(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	var body releaseBody
	if e := decodeBody(r, &body); e != nil {
		writeError(w, e)
		return
	}
	version := strings.TrimSpace(body.Version)
	if e := validVersion(version); e != nil {
		writeError(w, e)
		return
	}
	projects, e := h.releaseProjects(r, c, body.Projects, "")
	if e != nil {
		writeError(w, e)
		return
	}
	if err := h.register(r.Context(), c, projects, body.input(version)); err != nil {
		h.fail(w, "create release", err)
		return
	}
	h.writeRelease(w, r, c, projects, version, http.StatusCreated)
}

// getRelease serves `sentry-cli releases info`.
func (h *Handler) getRelease(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	version := r.PathValue("version")
	projects, e := h.releaseProjects(r, c, nil, version)
	if e != nil {
		writeError(w, e)
		return
	}
	h.writeRelease(w, r, c, projects, version, http.StatusOK)
}

// updateRelease serves `releases finalize` and `releases set-commits`.
func (h *Handler) updateRelease(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	version := r.PathValue("version")
	if e := validVersion(version); e != nil {
		writeError(w, e)
		return
	}
	var body releaseBody
	if e := decodeBody(r, &body); e != nil {
		writeError(w, e)
		return
	}
	projects, e := h.releaseProjects(r, c, body.Projects, version)
	if e != nil {
		writeError(w, e)
		return
	}
	if err := h.register(r.Context(), c, projects, body.input(version)); err != nil {
		h.fail(w, "update release", err)
		return
	}
	h.writeRelease(w, r, c, projects, version, http.StatusOK)
}

// createDeploy serves `sentry-cli deploys new`.
func (h *Handler) createDeploy(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	version := r.PathValue("version")
	var body struct {
		Environment  string     `json:"environment"`
		Name         string     `json:"name"`
		URL          string     `json:"url"`
		DateStarted  *time.Time `json:"dateStarted"`
		DateFinished *time.Time `json:"dateFinished"`
		Projects     []string   `json:"projects"`
	}
	if e := decodeBody(r, &body); e != nil {
		writeError(w, e)
		return
	}
	env := strings.ToLower(clip(body.Environment, 64))
	if env == "" {
		writeError(w, &apiError{http.StatusBadRequest, "environment is required"})
		return
	}
	projects, e := h.releaseProjects(r, c, body.Projects, version)
	if e != nil {
		writeError(w, e)
		return
	}
	at := time.Now().UTC()
	if body.DateFinished != nil && body.DateFinished.Before(at.Add(time.Hour)) {
		at = body.DateFinished.UTC()
	}
	by := clip(body.Name, 100)
	if by == "" && c.user != nil {
		by = c.user.Email
	}
	in := store.ReleaseInput{Version: version, Environment: env, ReleasedAt: at, ReleasedBy: by, URL: clip(body.URL, 2000)}
	if err := h.register(r.Context(), c, projects, in); err != nil {
		h.fail(w, "create deploy", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": uuid.NewString(), "environment": env, "name": body.Name,
		"url": body.URL, "dateStarted": body.DateStarted, "dateFinished": at.Format(time.RFC3339)})
}

// listRepos serves the repository lookup of `releases set-commits`: the
// repositories linked to the organization's projects.
func (h *Handler) listRepos(w http.ResponseWriter, r *http.Request) {
	c, e := h.authenticate(r, r.PathValue("org"))
	if e != nil {
		writeError(w, e)
		return
	}
	var repos []map[string]any
	err := h.db.InOrg(r.Context(), c.orgID, func(tx pgx.Tx) error {
		projects, err := store.ListProjects(r.Context(), tx, c.orgID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, p := range projects {
			repo, err := store.GetRepository(r.Context(), tx, p.ID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			name := repo.Owner + "/" + repo.Name
			if seen[name] {
				continue
			}
			seen[name] = true
			repos = append(repos, map[string]any{"id": p.ID.String(), "name": name, "url": repo.URL, "status": "active",
				"provider": map[string]string{"id": "integrations:github", "name": "GitHub"}})
		}
		return nil
	})
	if err != nil {
		h.fail(w, "list repositories", err)
		return
	}
	if repos == nil {
		repos = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, repos)
}

// writeRelease answers with the release as sentry-cli reads it.
func (h *Handler) writeRelease(w http.ResponseWriter, r *http.Request, c *caller, projects []*store.Project, version string, status int) {
	var infos []*store.ReleaseInfo
	err := h.db.InOrg(r.Context(), c.orgID, func(tx pgx.Tx) error {
		for _, p := range projects {
			info, err := store.GetReleaseInfo(r.Context(), tx, p.ID, version)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			infos = append(infos, info)
		}
		return nil
	})
	if err != nil {
		h.fail(w, "read release", err)
		return
	}
	if len(infos) == 0 {
		writeError(w, &apiError{http.StatusNotFound, "release " + version + " not found"})
		return
	}
	first := infos[0]
	out := map[string]any{"version": version, "shortVersion": version, "ref": nil, "url": nil,
		"dateCreated": first.CreatedAt.UTC().Format(time.RFC3339), "dateReleased": nil, "lastEvent": nil, "newGroups": 0,
		"projects": []map[string]string{}, "deployCount": 0, "lastDeploy": nil}
	projectsOut := []map[string]string{}
	newGroups, deploys := 0, 0
	for _, info := range infos {
		projectsOut = append(projectsOut, map[string]string{"slug": info.ProjectSlug, "name": info.ProjectName})
		newGroups += info.NewIssues
		deploys += info.Deploys
		if info.LastDeploy != nil {
			out["lastDeploy"] = map[string]any{"id": info.ID.String(), "environment": info.LastEnv,
				"dateFinished": info.LastDeploy.UTC().Format(time.RFC3339), "name": nil, "url": nil}
		}
	}
	out["projects"], out["newGroups"], out["deployCount"] = projectsOut, newGroups, deploys
	if first.URL != "" {
		out["url"] = first.URL
	}
	if first.CommitSHA != "" {
		out["lastCommit"] = map[string]any{"id": first.CommitSHA, "repository": map[string]string{"name": first.Repository}}
	}
	writeJSON(w, status, out)
}
