package ingest

import (
	"context"
	"errors"
	"mime"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// DefaultMaxAttachmentBytes bounds a single stored attachment.
const DefaultMaxAttachmentBytes = 10 << 20

// AttachmentFile is a file received with an event.
type AttachmentFile struct {
	Name        string
	ContentType string
	// Type is the Sentry attachment type, e.g. "event.attachment".
	Type string
	Data []byte
}

// StoreAttachments stores files sent with an event. Files of projects that
// discard attachments and files over the size limit are dropped; the result
// is the number stored.
func (s *Service) StoreAttachments(ctx context.Context, c *Caller, eventID uuid.UUID, files []AttachmentFile) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	if c.Key.ProjectSettings.GetPrivacy().GetDiscardAttachments() {
		metrics.AttachmentsDiscarded.Add(len(files))
		return 0, nil
	}
	limit := s.cfg.MaxAttachmentBytes
	if limit <= 0 {
		limit = DefaultMaxAttachmentBytes
	}
	var rows []store.Attachment
	for _, f := range files {
		if len(f.Data) > limit {
			metrics.AttachmentsTooLarge.Inc()
			continue
		}
		rows = append(rows, store.Attachment{
			OrgID: c.Key.Key.OrgID, ProjectID: c.Key.Key.ProjectID, EventID: eventID,
			Name: attachmentName(f.Name), ContentType: mediaType(f.ContentType), AttachmentType: attachmentType(f.Type),
			Data: f.Data,
		})
	}
	if len(rows) == 0 {
		return 0, nil
	}
	err := s.db.InOrg(ctx, c.Key.Key.OrgID, func(tx pgx.Tx) error { return store.InsertAttachments(ctx, tx, rows) })
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		metrics.AttachmentsStored.Inc()
		metrics.AttachmentBytes.Add(len(r.Data))
	}
	return len(rows), nil
}

// attachmentName keeps the base name of a file, at most 200 characters.
func attachmentName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, `\`, "/"))
	name = path.Base(name)
	if name == "." || name == "/" || name == "" {
		name = "attachment"
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// mediaType returns a well-formed media type without parameters, or
// application/octet-stream.
func mediaType(ct string) string {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil || len(mt) > 100 || !strings.Contains(mt, "/") {
		return "application/octet-stream"
	}
	return mt
}

var attachmentTypes = []string{
	"event.attachment", "event.view_hierarchy", "event.minidump", "event.applecrashreport",
	"unreal.context", "unreal.logs",
}

func attachmentType(t string) string {
	if slices.Contains(attachmentTypes, t) {
		return t
	}
	return "event.attachment"
}

// FeedbackInput is user feedback as received from an SDK.
type FeedbackInput struct {
	// FeedbackID identifies the submission; retries carry the same id.
	FeedbackID uuid.UUID
	// EventID is the event the feedback is about, when any.
	EventID *uuid.UUID
	Name    string
	Email   string
	Message string
	URL     string
	Source  string
	// Event carries the context of the feedback (user, tags, device, …);
	// nil when the SDK sent none.
	Event *ingestv1.Event
}

// ErrEmptyFeedback rejects feedback without a message.
var ErrEmptyFeedback = errors.New("feedback message is empty")

// AcceptFeedback normalizes (scrubs) and stores user feedback. Feedback
// counts toward the project's rate limit.
func (s *Service) AcceptFeedback(ctx context.Context, c *Caller, in FeedbackInput) error {
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		metrics.FeedbackRejected.Inc()
		return ErrEmptyFeedback
	}
	if ok, _ := s.limits.Allow(c.Key, 1); !ok {
		metrics.FeedbackRejected.Inc()
		return nil
	}
	ev := in.Event
	if ev == nil {
		ev = &ingestv1.Event{}
	}
	ev.EventId = in.FeedbackID.String()
	ev.Exceptions, ev.Threads = nil, nil
	ev.Message = &ingestv1.Message{Formatted: msg}
	if ev.Level == ingestv1.Level_LEVEL_UNSPECIFIED {
		ev.Level = ingestv1.Level_LEVEL_INFO
	}
	rk := c.Key
	project := ProjectContext{OrgID: rk.Key.OrgID, OrgSlug: rk.OrgSlug, ProjectID: rk.Key.ProjectID, ProjectSlug: rk.ProjectSlug,
		Settings: rk.ProjectSettings}
	p, err := Normalize(ev, NormalizeOptions{Project: project, TransportIP: parseAddrLoose(c.TransportIP), Retention: s.cfg.Retention})
	if err != nil {
		metrics.FeedbackRejected.Inc()
		return err
	}
	if slices.Contains(rk.ProjectSettings.GetIgnoredEnvironments(), p.Environment) {
		return nil
	}
	url := strings.TrimSpace(in.URL)
	if url == "" {
		url = p.URL
	}
	f := &store.Feedback{
		OrgID: rk.Key.OrgID, ProjectID: rk.Key.ProjectID, FeedbackID: in.FeedbackID, EventID: in.EventID,
		Name: clip(in.Name, 256), Email: clip(in.Email, 256), Message: p.Event.GetMessage().GetFormatted(),
		URL: clip(url, 2048), Environment: p.Environment, Release: p.Release, Source: clip(in.Source, 64),
		CustomerKey: p.CustomerKey, UserLabel: p.UserLabel, Payload: p.Payload,
	}
	var stored bool
	err = s.db.InOrg(ctx, rk.Key.OrgID, func(tx pgx.Tx) error {
		var err error
		stored, err = store.InsertFeedback(ctx, tx, f)
		return err
	})
	if err != nil {
		return err
	}
	if stored {
		metrics.FeedbackStored.Inc()
	} else {
		metrics.FeedbackDuplicate.Inc()
	}
	return nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// SessionRecorder returns the recorder of release health, or nil.
func (s *Service) SessionRecorder() *SessionRecorder { return s.sessions }

// RecordSessions adds session updates and aggregates of a caller.
func (s *Service) RecordSessions(c *Caller, updates []SessionUpdate, aggregates []SessionAggregate) {
	if s.sessions == nil {
		return
	}
	now := time.Now()
	for _, u := range updates {
		s.sessions.Record(c, u, now)
	}
	for _, a := range aggregates {
		s.sessions.RecordAggregate(c, a, now)
	}
}
