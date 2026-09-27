package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/ingest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// EventServer implements izci.api.v1.EventService.
type EventServer struct {
	*base
	apiv1connect.UnimplementedEventServiceHandler
}

var _ apiv1connect.EventServiceHandler = (*EventServer)(nil)

// eventSummarySelect reads EventSummary columns of events AS e.
const eventSummarySelect = `
	SELECT e.id, e.event_id, e.issue_id, e.received_at, e.occurred_at, e.level, e.title, e.environment, e.release,
	       e.customer_key, coalesce(c.name, ''), e.tenant_key, e.user_label, e.browser, e.os, e.url,
	       coalesce(host(e.client_ip), ''), e.server_name
	FROM events e
	LEFT JOIN customers c ON e.customer_key <> '' AND c.org_id = e.org_id AND c.key = e.customer_key`

func scanEventSummary(row pgx.Row) (*apiv1.EventSummary, error) {
	var (
		id, eventID, issueID  uuid.UUID
		received, occurred    time.Time
		level                 int16
		customerKey, customer string
		s                     apiv1.EventSummary
	)
	if err := row.Scan(&id, &eventID, &issueID, &received, &occurred, &level, &s.Title, &s.Environment, &s.Release,
		&customerKey, &customer, &s.Tenant, &s.User, &s.Browser, &s.Os, &s.Url, &s.ClientIp, &s.ServerName); err != nil {
		return nil, err
	}
	s.Id, s.EventId, s.IssueId = id.String(), eventID.String(), issueID.String()
	s.ReceivedAt, s.OccurredAt, s.Level = ts(received), ts(occurred), ingestv1.Level(level)
	if customerKey != "" {
		s.Customer = &apiv1.CustomerRef{Key: customerKey, Name: customer}
	}
	return &s, nil
}

// eventByID is the partition-pruning lookup of an event by server id: the
// id embeds the receive time.
func eventByID(raw string) (string, []any, bool) {
	id, err := uuid.Parse(raw)
	if err != nil || id.Version() != 7 {
		return "", nil, false
	}
	at := ingest.ReceivedAt(id)
	return "e.id = $1 AND e.received_at BETWEEN $2 AND $3", []any{id, at.Add(-time.Second), at.Add(time.Second)}, true
}

// GetEvent implements EventService.GetEvent.
func (s *EventServer) GetEvent(ctx context.Context, req *connect.Request[apiv1.GetEventRequest]) (*connect.Response[apiv1.GetEventResponse], error) {
	m := req.Msg
	t := auth.TenantFrom(ctx)
	var out apiv1.GetEventResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var (
			where string
			args  []any
			order = ""
		)
		switch {
		case m.GetId() != "":
			var ok bool
			if where, args, ok = eventByID(m.GetId()); !ok {
				return errNotFound("event")
			}
		case m.GetIssueId() != "":
			issueID, err := uuid.Parse(m.GetIssueId())
			if err != nil {
				return errNotFound("issue")
			}
			where, args = "e.issue_id = $1", []any{issueID}
			switch m.GetPick() {
			case apiv1.EventPick_EVENT_PICK_OLDEST:
				order = " ORDER BY e.received_at, e.id"
			case apiv1.EventPick_EVENT_PICK_RECOMMENDED:
				// Best score among the most recent events: bounded work even
				// for issues with millions of events.
				where = `(e.id, e.received_at) = (
					SELECT id, received_at FROM (
					  SELECT id, received_at, score FROM events WHERE issue_id = $1 ORDER BY received_at DESC, id DESC LIMIT 200
					) r ORDER BY score DESC, received_at DESC LIMIT 1)`
			default:
				order = " ORDER BY e.received_at DESC, e.id DESC"
			}
		case m.GetClientEventId() != "":
			eventID, err := uuid.Parse(m.GetClientEventId())
			if err != nil {
				return errNotFound("event")
			}
			where, args, order = "e.event_id = $1", []any{eventID}, " ORDER BY e.received_at DESC"
		default:
			return fieldError("id", "one of id, issue_id or client_event_id is required")
		}
		var (
			id, eventID, issueID, projectID uuid.UUID
			received                        time.Time
			clientIP, release               string
			warnings, payload               []byte
			size                            int32
			d                               = &apiv1.EventDetail{Issue: &apiv1.IssueRef{}, Project: &apiv1.ProjectRef{}}
			seq                             int32
		)
		err := tx.QueryRow(ctx, `
			SELECT e.id, e.event_id, e.issue_id, e.project_id, e.received_at, coalesce(host(e.client_ip), ''), e.release,
			       e.warnings, e.size_bytes, e.payload, coalesce(i.short_seq, 0), i.title, p.slug, p.name, p.platform
			FROM events e
			JOIN issues i ON i.id = e.issue_id
			JOIN projects p ON p.id = e.project_id
			WHERE `+where+order+` LIMIT 1`, args...).Scan(
			&id, &eventID, &issueID, &projectID, &received, &clientIP, &release, &warnings, &size, &payload,
			&seq, &d.Issue.Title, &d.Project.Slug, &d.Project.Name, &d.Project.Platform)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound("event")
		}
		if err != nil {
			return err
		}
		d.Id, d.EventId, d.ReceivedAt, d.ClientIp, d.SizeBytes = id.String(), eventID.String(), ts(received), clientIP, uint32(size)
		d.Issue.Id, d.Issue.ShortId = issueID.String(), ingest.ShortID(d.Project.Slug, seq)
		d.Project.Id = projectID.String()
		_ = json.Unmarshal(warnings, &d.Warnings)
		d.Event = &ingestv1.Event{}
		if err := jsonIn(payload, d.Event); err != nil {
			return err
		}
		if ua := ingest.UserAgentOf(d.Event); ua != "" {
			info := ingest.ParseUserAgent(ua)
			d.UserAgent = &apiv1.UserAgentInfo{Browser: info.Browser, BrowserVersion: info.BrowserVersion, Os: info.OS,
				OsVersion: info.OSVersion, Device: info.Device, DeviceType: info.DeviceType, Bot: info.Bot, Raw: info.Raw}
		}
		if err := s.sourceLinks(ctx, tx, projectID, release, d); err != nil {
			return err
		}
		if err := s.eventExtras(ctx, tx, t, projectID, eventID, d); err != nil {
			return err
		}
		out.Event = d
		// Neighbours within the issue.
		var prev, next uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM events WHERE issue_id = $1 AND (received_at, id) < ($2, $3)
			ORDER BY received_at DESC, id DESC LIMIT 1`, issueID, received, id).Scan(&prev)
		if err == nil {
			out.PreviousId = prev.String()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT id FROM events WHERE issue_id = $1 AND (received_at, id) > ($2, $3)
			ORDER BY received_at, id LIMIT 1`, issueID, received, id).Scan(&next)
		if err == nil {
			out.NextId = next.String()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get event", err)
	}
	return connect.NewResponse(&out), nil
}

// sourceLinks fills the repository fields used to link frames to code.
func (s *EventServer) sourceLinks(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, release string, d *apiv1.EventDetail) error {
	var (
		url, branch, commit string
		mappings            []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT r.url, r.default_branch, r.path_mappings,
		       coalesce((SELECT commit_sha FROM releases WHERE project_id = r.project_id AND version = $2), '')
		FROM project_repositories r WHERE r.project_id = $1`, projectID, release).Scan(&url, &branch, &mappings, &commit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	d.RepositoryUrl, d.SourceRef = url, branch
	if commit != "" {
		d.SourceRef = commit
	}
	var pm []struct {
		From string `json:"from_prefix"`
		To   string `json:"to_prefix"`
	}
	_ = json.Unmarshal(mappings, &pm)
	for _, m := range pm {
		d.PathRewrites = append(d.PathRewrites, &apiv1.PathRewrite{FromPrefix: m.From, ToPrefix: m.To})
	}
	return nil
}

var rawEventJSON = protojson.MarshalOptions{Multiline: true, Indent: "  ", UseProtoNames: true}

// GetRawEvent implements EventService.GetRawEvent.
func (s *EventServer) GetRawEvent(ctx context.Context, req *connect.Request[apiv1.GetRawEventRequest]) (*connect.Response[apiv1.GetRawEventResponse], error) {
	t := auth.TenantFrom(ctx)
	where, args, ok := eventByID(req.Msg.GetId())
	if !ok {
		return nil, errNotFound("event")
	}
	var out apiv1.GetRawEventResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT e.payload FROM events e WHERE `+where, args...).Scan(&payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound("event")
		}
		if err != nil {
			return err
		}
		// Re-encode through the message: jsonb reorders keys, the proto
		// field order is easier to read.
		var ev ingestv1.Event
		if err := jsonIn(payload, &ev); err != nil {
			return err
		}
		raw, err := rawEventJSON.Marshal(&ev)
		out.Json = strings.TrimSpace(string(raw))
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get raw event", err)
	}
	return connect.NewResponse(&out), nil
}
