package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

// AiServer implements izci.api.v1.AiService.
type AiServer struct {
	*base
	apiv1connect.UnimplementedAiServiceHandler
}

var _ apiv1connect.AiServiceHandler = (*AiServer)(nil)

// aiError maps errors of the AI runner to API errors.
func (s *AiServer) aiError(ctx context.Context, op string, err error) error {
	var publish *ai.PublishError
	switch {
	case errors.Is(err, ai.ErrDisabled):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_AI_DISABLED, "AI analysis is disabled on this server")
	case errors.Is(err, ai.ErrBudgetExceeded):
		return apiError(connect.CodeResourceExhausted, apiv1.ErrorReason_ERROR_REASON_AI_BUDGET_EXCEEDED, err.Error())
	case errors.Is(err, store.ErrRunActive):
		return apiError(connect.CodeAlreadyExists, apiv1.ErrorReason_ERROR_REASON_AI_RUN_ACTIVE, err.Error())
	case errors.Is(err, ai.ErrEventMismatch):
		return fieldError("event_id", err.Error())
	case errors.Is(err, ai.ErrNotReady):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_AI_RUN_NOT_READY, err.Error())
	case errors.Is(err, ai.ErrNoRepository):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_REPOSITORY_NOT_LINKED, err.Error())
	case errors.Is(err, ai.ErrInvalidBranch):
		return fieldError("branch", err.Error())
	case errors.As(err, &publish):
		return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_INTEGRATION_FAILED, publish.Error())
	}
	return githubError(ctx, s.log, op, err)
}

// runRefs holds the issues, projects and users referenced by runs.
type runRefs struct {
	issues   map[uuid.UUID]*apiv1.IssueRef
	projects map[uuid.UUID]*apiv1.ProjectRef
	users    map[uuid.UUID]*apiv1.UserRef
}

func loadRunRefs(ctx context.Context, tx pgx.Tx, runs []*store.AiRun) (*runRefs, error) {
	refs := &runRefs{issues: map[uuid.UUID]*apiv1.IssueRef{}, projects: map[uuid.UUID]*apiv1.ProjectRef{}}
	var issues, users []uuid.UUID
	for _, r := range runs {
		issues = append(issues, r.IssueID)
		if r.RequestedBy != nil {
			users = append(users, *r.RequestedBy)
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT i.id, coalesce(i.short_seq, 0), i.title, p.id, p.slug, p.name, p.platform
		FROM issues i JOIN projects p ON p.id = i.project_id WHERE i.id = ANY($1)`, issues)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, projectID uuid.UUID
		var seq int32
		var title string
		var p apiv1.ProjectRef
		if err := rows.Scan(&id, &seq, &title, &projectID, &p.Slug, &p.Name, &p.Platform); err != nil {
			return nil, err
		}
		p.Id = projectID.String()
		refs.projects[projectID] = &p
		refs.issues[id] = &apiv1.IssueRef{Id: id.String(), ShortId: ingest.ShortID(p.Slug, seq), Title: title}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	refs.users, err = userRefs(ctx, tx, users)
	return refs, err
}

var riskValues = map[string]apiv1.Risk{"low": apiv1.Risk_RISK_LOW, "medium": apiv1.Risk_RISK_MEDIUM, "high": apiv1.Risk_RISK_HIGH}

// toAiRun converts a run; full adds the analysis and the diff.
func toAiRun(r *store.AiRun, refs *runRefs, full bool) *apiv1.AiRun {
	out := &apiv1.AiRun{
		Id: r.ID.String(), Issue: refs.issues[r.IssueID], Project: refs.projects[r.ProjectID], Status: apiv1.AiRunStatus(r.Status),
		Trigger: apiv1.AiRunTrigger(r.Trigger), Mode: apiv1.AiRunMode(r.Mode), Model: r.Model, BaseCommit: r.BaseCommit,
		BaseBranch: r.BaseBranch, Error: r.Error, CreatedAt: ts(r.CreatedAt), StartedAt: tsp(r.StartedAt), FinishedAt: tsp(r.FinishedAt),
		Result: &apiv1.AiResult{Summary: r.Result.Summary, RootCause: r.Result.RootCause, FixSummary: r.Result.FixSummary,
			Confidence: r.Result.Confidence, Risk: riskValues[r.Result.Risk], TestSuggestions: r.Result.TestSuggestions},
		Usage: &apiv1.AiUsage{InputTokens: uint64(max(r.Usage.InputTokens, 0)), OutputTokens: uint64(max(r.Usage.OutputTokens, 0)),
			CostUsd: r.Usage.CostUSD},
	}
	if r.EventID != nil {
		out.EventId = r.EventID.String()
	}
	if r.RequestedBy != nil {
		out.RequestedBy = refs.users[*r.RequestedBy]
	}
	for _, f := range r.Files {
		out.Files = append(out.Files, &apiv1.ChangedFile{Path: f.Path, Additions: uint32(max(f.Additions, 0)),
			Deletions: uint32(max(f.Deletions, 0)), Status: f.Status})
	}
	if pr := r.PullRequest; pr != nil {
		out.PullRequest = &apiv1.PullRequestRef{Number: uint32(max(pr.Number, 0)), Url: pr.URL, Title: pr.Title, Branch: pr.Branch,
			Draft: pr.Draft, State: pr.State, AiRunId: r.ID.String()}
	}
	if full {
		out.AnalysisMarkdown, out.Diff = r.Analysis, r.Diff
	}
	return out
}

func toLogEntry(l store.AiLog) *apiv1.AiLogEntry {
	return &apiv1.AiLogEntry{Time: ts(l.Time), Kind: apiv1.AiLogKind(l.Kind), Message: l.Message, Tool: l.Tool}
}

// loadRun returns a converted run of the request's organization.
func (s *AiServer) loadRun(ctx context.Context, id uuid.UUID, full bool) (*store.AiRun, *apiv1.AiRun, error) {
	var run *store.AiRun
	var out *apiv1.AiRun
	err := s.inOrg(ctx, func(tx pgx.Tx, _ *auth.Tenant) error {
		var err error
		if run, err = store.GetAiRun(ctx, tx, id); err != nil {
			return err
		}
		refs, err := loadRunRefs(ctx, tx, []*store.AiRun{run})
		out = toAiRun(run, refs, full)
		return err
	})
	return run, out, err
}

func parseRunID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fieldError(field, "invalid run id")
	}
	return id, nil
}

// StartRun implements AiService.StartRun.
func (s *AiServer) StartRun(ctx context.Context, req *connect.Request[apiv1.StartRunRequest]) (*connect.Response[apiv1.StartRunResponse], error) {
	if s.ai == nil {
		return nil, s.aiError(ctx, "StartRun", ai.ErrDisabled)
	}
	m := req.Msg
	issueID, err := uuid.Parse(m.GetIssueId())
	if err != nil {
		return nil, fieldError("issue_id", "invalid issue id")
	}
	var eventID *uuid.UUID
	if m.GetEventId() != "" {
		id, err := uuid.Parse(m.GetEventId())
		if err != nil {
			return nil, fieldError("event_id", "invalid event id")
		}
		eventID = &id
	}
	user := auth.PrincipalFrom(ctx).User.ID
	run, err := s.ai.Start(ctx, auth.TenantFrom(ctx).OrgID(), ai.StartRequest{IssueID: issueID, EventID: eventID, Mode: int16(m.GetMode()),
		Model: m.GetModel(), Instructions: m.GetInstructions(), Trigger: store.AiTriggerManual, RequestedBy: &user})
	if err != nil {
		return nil, s.aiError(ctx, "StartRun", err)
	}
	_, out, err := s.loadRun(ctx, run.ID, true)
	if err != nil {
		return nil, internal(ctx, s.log, "StartRun", err)
	}
	return connect.NewResponse(&apiv1.StartRunResponse{Run: out}), nil
}

// GetRun implements AiService.GetRun.
func (s *AiServer) GetRun(ctx context.Context, req *connect.Request[apiv1.GetRunRequest]) (*connect.Response[apiv1.GetRunResponse], error) {
	id, err := parseRunID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	_, run, err := s.loadRun(ctx, id, true)
	if err != nil {
		return nil, internal(ctx, s.log, "GetRun", err)
	}
	out := &apiv1.GetRunResponse{Run: run}
	if req.Msg.GetIncludeLog() {
		err := s.inOrg(ctx, func(tx pgx.Tx, _ *auth.Tenant) error {
			logs, err := store.ListAiLogs(ctx, tx, id, 0)
			for _, l := range logs {
				out.Log = append(out.Log, toLogEntry(l))
			}
			return err
		})
		if err != nil {
			return nil, internal(ctx, s.log, "GetRun", err)
		}
	}
	return connect.NewResponse(out), nil
}

// ListRuns implements AiService.ListRuns.
func (s *AiServer) ListRuns(ctx context.Context, req *connect.Request[apiv1.ListRunsRequest]) (*connect.Response[apiv1.ListRunsResponse], error) {
	m := req.Msg
	limit := pageSize(m.GetPageSize())
	f := store.AiRunFilter{Status: int16(m.GetStatus())}
	if before, id, ok := decodeCursor(m.GetPageToken()); ok {
		f.Before, f.BeforeID = &before, id
	} else if m.GetPageToken() != "" {
		return nil, fieldError("page_token", "invalid page token")
	}
	var out apiv1.ListRunsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if ref := strings.TrimSpace(m.GetIssueId()); ref != "" {
			id, err := (&IssueServer{base: s.base}).resolveIssueID(ctx, tx, t, ref)
			if err != nil {
				return err
			}
			f.IssueID = &id
		}
		if slug := strings.TrimSpace(m.GetProject()); slug != "" {
			p, err := (&ProjectServer{base: s.base}).project(ctx, tx, t, slug)
			if err != nil {
				return err
			}
			f.ProjectID = &p.ID
		}
		runs, err := store.ListAiRuns(ctx, tx, t.OrgID(), f, limit+1)
		if err != nil {
			return err
		}
		if len(runs) > limit {
			last := runs[limit-1]
			out.NextPageToken = encodeCursor(last.CreatedAt, last.ID)
			runs = runs[:limit]
		}
		refs, err := loadRunRefs(ctx, tx, runs)
		if err != nil {
			return err
		}
		for _, r := range runs {
			out.Runs = append(out.Runs, toAiRun(r, refs, false))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "ListRuns", err)
	}
	return connect.NewResponse(&out), nil
}

// WatchRun implements AiService.WatchRun: the run, the recorded log, then
// new log entries and status changes until the run finishes.
func (s *AiServer) WatchRun(ctx context.Context, req *connect.Request[apiv1.WatchRunRequest], stream *connect.ServerStream[apiv1.WatchRunResponse]) error {
	id, err := parseRunID("id", req.Msg.GetId())
	if err != nil {
		return err
	}
	var ping <-chan struct{}
	if s.ai != nil {
		ch, unsubscribe := s.ai.Subscribe(id)
		defer unsubscribe()
		ping = ch
	}
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	lastSeq, lastStatus := 0, int16(-1)
	for {
		var logs []store.AiLog
		run, out, err := s.loadRun(ctx, id, true)
		if err == nil {
			err = s.inOrg(ctx, func(tx pgx.Tx, _ *auth.Tenant) error {
				logs, err = store.ListAiLogs(ctx, tx, id, lastSeq)
				return err
			})
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return internal(ctx, s.log, "WatchRun", err)
		}
		first := lastStatus < 0
		if first {
			if err := stream.Send(&apiv1.WatchRunResponse{Update: &apiv1.WatchRunResponse_Run{Run: out}}); err != nil {
				return nil
			}
		}
		for _, l := range logs {
			if err := stream.Send(&apiv1.WatchRunResponse{Update: &apiv1.WatchRunResponse_Log{Log: toLogEntry(l)}}); err != nil {
				return nil
			}
			lastSeq = l.Seq
		}
		// Status changes are sent after the log entries that led to them, so
		// the last message of a finished run is the run itself.
		if !first && run.Status != lastStatus || !run.Active() && len(logs) > 0 {
			if err := stream.Send(&apiv1.WatchRunResponse{Update: &apiv1.WatchRunResponse_Run{Run: out}}); err != nil {
				return nil
			}
		}
		lastStatus = run.Status
		if !run.Active() {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		case <-ping:
		case <-poll.C:
		}
	}
}

// CancelRun implements AiService.CancelRun.
func (s *AiServer) CancelRun(ctx context.Context, req *connect.Request[apiv1.CancelRunRequest]) (*connect.Response[apiv1.CancelRunResponse], error) {
	id, err := parseRunID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if s.ai == nil {
		return nil, s.aiError(ctx, "CancelRun", ai.ErrDisabled)
	}
	u := auth.PrincipalFrom(ctx).User
	who := u.Name
	if who == "" {
		who = u.Email
	}
	if _, err := s.ai.Cancel(ctx, auth.TenantFrom(ctx).OrgID(), id, "cancelled by "+who); err != nil {
		return nil, s.aiError(ctx, "CancelRun", err)
	}
	_, out, err := s.loadRun(ctx, id, true)
	if err != nil {
		return nil, internal(ctx, s.log, "CancelRun", err)
	}
	return connect.NewResponse(&apiv1.CancelRunResponse{Run: out}), nil
}

// CreateBranch implements AiService.CreateBranch.
func (s *AiServer) CreateBranch(ctx context.Context, req *connect.Request[apiv1.CreateBranchRequest]) (*connect.Response[apiv1.CreateBranchResponse], error) {
	id, err := parseRunID("run_id", req.Msg.GetRunId())
	if err != nil {
		return nil, err
	}
	if s.ai == nil {
		return nil, s.aiError(ctx, "CreateBranch", ai.ErrDisabled)
	}
	user := auth.PrincipalFrom(ctx).User.ID
	if _, err := s.ai.CreateBranch(ctx, auth.TenantFrom(ctx).OrgID(), id, req.Msg.GetBranch(), &user); err != nil {
		return nil, s.aiError(ctx, "CreateBranch", err)
	}
	_, out, err := s.loadRun(ctx, id, false)
	if err != nil {
		return nil, internal(ctx, s.log, "CreateBranch", err)
	}
	return connect.NewResponse(&apiv1.CreateBranchResponse{PullRequest: out.PullRequest}), nil
}

// CreatePullRequest implements AiService.CreatePullRequest.
func (s *AiServer) CreatePullRequest(ctx context.Context, req *connect.Request[apiv1.CreatePullRequestRequest]) (*connect.Response[apiv1.CreatePullRequestResponse], error) {
	m := req.Msg
	id, err := parseRunID("run_id", m.GetRunId())
	if err != nil {
		return nil, err
	}
	if s.ai == nil {
		return nil, s.aiError(ctx, "CreatePullRequest", ai.ErrDisabled)
	}
	user := auth.PrincipalFrom(ctx).User.ID
	if _, err := s.ai.CreatePullRequest(ctx, auth.TenantFrom(ctx).OrgID(), id,
		ai.PullRequestOptions{Draft: m.GetDraft(), Title: m.GetTitle(), Base: m.GetBaseBranch()}, &user); err != nil {
		return nil, s.aiError(ctx, "CreatePullRequest", err)
	}
	_, out, err := s.loadRun(ctx, id, false)
	if err != nil {
		return nil, internal(ctx, s.log, "CreatePullRequest", err)
	}
	return connect.NewResponse(&apiv1.CreatePullRequestResponse{PullRequest: out.PullRequest}), nil
}

// GetAiStatus implements AiService.GetAiStatus.
func (s *AiServer) GetAiStatus(ctx context.Context, _ *connect.Request[apiv1.GetAiStatusRequest]) (*connect.Response[apiv1.GetAiStatusResponse], error) {
	out := &apiv1.GetAiStatusResponse{}
	if s.ai != nil && s.ai.Enabled() {
		cfg := s.ai.Config()
		out.Enabled, out.DefaultModel, out.MaxConcurrent = true, cfg.Model, uint32(max(cfg.MaxConcurrent, 0))
		out.OpencodeVersion = s.ai.Version(ctx)
	}
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		queued, running, err := store.CountActiveAiRuns(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		monthly, err := store.CountAiRuns(ctx, tx, t.OrgID(), nil, false, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
		out.Queued, out.Running, out.MonthlyRuns = uint32(queued), uint32(running), uint32(max(monthly, 0))
		out.MonthlyLimit = t.Org.Limits.GetMonthlyAiRuns()
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "GetAiStatus", err)
	}
	return connect.NewResponse(out), nil
}
