package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/store"
)

func toTicket(t store.IssueTicket) *apiv1.TicketRef {
	return &apiv1.TicketRef{Key: t.Key, Url: t.URL, ChannelId: t.ChannelID.String(), ChannelName: t.ChannelName, CreatedAt: ts(t.CreatedAt)}
}

// CreateTicket implements IssueService.CreateTicket.
func (s *IssueServer) CreateTicket(ctx context.Context, req *connect.Request[apiv1.CreateTicketRequest]) (*connect.Response[apiv1.CreateTicketResponse], error) {
	t := auth.TenantFrom(ctx)
	channelID, _ := uuid.Parse(req.Msg.GetChannelId())
	var (
		issueID uuid.UUID
		ch      *store.Channel
		payload = map[string]any{}
		out     apiv1.CreateTicketResponse
	)
	// Look up everything first: the ticket is created outside any
	// transaction.
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var err error
		if issueID, err = s.resolveIssueID(ctx, tx, t, req.Msg.GetIssue()); err != nil {
			return err
		}
		ch, err = store.GetChannel(ctx, tx, channelID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && ch.Kind != store.ChannelJira) {
			return errNotFound("Jira channel")
		}
		if err != nil {
			return err
		}
		if existing, err := store.GetThread(ctx, tx, ch.ID, issueID); err == nil {
			if key, _ := existing.Payload["key"].(string); key != "" {
				url, _ := existing.Payload["url"].(string)
				out.Ticket = &apiv1.TicketRef{Key: key, Url: url, ChannelId: ch.ID.String(), ChannelName: ch.Name,
					CreatedAt: ts(existing.LastSentAt)}
				return nil
			}
		}
		var latest *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT last_event_id FROM issues WHERE id = $1`, issueID).Scan(&latest); err != nil {
			return err
		}
		if latest != nil {
			payload["event_id"] = latest.String()
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "create ticket", err)
	}
	if out.Ticket != nil {
		return connect.NewResponse(&out), nil
	}
	view, err := notify.LoadIssueView(ctx, s.db, issueID, payload)
	if err != nil {
		return nil, internal(ctx, s.log, "create ticket", err)
	}
	link := s.cfg.PublicURL + "/o/" + view.OrgSlug + "/issues/" + view.ShortID
	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticket, thread, err := s.jira.CreateTicket(createCtx, ch, view, link)
	if err != nil {
		return nil, jiraFailure("Jira ticket failed", err)
	}
	user := auth.PrincipalFrom(ctx).User.ID
	err = s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		if err := store.SaveThread(ctx, tx, t.OrgID(), *thread); err != nil {
			return err
		}
		if err := store.RecordDelivery(ctx, tx, ch.ID, ""); err != nil {
			return err
		}
		return store.AddIssueActivity(ctx, tx, t.OrgID(), []uuid.UUID{issueID}, store.ActivityTicketCreated, &user,
			map[string]string{"channel": ch.Name, "key": ticket.Key, "url": ticket.URL})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "record ticket", err)
	}
	out.Ticket = &apiv1.TicketRef{Key: ticket.Key, Url: ticket.URL, ChannelId: ch.ID.String(), ChannelName: ch.Name, CreatedAt: ts(time.Now())}
	out.Created = true
	return connect.NewResponse(&out), nil
}
