package sentry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/metrics"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// minClockDrift is the smallest difference between the client's sent_at and
// the receive time that is treated as a wrong device clock, as in Sentry:
// smaller differences are ordinary network and queueing delays.
const minClockDrift = 55 * time.Minute

// clockDrift returns how much client timestamps must be shifted, from the
// envelope's sent_at.
func clockDrift(sentAt any, received time.Time) time.Duration {
	s, ok := sentAt.(string)
	if !ok || s == "" {
		return 0
	}
	t, ok := parseTime(json.RawMessage(`"` + s + `"`))
	if !ok {
		return 0
	}
	d := received.Sub(t)
	if d > -minClockDrift && d < minClockDrift {
		return 0
	}
	return d
}

// shiftEvent moves the client timestamps of an event by d.
func shiftEvent(ev *ingestv1.Event, d time.Duration) {
	if d == 0 {
		return
	}
	shift := func(t *timestamppb.Timestamp) *timestamppb.Timestamp {
		if t == nil {
			return nil
		}
		return timestamppb.New(t.AsTime().Add(d))
	}
	ev.Timestamp = shift(ev.Timestamp)
	for _, b := range ev.Breadcrumbs {
		b.Timestamp = shift(b.Timestamp)
	}
}

// feedbackContext is contexts.feedback of a Sentry feedback event.
type feedbackContext struct {
	Message           string `json:"message"`
	ContactEmail      string `json:"contact_email"`
	Name              string `json:"name"`
	URL               string `json:"url"`
	AssociatedEventID string `json:"associated_event_id"`
	Source            string `json:"source"`
}

// DecodeFeedback parses a "feedback" envelope item: an event whose
// contexts.feedback holds what the user wrote.
func DecodeFeedback(data []byte, drift time.Duration) (ingest.FeedbackInput, error) {
	var raw struct {
		EventID  string `json:"event_id"`
		Contexts struct {
			Feedback feedbackContext `json:"feedback"`
		} `json:"contexts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ingest.FeedbackInput{}, fmt.Errorf("invalid feedback JSON: %w", err)
	}
	ev, err := Decode(data)
	if err != nil {
		return ingest.FeedbackInput{}, err
	}
	shiftEvent(ev, drift)
	delete(ev.GetContexts().GetCustom(), "feedback")
	fb := raw.Contexts.Feedback
	in := ingest.FeedbackInput{
		FeedbackID: eventUUID(raw.EventID),
		Name:       fb.Name,
		Email:      fb.ContactEmail,
		Message:    fb.Message,
		URL:        fb.URL,
		Source:     fb.Source,
		Event:      ev,
	}
	if in.Source == "" {
		in.Source = "api"
	}
	if id, err := uuid.Parse(fb.AssociatedEventID); err == nil {
		in.EventID = &id
	}
	return in, nil
}

// DecodeUserReport parses the legacy "user_report" item
// ({"event_id", "name", "email", "comments"}).
func DecodeUserReport(data []byte) (ingest.FeedbackInput, error) {
	var r struct {
		EventID  string `json:"event_id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Comments string `json:"comments"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ingest.FeedbackInput{}, fmt.Errorf("invalid user report JSON: %w", err)
	}
	in := ingest.FeedbackInput{Name: r.Name, Email: r.Email, Message: r.Comments, Source: "user_report"}
	if id, err := uuid.Parse(r.EventID); err == nil {
		in.EventID = &id
	}
	// Reports carry no id of their own: derive one so that a retried
	// submission is stored once.
	in.FeedbackID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(r.EventID+"\x00"+r.Email+"\x00"+r.Comments))
	return in, nil
}

// eventUUID parses a Sentry event id (32 hex digits, with or without
// dashes), or makes a new one.
func eventUUID(s string) uuid.UUID {
	if id, err := uuid.Parse(s); err == nil {
		return id
	}
	return uuid.New()
}

// DecodeSession parses a "session" item.
func DecodeSession(data []byte, drift time.Duration) (ingest.SessionUpdate, error) {
	var s struct {
		SID       string          `json:"sid"`
		DID       any             `json:"did"`
		Init      bool            `json:"init"`
		Started   json.RawMessage `json:"started"`
		Timestamp json.RawMessage `json:"timestamp"`
		Status    string          `json:"status"`
		Errors    int             `json:"errors"`
		Attrs     sessionAttrs    `json:"attrs"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return ingest.SessionUpdate{}, fmt.Errorf("invalid session JSON: %w", err)
	}
	u := ingest.SessionUpdate{SID: s.SID, DistinctID: anyString(s.DID), Init: s.Init, Status: s.Status, Errors: s.Errors,
		Release: s.Attrs.Release, Environment: s.Attrs.Environment}
	if u.Status == "" {
		u.Status = ingest.SessionOK
	}
	if t, ok := parseTime(s.Started); ok {
		u.Started = t.Add(drift)
	} else if t, ok := parseTime(s.Timestamp); ok {
		u.Started = t.Add(drift)
	}
	return u, nil
}

type sessionAttrs struct {
	Release     string `json:"release"`
	Environment string `json:"environment"`
}

// DecodeSessionAggregates parses a "sessions" item.
func DecodeSessionAggregates(data []byte, drift time.Duration) ([]ingest.SessionAggregate, error) {
	var s struct {
		Aggregates []struct {
			Started  json.RawMessage `json:"started"`
			DID      any             `json:"did"`
			Exited   int             `json:"exited"`
			Errored  int             `json:"errored"`
			Abnormal int             `json:"abnormal"`
			Crashed  int             `json:"crashed"`
		} `json:"aggregates"`
		Attrs sessionAttrs `json:"attrs"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invalid sessions JSON: %w", err)
	}
	out := make([]ingest.SessionAggregate, 0, len(s.Aggregates))
	for _, a := range s.Aggregates {
		agg := ingest.SessionAggregate{DistinctID: anyString(a.DID), Exited: a.Exited, Errored: a.Errored,
			Abnormal: a.Abnormal, Crashed: a.Crashed, Release: s.Attrs.Release, Environment: s.Attrs.Environment}
		if t, ok := parseTime(a.Started); ok {
			agg.Started = t.Add(drift)
		}
		out = append(out, agg)
	}
	return out, nil
}

// recordClientReport counts what the SDK says it dropped before sending.
func recordClientReport(data []byte) {
	var r struct {
		DiscardedEvents []struct {
			Reason   string `json:"reason"`
			Category string `json:"category"`
			Quantity int    `json:"quantity"`
		} `json:"discarded_events"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if dec.Decode(&r) != nil {
		return
	}
	for _, d := range r.DiscardedEvents {
		metrics.ClientDiscarded(strings.ToLower(d.Reason), strings.ToLower(d.Category), d.Quantity)
	}
}
