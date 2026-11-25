package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/clientip"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// PrivacyServer implements izci.api.v1.PrivacyService.
type PrivacyServer struct {
	*base
	apiv1connect.UnimplementedPrivacyServiceHandler
}

var _ apiv1connect.PrivacyServiceHandler = (*PrivacyServer)(nil)

const (
	// subjectDeleteBatch is how many events one transaction of a subject
	// deletion removes.
	subjectDeleteBatch = 1000
	// subjectDeleteBudget bounds one DeleteSubjectData call; the client
	// calls again while done is false.
	subjectDeleteBudget = 20 * time.Second
)

// subject validates a data subject and resolves its project.
func (s *PrivacyServer) subject(ctx context.Context, tx pgx.Tx, t *auth.Tenant, in *apiv1.DataSubject) (store.Subject, error) {
	var out store.Subject
	email := strings.ToLower(strings.TrimSpace(in.GetEmail()))
	id, username := strings.TrimSpace(in.GetUserId()), strings.TrimSpace(in.GetUsername())
	ip := strings.TrimSpace(in.GetIpAddress())
	customer := strings.ToLower(strings.TrimSpace(in.GetCustomer()))
	person := email != "" || id != "" || username != "" || ip != ""
	switch {
	case person && customer != "":
		return out, fieldError("subject.customer", "a request is about a person or a customer, not both")
	case !person && customer == "":
		return out, fieldError("subject", "give an identifier of the person (e-mail, user id, username, IP address) or a customer")
	}
	out.Customer = customer
	if id != "" {
		out.Keys = append(out.Keys, "id:"+id)
	}
	if email != "" {
		out.Keys, out.Emails = append(out.Keys, "email:"+email), []string{email}
	}
	if username != "" {
		out.Keys = append(out.Keys, "username:"+username)
	}
	if ip != "" {
		a, err := netip.ParseAddr(ip)
		if err != nil {
			return out, fieldError("subject.ip_address", "not an IP address")
		}
		out.Keys = append(out.Keys, "ip:"+a.Unmap().String())
	}
	if slug := strings.ToLower(strings.TrimSpace(in.GetProject())); slug != "" {
		p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), slug)
		if err != nil {
			return out, errNotFound("project")
		}
		out.ProjectID = &p.ID
	}
	if person {
		issues, err := store.SubjectIssues(ctx, tx, t.OrgID(), subjectHashes(out), out.ProjectID)
		if err != nil {
			return out, err
		}
		out.IssueIDs = issues
	}
	return out, nil
}

// subjectHashes are the issue_subjects hashes of a person's keys.
func subjectHashes(s store.Subject) [][]byte {
	out := make([][]byte, len(s.Keys))
	for i, k := range s.Keys {
		out[i] = ingest.SubjectKeyHash(k)
	}
	return out
}

// describeSubject is the audit metadata of a request: identifiers are
// masked, since the audit log outlives the deleted data.
func describeSubject(in *apiv1.DataSubject) map[string]string {
	meta := map[string]string{}
	mask := func(v string) string {
		r := []rune(v)
		if len(r) <= 2 {
			return "***"
		}
		return string(r[:2]) + "***"
	}
	if v := strings.TrimSpace(in.GetEmail()); v != "" {
		local, domain, _ := strings.Cut(v, "@")
		meta["email"] = mask(local) + "@" + domain
	}
	if v := strings.TrimSpace(in.GetUserId()); v != "" {
		meta["user_id"] = mask(v)
	}
	if v := strings.TrimSpace(in.GetUsername()); v != "" {
		meta["username"] = mask(v)
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(in.GetIpAddress())); err == nil {
		meta["ip_address"] = clientip.Anonymize(a.Unmap()).String()
	}
	if v := strings.TrimSpace(in.GetCustomer()); v != "" {
		meta["customer"] = strings.ToLower(v)
	}
	if v := strings.TrimSpace(in.GetProject()); v != "" {
		meta["project"] = strings.ToLower(v)
	}
	return meta
}

// subjectName is the audit target name of a request.
func subjectName(meta map[string]string) string {
	for _, k := range []string{"customer", "email", "user_id", "username", "ip_address"} {
		if v := meta[k]; v != "" {
			return v
		}
	}
	return ""
}

// FindSubjectData implements PrivacyService.FindSubjectData.
func (s *PrivacyServer) FindSubjectData(ctx context.Context, req *connect.Request[apiv1.FindSubjectDataRequest]) (*connect.Response[apiv1.FindSubjectDataResponse], error) {
	var out apiv1.FindSubjectDataResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		subj, err := s.subject(ctx, tx, t, req.Msg.GetSubject())
		if err != nil {
			return err
		}
		sum, err := store.FindSubject(ctx, tx, t.OrgID(), subj)
		if err != nil {
			return err
		}
		out.Events, out.Issues = uint64(sum.Events), uint64(sum.Issues)
		out.Attachments, out.Feedback = uint64(sum.Attachments), uint64(sum.Feedback)
		for _, e := range sum.Latest {
			out.Latest = append(out.Latest, &apiv1.SubjectEvent{Id: e.ID.String(), Project: e.ProjectSlug,
				IssueShortId: ingest.ShortID(e.ProjectSlug, e.IssueSeq), Title: e.Title, UserLabel: e.UserLabel, ReceivedAt: ts(e.ReceivedAt)})
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "find subject data", err)
	}
	return connect.NewResponse(&out), nil
}

// exportedEvent is one event of an export.
type exportedEvent struct {
	ID          uuid.UUID                  `json:"id"`
	EventID     uuid.UUID                  `json:"event_id"`
	Project     string                     `json:"project"`
	Issue       string                     `json:"issue,omitempty"`
	ReceivedAt  time.Time                  `json:"received_at"`
	OccurredAt  time.Time                  `json:"occurred_at"`
	Attachments []store.ExportedAttachment `json:"attachments,omitempty"`
	Event       json.RawMessage            `json:"event"`
}

func parseExportToken(tok string) (*store.ExportCursor, error) {
	if tok == "" {
		return nil, nil
	}
	at, id, ok := strings.Cut(tok, "_")
	nanos, err := strconv.ParseInt(at, 10, 64)
	uid, uerr := uuid.Parse(id)
	if !ok || err != nil || uerr != nil {
		return nil, fieldError("page_token", "invalid page token")
	}
	return &store.ExportCursor{ReceivedAt: time.Unix(0, nanos), ID: uid}, nil
}

// ExportSubjectData implements PrivacyService.ExportSubjectData.
func (s *PrivacyServer) ExportSubjectData(ctx context.Context, req *connect.Request[apiv1.ExportSubjectDataRequest]) (*connect.Response[apiv1.ExportSubjectDataResponse], error) {
	m := req.Msg
	after, err := parseExportToken(m.GetPageToken())
	if err != nil {
		return nil, err
	}
	size := int(m.GetPageSize())
	if size == 0 {
		size = 200
	}
	var out apiv1.ExportSubjectDataResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		subj, err := s.subject(ctx, tx, t, m.GetSubject())
		if err != nil {
			return err
		}
		events, err := store.ExportSubjectEvents(ctx, tx, t.OrgID(), subj, after, size)
		if err != nil {
			return err
		}
		for _, e := range events {
			js, err := json.Marshal(exportedEvent{ID: e.ID, EventID: e.EventID, Project: e.ProjectSlug, Issue: ingest.ShortID(e.ProjectSlug, e.IssueSeq),
				ReceivedAt: e.ReceivedAt, OccurredAt: e.OccurredAt, Attachments: e.Attachments, Event: e.Payload})
			if err != nil {
				return err
			}
			out.EventsJson = append(out.EventsJson, string(js))
		}
		if len(events) == size {
			last := events[len(events)-1]
			out.NextPageToken = strconv.FormatInt(last.ReceivedAt.UnixNano(), 10) + "_" + last.ID.String()
		}
		if after != nil {
			return nil
		}
		// The first page carries the feedback, and is the one recorded.
		feedback, err := store.ExportSubjectFeedback(ctx, tx, t.OrgID(), subj)
		if err != nil {
			return err
		}
		for _, f := range feedback {
			js, err := json.Marshal(f)
			if err != nil {
				return err
			}
			out.FeedbackJson = append(out.FeedbackJson, string(js))
		}
		meta := describeSubject(m.GetSubject())
		return s.audit(ctx, tx, "privacy.export", "subject", "", subjectName(meta), meta)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "export subject data", err)
	}
	return connect.NewResponse(&out), nil
}

// forgetUsers removes the facets and affected-user records that named the
// users of deleted events.
func forgetUsers(ctx context.Context, tx pgx.Tx, deleted []store.DeletedEvent) error {
	hashes := make([][]byte, len(deleted))
	for i, d := range deleted {
		if len(d.User) == 0 {
			continue
		}
		var u ingestv1.User
		if err := protojson.Unmarshal(d.User, &u); err == nil {
			hashes[i] = ingest.UserHash(&u)
		}
	}
	return store.ForgetEventUsers(ctx, tx, deleted, hashes)
}

// DeleteSubjectData implements PrivacyService.DeleteSubjectData.
func (s *PrivacyServer) DeleteSubjectData(ctx context.Context, req *connect.Request[apiv1.DeleteSubjectDataRequest]) (*connect.Response[apiv1.DeleteSubjectDataResponse], error) {
	t := auth.TenantFrom(ctx)
	var subj store.Subject
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		var err error
		subj, err = s.subject(ctx, tx, t, req.Msg.GetSubject())
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete subject data", err)
	}
	var out apiv1.DeleteSubjectDataResponse
	deadline := time.Now().Add(subjectDeleteBudget)
	for {
		var n int64
		err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
			d, err := store.DeleteSubjectEvents(ctx, tx, t.OrgID(), subj, subjectDeleteBatch)
			if err != nil {
				return err
			}
			n = d.Events
			out.Events += uint64(d.Events)
			out.Attachments += uint64(d.Attachments)
			out.Feedback += uint64(d.Feedback)
			return forgetUsers(ctx, tx, d.Deleted)
		})
		if err != nil {
			return nil, internal(ctx, s.log, "delete subject data", err)
		}
		if n < subjectDeleteBatch {
			out.Done = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	err = s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		if out.Done {
			feedback, err := store.DeleteSubjectRecords(ctx, tx, t.OrgID(), subj)
			if err != nil {
				return err
			}
			out.Feedback += uint64(feedback)
			if subj.Customer == "" {
				if err := store.ForgetSubjectIssues(ctx, tx, t.OrgID(), subjectHashes(subj), subj.IssueIDs); err != nil {
					return err
				}
			}
		}
		meta := describeSubject(req.Msg.GetSubject())
		meta["events"] = strconv.FormatUint(out.Events, 10)
		meta["attachments"] = strconv.FormatUint(out.Attachments, 10)
		meta["feedback"] = strconv.FormatUint(out.Feedback, 10)
		meta["done"] = strconv.FormatBool(out.Done)
		return s.audit(ctx, tx, "privacy.delete", "subject", "", subjectName(meta), meta)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete subject data", err)
	}
	return connect.NewResponse(&out), nil
}

// retention helpers ---------------------------------------------------------

// maxRetentionDays is the longest retention an organization can choose:
// the server's, or its plan's limit when shorter.
func (b *base) maxRetentionDays(o *store.Organization) uint32 {
	days := uint32(b.cfg.Retention.Days)
	if l := o.Limits.GetRetentionDays(); l > 0 && l < days {
		days = l
	}
	return days
}

// orgRetentionDays is how long the organization keeps its data.
func (b *base) orgRetentionDays(o *store.Organization) uint32 {
	days := b.maxRetentionDays(o)
	if d := o.Settings.GetRetentionDays(); d > 0 && d < days {
		days = d
	}
	return days
}

// organization converts an organization for the API, with the retention
// it can choose.
func (b *base) organization(o *store.Organization, projects, members uint32) *apiv1.Organization {
	out := toOrganization(o, projects, members)
	out.MaxRetentionDays = b.maxRetentionDays(o)
	return out
}

// checkRetention rejects a retention longer than allowed; shortening it
// deletes data at the next retention pass, so it needs a recently
// re-authenticated session.
func checkRetention(ctx context.Context, field string, days, before, limit uint32) error {
	if days > limit {
		return fieldError(field, fmt.Sprintf("data can be kept for at most %d days here", limit))
	}
	effective := func(d uint32) uint32 {
		if d == 0 || d > limit {
			return limit
		}
		return d
	}
	if effective(days) < effective(before) {
		return requireRecentAuth(ctx)
	}
	return nil
}
