package realtime

import (
	"testing"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func TestHubDeliversPerOrganizationWithoutBlocking(t *testing.T) {
	h := NewHub()
	orgA, orgB := uuid.New(), uuid.New()
	a := h.Subscribe(orgA, 2)
	b := h.Subscribe(orgB, 2)
	if h.Subscribers() != 2 {
		t.Fatalf("subscribers = %d", h.Subscribers())
	}
	msg := func(org uuid.UUID) Message {
		return Message{Kind: apiv1.LiveEventKind_LIVE_EVENT_KIND_EVENT, OrgID: org, Level: ingestv1.Level_LEVEL_ERROR}
	}
	// Three messages into a buffer of two: the third is dropped, not blocked on.
	h.Publish(msg(orgA), msg(orgA), msg(orgA))
	if len(a.C) != 2 || a.Dropped.Load() != 1 {
		t.Fatalf("org A: %d queued, %d dropped", len(a.C), a.Dropped.Load())
	}
	if len(b.C) != 0 {
		t.Fatalf("org B received %d messages of org A", len(b.C))
	}
	h.Unsubscribe(a)
	h.Publish(msg(orgA))
	if h.Subscribers() != 1 || len(a.C) != 2 {
		t.Fatal("unsubscribed subscriber still receives messages")
	}
}

func TestMatches(t *testing.T) {
	issue := &apiv1.IssueSummary{Project: &apiv1.ProjectRef{Slug: "kalem"}}
	ev := Message{Kind: apiv1.LiveEventKind_LIVE_EVENT_KIND_EVENT, Level: ingestv1.Level_LEVEL_WARNING, Issue: issue}
	cases := []struct {
		name     string
		m        Message
		projects []string
		min      ingestv1.Level
		want     bool
	}{
		{"no filters", ev, nil, ingestv1.Level_LEVEL_UNSPECIFIED, true},
		{"project match is case-insensitive", ev, []string{"KALEM"}, 0, true},
		{"other project", ev, []string{"platform"}, 0, false},
		{"below minimum level", ev, nil, ingestv1.Level_LEVEL_ERROR, false},
		{"triage ignores the level filter", Message{Kind: apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_UPDATED,
			Level: ingestv1.Level_LEVEL_INFO, Issue: issue}, nil, ingestv1.Level_LEVEL_ERROR, true},
	}
	for _, c := range cases {
		if got := Matches(c.m, c.projects, c.min); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
