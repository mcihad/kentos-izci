package ai

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

// ErrInvalidBranch rejects a branch name git would not accept.
var ErrInvalidBranch = errors.New("invalid branch name")

// PublishError is a git failure while pushing a change, e.g. a branch that
// exists with other commits or a patch that no longer applies.
type PublishError struct{ Err error }

func (e *PublishError) Error() string { return e.Err.Error() }

func (e *PublishError) Unwrap() error { return e.Err }

// BranchOptions describe the branch a run's change is pushed to.
type BranchOptions struct {
	// Name defaults to "izci/fix/<short-id>-<slug>-<run>".
	Name string
	// AllowSensitive confirms a change to files CI, build tools or git run
	// (see SensitiveFiles); without it such a change is refused.
	AllowSensitive bool
	// automatic publishing: never sensitive, "izci-auto/" branches.
	automatic bool
}

// PullRequestOptions describe a pull request made from a run.
type PullRequestOptions struct {
	Draft bool
	// Title defaults to "fix: <issue title> (<short id>)".
	Title string
	// Base defaults to the branch the analysis ran on.
	Base string
	// AllowSensitive is as for BranchOptions.
	AllowSensitive bool
	// automatic publishing: always a draft, see BranchOptions.
	automatic bool
}

// AutoBranchPrefix starts the branches of automatic publishing, so that CI
// can be told not to build them (they carry code influenced by error data
// from outside).
const AutoBranchPrefix = "izci-auto/"

// publication is everything needed to push a run's change.
type publication struct {
	run       *store.AiRun
	repo      *store.Repository
	client    *github.Client
	shortID   string
	title     string
	permalink string
	timesSeen int64
	users     int32
	customers int32
	lastSeen  time.Time
}

// CreateBranch pushes the proposed change of a run to a new branch.
// Calling it again returns the existing branch.
func (r *Runner) CreateBranch(ctx context.Context, orgID, runID uuid.UUID, o BranchOptions, actor *uuid.UUID) (*store.PullRequestInfo, error) {
	p, err := r.publication(ctx, orgID, runID)
	if err != nil {
		return nil, err
	}
	if pr := p.run.PullRequest; pr != nil {
		return pr, nil
	}
	if err := checkSensitive(p.run, o.AllowSensitive && !o.automatic); err != nil {
		return nil, err
	}
	info, err := r.pushBranch(ctx, p, o.Name, o.automatic)
	if err != nil {
		return nil, err
	}
	return info, r.recordPublication(ctx, p, info, store.ActivityBranchCreated, actor)
}

// CreatePullRequest pushes the change (unless a branch exists already) and
// opens a pull request. Calling it again returns the existing one.
func (r *Runner) CreatePullRequest(ctx context.Context, orgID, runID uuid.UUID, o PullRequestOptions, actor *uuid.UUID) (*store.PullRequestInfo, error) {
	p, err := r.publication(ctx, orgID, runID)
	if err != nil {
		return nil, err
	}
	info := p.run.PullRequest
	if info != nil && info.Number > 0 {
		return info, nil
	}
	if o.automatic {
		o.Draft = true
	}
	if info == nil {
		if err := checkSensitive(p.run, o.AllowSensitive && !o.automatic); err != nil {
			return nil, err
		}
		if info, err = r.pushBranch(ctx, p, "", o.automatic); err != nil {
			return nil, err
		}
		// Keep the pushed branch even if opening the pull request fails.
		if err := r.recordPublication(ctx, p, info, store.ActivityBranchCreated, actor); err != nil {
			return nil, err
		}
	}
	title := strings.TrimSpace(o.Title)
	if title == "" {
		title = clip(fmt.Sprintf("fix: %s (%s)", p.title, p.shortID), 250)
	}
	base := firstNonEmpty(o.Base, p.run.BaseBranch, p.repo.DefaultBranch)
	pr, err := p.client.CreatePullRequest(ctx, p.repo.Owner, p.repo.Name, github.NewPullRequest{
		Title: title, Body: pullRequestBody(p, o.automatic), Head: info.Branch, Base: base, Draft: o.Draft})
	if err != nil {
		return nil, fmt.Errorf("open pull request: %w", err)
	}
	info = &store.PullRequestInfo{Number: pr.Number, URL: pr.HTMLURL, Title: pr.Title, Branch: info.Branch, Draft: pr.Draft, State: "open"}
	return info, r.recordPublication(ctx, p, info, store.ActivityPullRequest, actor)
}

func (r *Runner) publication(ctx context.Context, orgID, runID uuid.UUID) (*publication, error) {
	p := &publication{}
	var integration *store.Integration
	err := r.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var err error
		if p.run, err = store.GetAiRun(ctx, tx, runID); err != nil {
			return err
		}
		if p.run.Status != store.AiSucceeded || p.run.Diff == "" || p.run.BaseCommit == "" {
			return ErrNotReady
		}
		var seq int32
		var slug string
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(i.short_seq, 0), p.slug, o.slug, i.title, i.times_seen, i.users_count, i.customers_count, i.last_seen
			FROM issues i JOIN projects p ON p.id = i.project_id JOIN organizations o ON o.id = i.org_id WHERE i.id = $1`,
			p.run.IssueID).Scan(&seq, &slug, &p.permalink, &p.title, &p.timesSeen, &p.users, &p.customers, &p.lastSeen); err != nil {
			return err
		}
		p.shortID = ingest.ShortID(slug, seq)
		p.permalink = r.opts.PublicURL + "/o/" + p.permalink + "/issues/" + p.shortID
		p.repo, err = store.GetRepository(ctx, tx, p.run.ProjectID)
		if errors.Is(err, store.ErrNotFound) || err == nil && p.repo.IntegrationID == nil {
			return ErrNoRepository
		}
		if err != nil {
			return err
		}
		integration, err = store.GetIntegration(ctx, tx, *p.repo.IntegrationID)
		if errors.Is(err, store.ErrNotFound) {
			return ErrNoRepository
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if r.opts.GitHub == nil {
		return nil, github.ErrAppNotConfigured
	}
	if p.client, err = r.opts.GitHub.Client(integration); err != nil {
		return nil, err
	}
	return p, nil
}

// checkSensitive refuses a change to sensitive files unless confirmed.
func checkSensitive(run *store.AiRun, confirmed bool) error {
	if files := SensitiveFiles(changedPaths(run.Files)); len(files) > 0 && !confirmed {
		return &SensitiveChangesError{Files: files}
	}
	return nil
}

// pushBranch commits the run's patch in the mirror and pushes it.
func (r *Runner) pushBranch(ctx context.Context, p *publication, branch string, automatic bool) (*store.PullRequestInfo, error) {
	if branch = strings.TrimSpace(branch); branch == "" || automatic {
		branch = defaultBranch(p.shortID, p.title, p.run.ID)
		if automatic {
			branch = AutoBranchPrefix + strings.TrimPrefix(branch, "izci/")
		}
	}
	if !validBranch(branch) {
		return nil, ErrInvalidBranch
	}
	remote := remoteURL(p.repo.URL)
	if err := r.checkRemote(remote); err != nil {
		return nil, err
	}
	token, err := p.client.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("GitHub token: %w", err)
	}
	auth := gitAuth{token: token}
	unlock := r.lockMirror(p.run.ProjectID)
	defer unlock()
	mirror, err := r.ws.sync(ctx, p.run.ProjectID, remote, auth)
	if err != nil {
		return nil, &PublishError{fmt.Errorf("sync repository: %w", err)}
	}
	when := p.run.CreatedAt
	if p.run.FinishedAt != nil {
		when = *p.run.FinishedAt
	}
	message := fmt.Sprintf("fix: %s\n\nProposed by the izci AI analysis of %s.\n\nIssue: %s\nRun: %s\n",
		clip(firstLine(p.title), 64), p.shortID, p.permalink, p.run.ID)
	id := identity{name: r.opts.Config.GitAuthorName, email: r.opts.Config.GitAuthorEmail, when: when}
	commit, err := r.ws.commitPatch(ctx, mirror, p.run.BaseCommit, []byte(p.run.Diff), message, id)
	if err != nil {
		return nil, &PublishError{err}
	}
	if err := push(ctx, mirror, remote, commit, branch, auth); err != nil {
		return nil, &PublishError{err}
	}
	web := strings.TrimSuffix(strings.TrimSuffix(p.repo.URL, "/"), ".git")
	return &store.PullRequestInfo{URL: web + "/tree/" + branch, Branch: branch, State: "branch"}, nil
}

func (r *Runner) recordPublication(ctx context.Context, p *publication, info *store.PullRequestInfo, kind int16, actor *uuid.UUID) error {
	err := r.db.InOrg(ctx, p.run.OrgID, func(tx pgx.Tx) error {
		if err := store.SetAiRunPullRequest(ctx, tx, p.run.ID, info); err != nil {
			return err
		}
		data := map[string]string{"run_id": p.run.ID.String(), "branch": info.Branch, "url": info.URL}
		if info.Number > 0 {
			data["number"] = fmt.Sprint(info.Number)
		}
		if err := store.AddIssueActivity(ctx, tx, p.run.OrgID, []uuid.UUID{p.run.IssueID}, kind, actor, data); err != nil {
			return err
		}
		return store.RefreshIssueAiState(ctx, tx, p.run.IssueID)
	})
	if err != nil {
		return err
	}
	p.run.PullRequest = info
	r.ping(p.run.ID)
	r.issueChanged(ctx, p.run.OrgID, p.run.IssueID)
	return nil
}

// pullRequestBody is the (Turkish) description of a pull request.
func pullRequestBody(p *publication, automatic bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## İzci yapay zekâ analizi\n\n**Hata:** [%s](%s) — %s\n\n", p.shortID, p.permalink, escapeMarkdown(p.title))
	if automatic {
		b.WriteString("> **Otomatik açıldı.** Bu değişikliği kimse istemedi; yeni bir hata kaydı üzerine kendiliğinden hazırlandı. " +
			"Hata kayıtları uygulamanın dışından gelir ve yapay zekâyı yanıltmaya çalışan metin içerebilir: değişikliği satır satır " +
			"inceleyin, CI'ın bu dalda sırlarla çalışmadığından emin olun.\n\n")
	}
	fmt.Fprintf(&b, "**Etki:** %d olay · %d kullanıcı · %d müşteri · son görülme %s\n\n",
		p.timesSeen, p.users, p.customers, p.lastSeen.UTC().Format("2006-01-02 15:04 UTC"))
	res := p.run.Result
	var meta []string
	if res.Confidence > 0 {
		meta = append(meta, fmt.Sprintf("**Güven:** %%%d", int(res.Confidence*100+0.5)))
	}
	if label := map[string]string{"low": "düşük", "medium": "orta", "high": "yüksek"}[res.Risk]; label != "" {
		meta = append(meta, "**Risk:** "+label)
	}
	if len(meta) > 0 {
		b.WriteString(strings.Join(meta, " · ") + "\n\n")
	}
	b.WriteString(clip(p.run.Analysis, 50000))
	fmt.Fprintf(&b, "\n\n---\n\nBu değişikliği izci yapay zekâ ile önerdi. Birleştirmeden önce dikkatle inceleyin ve testleri çalıştırın. Koşu: `%s`\n", p.run.ID)
	return b.String()
}

var branchSlug = regexp.MustCompile(`[^a-z0-9]+`)

// defaultBranch names a branch after the issue: izci/fix/kalem-7-nil-map-write.
func defaultBranch(shortID, title string, runID uuid.UUID) string {
	fold := strings.NewReplacer("ç", "c", "ğ", "g", "ı", "i", "ö", "o", "ş", "s", "ü", "u", "İ", "i").Replace(strings.ToLower(title))
	slug := strings.Trim(branchSlug.ReplaceAllString(fold, "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	name := "izci/fix/" + strings.ToLower(shortID)
	if slug != "" {
		name += "-" + slug
	}
	return name + "-" + runID.String()[:8]
}

// validBranch follows git check-ref-format for branch names.
func validBranch(name string) bool {
	if name == "" || len(name) > 200 || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") || strings.Contains(name, "..") ||
		strings.Contains(name, "//") || strings.Contains(name, "@{") || name == "@" {
		return false
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f || strings.ContainsRune(" ~^:?*[\\", c) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

var markdownSpecial = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", "&lt;", ">", "&gt;")

func escapeMarkdown(s string) string { return markdownSpecial.Replace(firstLine(s)) }
