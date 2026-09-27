package ownership

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// ErrNotConfigured means the project has no repository linked through a
// GitHub connection, so there is nothing to blame.
var ErrNotConfigured = errors.New("project has no repository linked through GitHub")

// blameFrames is how many of the innermost resolvable frames are blamed.
const blameFrames = 3

// repoTree is the file list of a repository at one ref.
type repoTree struct {
	files  map[string]bool
	byBase map[string][]string
}

// suspectInput is what a search needs, loaded in the tenant's scope.
type suspectInput struct {
	event       *ingestv1.Event
	repo        *store.Repository
	integration *store.Integration
	ref         string
}

func (s *Service) loadSuspectInput(ctx context.Context, orgID, issueID uuid.UUID) (*suspectInput, error) {
	in := &suspectInput{}
	err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1`, issueID).Scan(&projectID); err != nil {
			return err
		}
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT payload FROM events WHERE issue_id = $1 ORDER BY received_at DESC LIMIT 1`, issueID).Scan(&payload)
		if err != nil {
			return err
		}
		in.event = &ingestv1.Event{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, in.event); err != nil {
			return err
		}
		in.repo, err = store.GetRepository(ctx, tx, projectID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && in.repo.IntegrationID == nil) {
			return ErrNotConfigured
		}
		if err != nil {
			return err
		}
		if in.integration, err = store.GetIntegration(ctx, tx, *in.repo.IntegrationID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrNotConfigured
			}
			return err
		}
		in.ref = in.repo.DefaultBranch
		if rel := in.event.GetRelease(); rel != "" {
			var sha string
			err := tx.QueryRow(ctx, `SELECT commit_sha FROM releases WHERE project_id = $1 AND version = $2`, projectID, rel).Scan(&sha)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if sha != "" {
				in.ref = sha
			}
		}
		if in.ref == "" {
			in.ref = "HEAD"
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return in, err
}

// FindSuspectCommit blames the innermost in-app frames of an issue's
// latest event in its linked GitHub repository, stores the most recent
// commit among them and returns it. The release's commit is blamed when
// known, the default branch otherwise. ErrNotConfigured means there is no
// repository to look in.
func (s *Service) FindSuspectCommit(ctx context.Context, orgID, issueID uuid.UUID) (*store.SuspectCommit, error) {
	in, err := s.loadSuspectInput(ctx, orgID, issueID)
	if err != nil {
		return nil, err
	}
	res := &store.SuspectCommit{IssueID: issueID, OrgID: orgID}
	if err := s.blame(ctx, in, res); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		res.State, res.Detail = store.SuspectFailed, truncate(err.Error(), 500)
	}
	err = s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error { return store.SaveSuspectCommit(ctx, tx, res) })
	return res, err
}

func (s *Service) blame(ctx context.Context, in *suspectInput, res *store.SuspectCommit) error {
	if s.github == nil {
		return ErrNotConfigured
	}
	client, err := s.github.Client(in.integration)
	if err != nil {
		return err
	}
	var rewrites []Rewrite
	_ = json.Unmarshal(in.repo.PathMappings, &rewrites)
	frames := lineFrames(in.event)
	if len(frames) == 0 {
		res.State = store.SuspectNoFrames
		return nil
	}
	tree, err := s.tree(ctx, client, in.repo.Owner, in.repo.Name, in.ref)
	if err != nil {
		return fmt.Errorf("list repository files: %w", err)
	}
	type blamed struct {
		path   string
		line   int
		commit github.Commit
	}
	var found []blamed
	seen := map[string]bool{}
	for _, f := range frames {
		if len(found) == blameFrames {
			break
		}
		p := tree.resolve(f.paths, rewrites)
		key := fmt.Sprintf("%s:%d", p, f.line)
		if p == "" || seen[key] {
			continue
		}
		seen[key] = true
		ranges, err := client.Blame(ctx, in.repo.Owner, in.repo.Name, in.ref, p)
		if github.Forbidden(err) {
			continue // not at this ref after all
		}
		if err != nil {
			return fmt.Errorf("blame %s: %w", p, err)
		}
		for _, r := range ranges {
			if f.line >= r.Start && f.line <= r.End {
				found = append(found, blamed{p, f.line, r.Commit})
				break
			}
		}
	}
	if len(found) == 0 {
		res.State = store.SuspectNotInRepository
		return nil
	}
	// The most recent change among the innermost frames; the innermost
	// frame wins ties.
	best := found[0]
	for _, b := range found[1:] {
		if b.commit.CommittedAt.After(best.commit.CommittedAt) {
			best = b
		}
	}
	c := best.commit
	res.State, res.SHA, res.Message, res.URL = store.SuspectFound, c.SHA, truncate(c.Message, 300), c.URL
	res.AuthorName, res.AuthorEmail, res.AuthorLogin = c.AuthorName, c.AuthorEmail, c.AuthorLogin
	res.Path, res.Line = best.path, int32(best.line)
	if !c.CommittedAt.IsZero() {
		t := c.CommittedAt
		res.CommittedAt = &t
	}
	return nil
}

type lineFrame struct {
	paths []string
	line  int
}

// lineFrames are the in-app frames with line numbers, innermost first
// (all frames when none is in-app).
func lineFrames(ev *ingestv1.Event) []lineFrame {
	var all, app []lineFrame
	add := func(st *ingestv1.Stacktrace) {
		frames := st.GetFrames()
		for i := len(frames) - 1; i >= 0; i-- {
			f := frames[i]
			if f.GetLineno() == 0 {
				continue
			}
			var paths []string
			for _, p := range []string{f.GetAbsPath(), f.GetFilename()} {
				if p = NormalizePath(p); p != "" && !strings.Contains(p, "..") {
					paths = append(paths, p)
				}
			}
			if len(paths) == 0 {
				continue
			}
			lf := lineFrame{paths: paths, line: int(f.GetLineno())}
			all = append(all, lf)
			if f.GetInApp() {
				app = append(app, lf)
			}
		}
	}
	exceptions := ev.GetExceptions()
	for i := len(exceptions) - 1; i >= 0; i-- {
		add(exceptions[i].GetStacktrace())
	}
	for _, th := range ev.GetThreads() {
		if th.GetCrashed() || len(exceptions) == 0 {
			add(th.GetStacktrace())
		}
	}
	if len(app) > 0 {
		return app
	}
	return all
}

func (s *Service) tree(ctx context.Context, client *github.Client, owner, name, ref string) (*repoTree, error) {
	key := owner + "/" + name + "@" + ref
	if t, ok := s.trees.Get(key); ok {
		return t, nil
	}
	files, _, err := client.Tree(ctx, owner, name, ref)
	if err != nil {
		return nil, err
	}
	t := &repoTree{files: make(map[string]bool, len(files)), byBase: map[string][]string{}}
	for _, f := range files {
		t.files[f] = true
		base := path.Base(f)
		t.byBase[base] = append(t.byBase[base], f)
	}
	s.trees.Add(key, t)
	return t, nil
}

// resolve finds the repository file of a frame: a path mapping or suffix
// that exists, otherwise the only file with the same name and the longest
// common ending.
func (t *repoTree) resolve(paths []string, rewrites []Rewrite) string {
	for _, p := range paths {
		for _, c := range RepoPaths(p, rewrites) {
			if t.files[c] {
				return c
			}
		}
	}
	for _, p := range paths {
		candidates := t.byBase[path.Base(p)]
		best, bestLen, tie := "", 0, false
		for _, c := range candidates {
			n := commonSuffix(p, c)
			switch {
			case n > bestLen:
				best, bestLen, tie = c, n, false
			case n == bestLen:
				tie = true
			}
		}
		if best != "" && !tie {
			return best
		}
	}
	return ""
}

// commonSuffix counts the path segments two paths end with.
func commonSuffix(a, b string) int {
	as, bs := strings.Split(strings.Trim(a, "/"), "/"), strings.Split(strings.Trim(b, "/"), "/")
	n := 0
	for n < len(as) && n < len(bs) && strings.EqualFold(as[len(as)-1-n], bs[len(bs)-1-n]) {
		n++
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// newTreeCache keeps repository file lists for a while: branches move, a
// release commit does not, and a few minutes spare the API.
func newTreeCache() *expirable.LRU[string, *repoTree] {
	return expirable.NewLRU[string, *repoTree](16, nil, 10*time.Minute)
}
