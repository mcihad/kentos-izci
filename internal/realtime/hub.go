// Package realtime fans live updates (stored events, new, regressed and
// triaged issues) out to streaming API subscribers of the same
// organization. Slow subscribers lose messages instead of slowing ingest.
package realtime

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Message is one live update.
type Message struct {
	Kind      apiv1.LiveEventKind
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Level     ingestv1.Level
	Time      time.Time
	Issue     *apiv1.IssueSummary
	Event     *apiv1.EventSummary
}

// Subscription receives messages of one organization.
type Subscription struct {
	org     uuid.UUID
	C       chan Message
	Dropped atomic.Int64
}

// Hub is an in-process publish/subscribe hub.
type Hub struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
}

// NewHub creates a hub.
func NewHub() *Hub { return &Hub{subs: map[*Subscription]struct{}{}} }

// Subscribe registers a subscriber for an organization.
func (h *Hub) Subscribe(org uuid.UUID, buffer int) *Subscription {
	s := &Subscription{org: org, C: make(chan Message, buffer)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// Subscribers returns the number of subscribers.
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Publish delivers messages without blocking.
func (h *Hub) Publish(msgs ...Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.subs) == 0 {
		return
	}
	for _, m := range msgs {
		for s := range h.subs {
			if s.org != m.OrgID {
				continue
			}
			select {
			case s.C <- m:
			default:
				s.Dropped.Add(1)
			}
		}
	}
}

// FromBatch converts a committed ingest batch into live messages.
func FromBatch(b *ingest.Batch, publicURL string) []Message {
	var out []Message
	for _, o := range b.Issues {
		latest := o.Latest()
		summary := &apiv1.IssueSummary{
			Id: o.ID.String(), ShortId: o.ShortID,
			Project: &apiv1.ProjectRef{Id: o.ProjectID.String(), Slug: o.ProjectSlug, Platform: latest.Platform},
			Title:   latest.Title, Culprit: latest.Culprit, Type: latest.Type, Value: latest.Value,
			Level: latest.Level, Platform: latest.Platform, Status: apiv1.IssueStatus(o.Status),
			LastSeen: timestamppb.New(latest.OccurredAt), TimesSeen: uint64(o.TimesSeen), Unhandled: latest.Unhandled,
			Permalink: publicURL + "/o/" + o.OrgSlug + "/issues/" + o.ShortID,
		}
		if o.Created {
			summary.FirstSeen = timestamppb.New(o.Events[0].OccurredAt)
			summary.Substatus = apiv1.IssueSubstatus_ISSUE_SUBSTATUS_NEW
		}
		if o.Regressed {
			summary.Substatus = apiv1.IssueSubstatus_ISSUE_SUBSTATUS_REGRESSED
		}
		kind := apiv1.LiveEventKind_LIVE_EVENT_KIND_EVENT
		switch {
		case o.Created:
			kind = apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_CREATED
		case o.Regressed:
			kind = apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_REGRESSED
		}
		for i, p := range o.Events {
			k := apiv1.LiveEventKind_LIVE_EVENT_KIND_EVENT
			if i == len(o.Events)-1 {
				k = kind
			}
			out = append(out, Message{
				Kind: k, OrgID: o.OrgID, ProjectID: o.ProjectID, Level: p.Level, Time: p.ReceivedAt,
				Issue: summary, Event: EventSummary(p, o.ID),
			})
		}
	}
	return out
}

// EventSummary converts a processed event.
func EventSummary(p *ingest.Processed, issueID uuid.UUID) *apiv1.EventSummary {
	s := &apiv1.EventSummary{
		Id: p.ID.String(), EventId: p.EventID.String(), IssueId: issueID.String(),
		ReceivedAt: timestamppb.New(p.ReceivedAt), OccurredAt: timestamppb.New(p.OccurredAt),
		Level: p.Level, Title: p.Title, Environment: p.Environment, Release: p.Release,
		Tenant: p.TenantKey, User: p.UserLabel, Browser: p.Browser, Os: p.OS, Url: p.URL, ServerName: p.ServerName,
	}
	if p.CustomerKey != "" {
		s.Customer = &apiv1.CustomerRef{Key: p.CustomerKey, Name: p.CustomerName}
	}
	if p.ClientIP.IsValid() {
		s.ClientIp = p.ClientIP.String()
	}
	return s
}

// Matches reports whether a message passes a subscriber's filters.
func Matches(m Message, projectSlugs []string, minLevel ingestv1.Level) bool {
	if m.Kind != apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_UPDATED && m.Level < minLevel {
		return false
	}
	if len(projectSlugs) == 0 || m.Issue == nil || m.Issue.GetProject() == nil {
		return true
	}
	for _, s := range projectSlugs {
		if strings.EqualFold(s, m.Issue.GetProject().GetSlug()) {
			return true
		}
	}
	return false
}
