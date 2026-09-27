package ownership

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Service applies ownership: owners of issues, automatic assignment and
// suspect commits.
type Service struct {
	db      *db.DB
	log     *slog.Logger
	github  *github.Access
	rules   *expirable.LRU[string, *Ruleset]
	trees   *expirable.LRU[string, *repoTree]
	pending chan candidate
}

// candidate is a new issue of a project that assigns to suspect commit
// authors.
type candidate struct {
	orgID, issueID uuid.UUID
	notBefore      time.Time
}

// NewService creates the service; access may be nil without GitHub.
func NewService(database *db.DB, log *slog.Logger, access *github.Access) *Service {
	return &Service{
		db: database, log: log, github: access,
		rules:   expirable.NewLRU[string, *Ruleset](1000, nil, time.Hour),
		trees:   newTreeCache(),
		pending: make(chan candidate, 500),
	}
}

// Compile compiles a configuration; invalid lines are skipped (the API
// reports them on save). Compiled rulesets are cached by version.
func (s *Service) Compile(o *store.Ownership) *Ruleset {
	h := fnv.New64a()
	h.Write(o.PathMappings)
	key := fmt.Sprintf("%s/%d/%x", o.ProjectID, o.UpdatedAt.UnixNano(), h.Sum64())
	if rs, ok := s.rules.Get(key); ok {
		return rs
	}
	rs := Compile(o)
	s.rules.Add(key, rs)
	return rs
}

// Compile compiles a configuration without caching.
func Compile(o *store.Ownership) *Ruleset {
	co, _ := ParseCodeowners(o.Codeowners)
	rules, _ := ParseRules(o.Rules)
	rs := &Ruleset{Rules: append(co, rules...)}
	_ = json.Unmarshal(o.PathMappings, &rs.Rewrites)
	return rs
}

// firstMember picks who a rule's owners resolve to first: its owners in
// order, an alias's members in order.
func firstMember(owners []string, members map[string][]*store.User) *store.User {
	for _, o := range owners {
		if us := members[NormalizeOwner(o)]; len(us) > 0 {
			return us[0]
		}
	}
	return nil
}

func normalized(owners []string) []string {
	out := make([]string, 0, len(owners))
	for _, o := range owners {
		if n := NormalizeOwner(o); !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// HasNewIssues reports whether a batch created issues.
func HasNewIssues(b *ingest.Batch) bool {
	for _, o := range b.Issues {
		if o.Created {
			return true
		}
	}
	return false
}

// AssignNew assigns the new issues of a batch in projects that assign to
// issue owners, inside the batch transaction so that notifications already
// name the assignee. Projects assigning to suspect commit authors are
// queued for Run.
func (s *Service) AssignNew(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error {
	var created []*ingest.IssueOutcome
	var projects []uuid.UUID
	for _, o := range b.Issues {
		if o.Created {
			created = append(created, o)
			if !slices.Contains(projects, o.ProjectID) {
				projects = append(projects, o.ProjectID)
			}
		}
	}
	if len(created) == 0 {
		return nil
	}
	configs, err := store.OwnershipOf(ctx, tx, projects)
	if err != nil {
		return err
	}
	for _, o := range created {
		cfg := configs[o.ProjectID]
		switch {
		case cfg == nil:
			continue
		case cfg.AutoAssign == store.AutoAssignSuspectCommit:
			select {
			case s.pending <- candidate{orgID: o.OrgID, issueID: o.ID, notBefore: time.Now().Add(time.Second)}:
			default:
				s.log.Debug("ownership queue full; issue not assigned", slog.String("issue", o.ID.String()))
			}
			continue
		case cfg.AutoAssign != store.AutoAssignOwners:
			continue
		}
		winner := Winner(s.Compile(cfg).Match(SubjectOf(o.Events[0].Event)))
		if winner == nil || len(winner.Owners) == 0 {
			continue
		}
		members, err := store.OwnerMembers(ctx, tx, o.OrgID, normalized(winner.Owners))
		if err != nil {
			return err
		}
		if u := firstMember(winner.Owners, members); u != nil {
			if _, err := store.AssignIssue(ctx, tx, o.OrgID, o.ID, u, map[string]string{"auto": "rule", "rule": winner.Text()}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Run assigns queued issues to their suspect commit's author, or to the
// owner their rules name when the author is not a member, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-s.pending:
			if d := time.Until(c.notBefore); d > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			if err := s.assignBySuspect(ctx, c); err != nil && ctx.Err() == nil && !errors.Is(err, store.ErrNotFound) {
				s.log.Warn("suspect commit assignment", slog.String("issue", c.issueID.String()), slog.Any("err", err))
			}
		}
	}
}

func (s *Service) assignBySuspect(ctx context.Context, c candidate) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	sc, err := s.FindSuspectCommit(ctx, c.orgID, c.issueID)
	if err != nil && !errors.Is(err, ErrNotConfigured) {
		return err
	}
	owners, err := s.IssueOwners(ctx, c.orgID, c.issueID)
	if err != nil {
		return err
	}
	return s.db.InOrg(ctx, c.orgID, func(tx pgx.Tx) error {
		if sc != nil && sc.State == store.SuspectFound {
			if u, err := s.commitAuthor(ctx, tx, c.orgID, sc); err != nil {
				return err
			} else if u != nil {
				_, err := store.AssignIssue(ctx, tx, c.orgID, c.issueID, u, map[string]string{"auto": "suspect_commit", "commit": sc.SHA})
				return err
			}
		}
		if owners.Winner != nil && owners.Assignee != nil {
			_, err := store.AssignIssue(ctx, tx, c.orgID, c.issueID, owners.Assignee, map[string]string{"auto": "rule", "rule": owners.Winner.Text()})
			return err
		}
		return nil
	})
}

// commitAuthor is the member who wrote a commit: by e-mail address, then by
// their GitHub login mapped as "@login".
func (s *Service) commitAuthor(ctx context.Context, q store.Querier, orgID uuid.UUID, sc *store.SuspectCommit) (*store.User, error) {
	var keys []string
	if sc.AuthorEmail != "" {
		keys = append(keys, NormalizeOwner(sc.AuthorEmail))
	}
	if sc.AuthorLogin != "" {
		keys = append(keys, NormalizeOwner("@"+sc.AuthorLogin))
	}
	if len(keys) == 0 {
		return nil, nil
	}
	members, err := store.OwnerMembers(ctx, q, orgID, keys)
	if err != nil {
		return nil, err
	}
	return firstMember(keys, members), nil
}

// CommitAuthor is the member who wrote a suspect commit, if any.
func (s *Service) CommitAuthor(ctx context.Context, q store.Querier, orgID uuid.UUID, sc *store.SuspectCommit) (*store.User, error) {
	return s.commitAuthor(ctx, q, orgID, sc)
}

// Owner is a member who owns an issue and the rules that say so.
type Owner struct {
	User  *store.User
	Rules []*Rule
}

// Owners of an issue by its latest event and the current rules.
type Owners struct {
	// Owners, those of the deciding rule first.
	Owners []*Owner
	// Unmapped lists owners of matching rules that are not members:
	// unknown e-mail addresses and aliases without a mapping.
	Unmapped []string
	// Winner is the rule deciding automatic assignment, Assignee whom it
	// assigns to.
	Winner   *Rule
	Assignee *store.User
}

// IssueOwners evaluates the project's rules for an issue's latest event.
func (s *Service) IssueOwners(ctx context.Context, orgID, issueID uuid.UUID) (*Owners, error) {
	out := &Owners{}
	err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1`, issueID).Scan(&projectID); err != nil {
			return err
		}
		cfg, err := store.GetOwnership(ctx, tx, orgID, projectID)
		if err != nil {
			return err
		}
		rs := s.Compile(cfg)
		if len(rs.Rules) == 0 {
			return nil
		}
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT payload FROM events WHERE issue_id = $1 ORDER BY received_at DESC LIMIT 1`, issueID).Scan(&payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var ev ingestv1.Event
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, &ev); err != nil {
			return err
		}
		return s.owners(ctx, tx, orgID, rs.Match(SubjectOf(&ev)), out)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return out, err
}

func (s *Service) owners(ctx context.Context, q store.Querier, orgID uuid.UUID, matches []*Rule, out *Owners) error {
	var all []string
	for _, r := range matches {
		all = append(all, r.Owners...)
	}
	members, err := store.OwnerMembers(ctx, q, orgID, normalized(all))
	if err != nil {
		return err
	}
	out.Winner = Winner(matches)
	if out.Winner != nil {
		out.Assignee = firstMember(out.Winner.Owners, members)
	}
	byUser := map[uuid.UUID]*Owner{}
	// The deciding rule first, then the others from the last to the first.
	for i := len(matches) - 1; i >= 0; i-- {
		r := matches[i]
		for _, o := range r.Owners {
			us := members[NormalizeOwner(o)]
			if len(us) == 0 {
				if n := NormalizeOwner(o); !slices.Contains(out.Unmapped, n) {
					out.Unmapped = append(out.Unmapped, n)
				}
				continue
			}
			for _, u := range us {
				ow := byUser[u.ID]
				if ow == nil {
					ow = &Owner{User: u}
					byUser[u.ID] = ow
					out.Owners = append(out.Owners, ow)
				}
				if !slices.Contains(ow.Rules, r) {
					ow.Rules = append(ow.Rules, r)
				}
			}
		}
	}
	return nil
}
