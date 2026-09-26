package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/store"
)

// Limits of a proposed change.
const (
	maxPatchBytes   = 4 << 20
	maxChangedFile  = 1 << 20
	maxChangedTotal = 8 << 20
)

// excluded are paths never taken into a proposed change.
var excluded = []string{":(exclude).izci", ":(exclude).opencode"}

// workspace keeps bare mirrors of linked repositories and per-run
// checkouts under the data directory.
type workspace struct {
	root string
}

func (w workspace) mirrorDir(projectID uuid.UUID) string {
	return filepath.Join(w.root, "repos", projectID.String()+".git")
}

func (w workspace) runDir(runID uuid.UUID) string {
	return filepath.Join(w.root, "work", runID.String())
}

// sync creates the bare mirror of a repository or fetches its updates.
func (w workspace) sync(ctx context.Context, projectID uuid.UUID, remote string, auth gitAuth) (string, error) {
	dir := w.mirrorDir(projectID)
	g := bare(dir, auth)
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", err
		}
		if _, err := g.run(ctx, "init", "--quiet", "--bare"); err != nil {
			return "", err
		}
		if _, err := g.run(ctx, "remote", "add", "origin", remote); err != nil {
			return "", err
		}
	} else if _, err := g.run(ctx, "remote", "set-url", "origin", remote); err != nil {
		// The repository may have been relinked to another remote.
		return "", err
	}
	g.limit = 16 << 20
	_, err := g.run(ctx, "fetch", "--prune", "--quiet", "--no-tags", "origin", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	return dir, err
}

// resolve returns the commit to analyze: the release commit when the
// mirror has it, else the head of the default branch.
func resolve(ctx context.Context, mirror, releaseCommit, branch string) (commit, usedBranch string, err error) {
	g := bare(mirror, gitAuth{})
	if releaseCommit != "" && !strings.HasPrefix(releaseCommit, "-") {
		if c, err := g.text(ctx, "rev-parse", "--verify", "--quiet", releaseCommit+"^{commit}"); err == nil && c != "" {
			return c, branch, nil
		}
	}
	for _, b := range []string{branch, "main", "master"} {
		if b == "" {
			continue
		}
		if c, err := g.text(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err == nil && c != "" {
			return c, b, nil
		}
	}
	first, err := g.text(ctx, "for-each-ref", "--count=1", "--format=%(refname:short) %(objectname)", "refs/heads")
	if b, c, ok := strings.Cut(first, " "); err == nil && ok {
		return c, b, nil
	}
	return "", "", errors.New("the repository has no branch to analyze")
}

// checkout is a run's working copy. gitDir is recorded right after the
// checkout is created, before the agent can touch the directory.
type checkout struct {
	dir    string
	gitDir string
	mirror string
	base   string
}

func (c checkout) git() gitCmd { return gitCmd{gitDir: c.gitDir, workTree: c.dir} }

// addWorktree checks out commit into the run's directory.
func (w workspace) addWorktree(ctx context.Context, mirror string, runID uuid.UUID, commit string) (*checkout, error) {
	dir := w.runDir(runID)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return nil, err
	}
	if _, err := bare(mirror, gitAuth{}).run(ctx, "worktree", "add", "--quiet", "--detach", "--force", dir, commit); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return nil, fmt.Errorf("read worktree link: %w", err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	if !ok {
		return nil, errors.New("unexpected worktree link")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	// A committed .izci entry (or a symlink by that name) must not redirect
	// the files izci writes and reads.
	if err := os.RemoveAll(filepath.Join(dir, ".izci")); err != nil {
		return nil, err
	}
	return &checkout{dir: dir, gitDir: filepath.Clean(gitDir), mirror: mirror, base: commit}, nil
}

// removeCheckout deletes a run's working copy.
func (w workspace) removeCheckout(ctx context.Context, mirror string, runID uuid.UUID) {
	dir := w.runDir(runID)
	if mirror != "" {
		g := bare(mirror, gitAuth{})
		_, _ = g.run(ctx, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
		_, _ = g.run(ctx, "worktree", "prune")
		return
	}
	_ = os.RemoveAll(dir)
}

// scratch creates a plain directory for runs without a repository.
func (w workspace) scratch(runID uuid.UUID) (string, error) {
	dir := w.runDir(runID)
	_ = os.RemoveAll(dir)
	return dir, os.MkdirAll(dir, 0o750)
}

// changes stages what the agent changed and returns it as a binary-safe
// patch against the base commit.
func (c checkout) changes(ctx context.Context) ([]byte, []store.ChangedFile, error) {
	g := c.git()
	status, err := g.run(ctx, append([]string{"status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--", "."}, excluded...)...)
	if err != nil {
		return nil, nil, err
	}
	var total int64
	for _, rec := range bytes.Split(status, []byte{0}) {
		if len(rec) < 4 || rec[0] == 'D' || rec[1] == 'D' {
			continue
		}
		path := string(rec[3:])
		st, err := os.Lstat(filepath.Join(c.dir, filepath.FromSlash(path)))
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if st.Size() > maxChangedFile {
			return nil, nil, fmt.Errorf("the agent wrote %s (%d KiB); files over 1 MiB are not accepted in a fix", path, st.Size()>>10)
		}
		if total += st.Size(); total > maxChangedTotal {
			return nil, nil, errors.New("the changed files exceed 8 MiB")
		}
	}
	if len(bytes.TrimSpace(status)) == 0 {
		return nil, nil, nil
	}
	if _, err := g.run(ctx, append([]string{"add", "--all", "--", "."}, excluded...)...); err != nil {
		return nil, nil, err
	}
	g.limit = maxPatchBytes
	patch, err := g.run(ctx, "diff", "--cached", "--binary", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", c.base, "--")
	if errors.Is(err, errOutputTooLarge) {
		return nil, nil, errors.New("the proposed change is larger than 4 MiB")
	}
	if err != nil || len(patch) == 0 {
		return nil, nil, err
	}
	counts, err := g.run(ctx, "diff", "--cached", "--numstat", "-z", "--no-renames", c.base, "--")
	if err != nil {
		return nil, nil, err
	}
	names, err := g.run(ctx, "diff", "--cached", "--name-status", "-z", "--no-renames", c.base, "--")
	if err != nil {
		return nil, nil, err
	}
	n := numstat(counts)
	paths, kinds := nameStatus(names)
	files := make([]store.ChangedFile, 0, len(paths))
	for _, p := range paths {
		files = append(files, store.ChangedFile{Path: p, Additions: n[p][0], Deletions: n[p][1], Status: kinds[p]})
	}
	return patch, files, nil
}

// identity is the author of commits made by izci.
type identity struct {
	name, email string
	when        time.Time
}

func (id identity) env() []string {
	date := id.when.UTC().Format(time.RFC3339)
	return []string{"GIT_AUTHOR_NAME=" + id.name, "GIT_AUTHOR_EMAIL=" + id.email, "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + id.name, "GIT_COMMITTER_EMAIL=" + id.email, "GIT_COMMITTER_DATE=" + date}
}

// commitPatch creates a commit of patch on top of base inside the mirror,
// without a working tree: the patch is applied to a temporary index. The
// same inputs give the same commit, so a branch can be pushed again.
func (w workspace) commitPatch(ctx context.Context, mirror, base string, patch []byte, message string, id identity) (string, error) {
	tmp, err := os.MkdirTemp(filepath.Dir(mirror), ".index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	g := bare(mirror, gitAuth{})
	g.index = filepath.Join(tmp, "index")
	if _, err := g.run(ctx, "read-tree", base); err != nil {
		return "", err
	}
	g.stdin = bytes.NewReader(patch)
	if _, err := g.run(ctx, "apply", "--cached", "--whitespace=nowarn", "-"); err != nil {
		return "", fmt.Errorf("the proposed change no longer applies: %w", err)
	}
	g.stdin = nil
	tree, err := g.text(ctx, "write-tree")
	if err != nil {
		return "", err
	}
	g.env, g.stdin = id.env(), strings.NewReader(message)
	return g.text(ctx, "commit-tree", tree, "-p", base)
}

// push publishes commit as a new branch of the remote. It never forces:
// an existing branch with other commits makes it fail.
func push(ctx context.Context, mirror, remote, commit, branch string, auth gitAuth) error {
	_, err := bare(mirror, auth).run(ctx, "push", "--quiet", "--no-verify", remote, commit+":refs/heads/"+branch)
	if err != nil && strings.Contains(err.Error(), "rejected") {
		return fmt.Errorf("branch %s already exists with other commits", branch)
	}
	return err
}

// recentCommits lists the latest commits touching paths, newest first.
func recentCommits(ctx context.Context, mirror, commit string, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	out, err := bare(mirror, gitAuth{}).text(ctx, append([]string{"log", "-n", "15", "--no-merges", "--date=short",
		"--format=%h %ad %an: %s", commit, "--"}, paths...)...)
	if err != nil {
		return ""
	}
	return out
}

// remoteURL is the git URL of a linked repository.
func remoteURL(webURL string) string {
	if strings.HasPrefix(webURL, "file://") || strings.HasPrefix(webURL, "/") || strings.HasSuffix(webURL, ".git") {
		return webURL
	}
	return strings.TrimRight(webURL, "/") + ".git"
}

func ensureDir(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	return nil
}
