package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// FeedbackServer implements izci.api.v1.FeedbackService.
type FeedbackServer struct {
	*base
	apiv1connect.UnimplementedFeedbackServiceHandler
}

var _ apiv1connect.FeedbackServiceHandler = (*FeedbackServer)(nil)

// ListFeedback implements FeedbackService.ListFeedback.
func (s *FeedbackServer) ListFeedback(ctx context.Context, req *connect.Request[apiv1.ListFeedbackRequest]) (*connect.Response[apiv1.ListFeedbackResponse], error) {
	limit := pageSize(req.Msg.GetPageSize())
	f := store.FeedbackFilter{Query: req.Msg.GetQuery(), Limit: limit + 1}
	if before, beforeID, ok := decodeCursor(req.Msg.GetPageToken()); ok {
		f.Before, f.BeforeID = &before, beforeID
	}
	if raw := req.Msg.GetIssueId(); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fieldError("issue_id", "must be a UUID")
		}
		f.IssueID = &id
	}
	var out apiv1.ListFeedbackResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if slug := req.Msg.GetProject(); slug != "" {
			p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), strings.ToLower(slug))
			if errors.Is(err, store.ErrNotFound) {
				return errNotFound("project")
			}
			if err != nil {
				return err
			}
			f.ProjectID = &p.ID
		}
		rows, err := store.ListFeedback(ctx, tx, f)
		if err != nil {
			return err
		}
		for i, r := range rows {
			if i == limit {
				last := rows[limit-1]
				out.NextPageToken = encodeCursor(last.CreatedAt, last.ID)
				break
			}
			out.Feedback = append(out.Feedback, toFeedback(r))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list feedback", err)
	}
	return connect.NewResponse(&out), nil
}

// toFeedback converts a stored feedback row.
func toFeedback(r store.FeedbackRow) *apiv1.UserFeedback {
	f := &apiv1.UserFeedback{
		Id:          r.ID.String(),
		Project:     &apiv1.ProjectRef{Id: r.ProjectID.String(), Slug: r.ProjectSlug, Name: r.ProjectName, Platform: r.ProjectPlatform},
		Name:        r.Name,
		Email:       r.Email,
		Message:     r.Message,
		Url:         r.URL,
		Environment: r.Environment,
		Release:     r.Release,
		Source:      r.Source,
		User:        r.UserLabel,
		CreatedAt:   ts(r.CreatedAt),
	}
	if r.EventID != nil {
		f.EventId = r.EventID.String()
	}
	if r.IssueID != nil && r.IssueShortSeq > 0 {
		f.Issue = &apiv1.IssueRef{Id: r.IssueID.String(), ShortId: ingest.ShortID(r.ProjectSlug, r.IssueShortSeq), Title: r.IssueTitle}
	}
	if r.CustomerKey != "" {
		f.Customer = &apiv1.CustomerRef{Key: r.CustomerKey, Name: r.CustomerName}
	}
	if len(r.Payload) > 0 {
		var ev ingestv1.Event
		if jsonIn(r.Payload, &ev) == nil {
			f.Browser, f.Os = browserAndOS(&ev)
		}
	}
	return f
}

// browserAndOS labels the browser and operating system of an event from
// its contexts, falling back to its User-Agent header.
func browserAndOS(ev *ingestv1.Event) (string, string) {
	c := ev.GetContexts()
	browser := strings.TrimSpace(c.GetBrowser().GetName() + " " + c.GetBrowser().GetVersion())
	os := strings.TrimSpace(c.GetOs().GetName() + " " + c.GetOs().GetVersion())
	if ua := ingest.UserAgentOf(ev); ua != "" && (browser == "" || os == "") {
		info := ingest.ParseUserAgent(ua)
		if browser == "" {
			browser = strings.TrimSpace(info.Browser + " " + majorVersion(info.BrowserVersion))
		}
		if os == "" {
			os = strings.TrimSpace(info.OS + " " + info.OSVersion)
		}
	}
	return browser, os
}

func majorVersion(v string) string {
	major, _, _ := strings.Cut(v, ".")
	return major
}

// eventExtras attaches the files and user feedback of an event.
func (s *EventServer) eventExtras(ctx context.Context, tx pgx.Tx, t *auth.Tenant, projectID, eventID uuid.UUID, d *apiv1.EventDetail) error {
	files, err := store.ListEventAttachments(ctx, tx, projectID, eventID)
	if err != nil {
		return err
	}
	for _, a := range files {
		d.Attachments = append(d.Attachments, toAttachment(a, t.Org.Slug))
	}
	rows, err := store.ListFeedback(ctx, tx, store.FeedbackFilter{ProjectID: &projectID, EventID: &eventID, Limit: 20})
	if err != nil {
		return err
	}
	for _, r := range rows {
		d.Feedback = append(d.Feedback, toFeedback(r))
	}
	return nil
}

func toAttachment(a store.Attachment, orgSlug string) *apiv1.EventAttachment {
	return &apiv1.EventAttachment{
		Id: a.ID.String(), Name: a.Name, ContentType: a.ContentType, AttachmentType: a.AttachmentType,
		SizeBytes: uint32(max(a.Size, 0)), CreatedAt: ts(a.CreatedAt),
		Url: "/attachments/" + url.PathEscape(orgSlug) + "/" + a.ID.String(),
	}
}

// inlineTypes are served for display in the browser; everything else is a
// download. HTML, SVG and scripts are never rendered from izci's origin.
var inlineTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "text/plain", "application/json"}

// ServeAttachment serves GET /attachments/{org}/{id} to members of the
// organization (session cookie or Bearer personal access token with the
// read scope).
func (s *EventServer) ServeAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.auth.Authenticate(ctx, r.Header)
	if err != nil {
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}
	if p == nil || !p.HasScope(apiv1.TokenScope_TOKEN_SCOPE_READ) {
		http.Error(w, "sign in to download attachments", http.StatusUnauthorized)
		return
	}
	t, err := s.auth.ResolveTenant(ctx, p, r.PathValue("org"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var a *store.Attachment
	err = s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		a, err = store.GetAttachment(ctx, tx, id)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "cannot read the attachment", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("Content-Length", strconv.Itoa(len(a.Data)))
	disposition := "attachment"
	switch {
	case r.URL.Query().Get("download") == "1":
	case slices.Contains(inlineTypes, a.ContentType):
		disposition = "inline"
	}
	ct := a.ContentType
	if !slices.Contains(inlineTypes, ct) {
		ct = "application/octet-stream"
	} else if strings.HasPrefix(ct, "text/") || ct == "application/json" {
		// JSON is shown as text, so the browser never interprets it.
		ct = "text/plain; charset=utf-8"
	}
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disposition, url.PathEscape(a.Name)))
	_, _ = w.Write(a.Data)
}
