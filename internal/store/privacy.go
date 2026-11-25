package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Subject is whom a data subject request (KVKK, GDPR) is about: a person,
// by any of their identifiers, or a customer.
type Subject struct {
	// Keys are the identifiers of a person as izci_subject_keys writes
	// them: "id:…", "email:…" (lower case), "username:…", "ip:…".
	Keys []string
	// IssueIDs are the issues with events about the person
	// (SubjectIssues); only their events are searched.
	IssueIDs []uuid.UUID
	// Emails are the person's e-mail addresses in lower case, matched
	// against user feedback.
	Emails []string
	// Customer is a customer key (lower case); Keys and Emails are then
	// empty.
	Customer string
	// ProjectID limits the request to one project.
	ProjectID *uuid.UUID
}

// argList numbers query arguments as they are added.
type argList []any

func (a *argList) add(v any) string {
	*a = append(*a, v)
	return "$" + strconv.Itoa(len(*a))
}

// events is the condition on events (alias e) that matches the subject.
func (s Subject) events(orgID uuid.UUID, args *argList) string {
	cond := "e.org_id = " + args.add(orgID)
	if s.Customer != "" {
		// events_customer_idx
		cond += " AND e.customer_key = " + args.add(s.Customer)
	} else {
		// events_issue_idx, then the payload of those issues' events.
		cond += " AND e.issue_id = ANY(" + args.add(s.IssueIDs) + "::uuid[])" +
			" AND izci_subject_keys(e.payload -> 'user', e.client_ip) && " + args.add(s.Keys) + "::text[]"
	}
	if s.ProjectID != nil {
		cond += " AND e.project_id = " + args.add(*s.ProjectID)
	}
	return cond
}

// feedback is the condition on user feedback (alias f) that names the
// subject itself, besides feedback tied to the subject's events.
func (s Subject) feedback(orgID uuid.UUID, args *argList) string {
	cond := "f.org_id = " + args.add(orgID)
	if s.ProjectID != nil {
		cond += " AND f.project_id = " + args.add(*s.ProjectID)
	}
	if s.Customer != "" {
		return cond + " AND f.customer_key = " + args.add(s.Customer)
	}
	return cond + " AND (lower(f.email) = ANY(" + args.add(s.Emails) + "::text[]) OR izci_subject_keys(f.payload -> 'user', NULL) && " +
		args.add(s.Keys) + "::text[])"
}

// SubjectIssues returns the issues of the organization (of one project,
// when projectID is set) that have events naming any of the key hashes
// (ingest.SubjectKeyHash).
func SubjectIssues(ctx context.Context, q Querier, orgID uuid.UUID, hashes [][]byte, projectID *uuid.UUID) ([]uuid.UUID, error) {
	args := argList{orgID, hashes}
	project := ""
	if projectID != nil {
		project = " AND i.project_id = " + args.add(*projectID)
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT s.issue_id FROM issue_subjects s JOIN issues i ON i.id = s.issue_id
		WHERE s.org_id = $1 AND s.key_hash = ANY($2::bytea[])`+project, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ForgetSubjectIssues removes the subject's keys from issue_subjects once
// its events are deleted.
func ForgetSubjectIssues(ctx context.Context, q Querier, orgID uuid.UUID, hashes [][]byte, issues []uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM issue_subjects WHERE org_id = $1 AND key_hash = ANY($2::bytea[]) AND issue_id = ANY($3::uuid[])`,
		orgID, hashes, issues)
	return err
}

// SubjectEvent is an event about a subject.
type SubjectEvent struct {
	ID          uuid.UUID
	ProjectSlug string
	IssueSeq    int32
	Title       string
	UserLabel   string
	ReceivedAt  time.Time
}

// SubjectSummary counts what izci keeps about a subject.
type SubjectSummary struct {
	Events, Issues, Attachments, Feedback int64
	Latest                                []SubjectEvent
}

// FindSubject counts the events, issues, attachments and user feedback
// about a subject and returns the latest events.
func FindSubject(ctx context.Context, q Querier, orgID uuid.UUID, s Subject) (*SubjectSummary, error) {
	var out SubjectSummary
	var args argList
	match := s.events(orgID, &args)
	if err := q.QueryRow(ctx, `SELECT count(*), count(DISTINCT e.issue_id) FROM events e WHERE `+match, args...).
		Scan(&out.Events, &out.Issues); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM event_attachments a
		JOIN (SELECT DISTINCT e.project_id, e.event_id FROM events e WHERE `+match+`) v
		  ON a.project_id = v.project_id AND a.event_id = v.event_id`, args...).Scan(&out.Attachments); err != nil {
		return nil, err
	}
	fargs := append(argList{}, args...)
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM user_feedback f
		WHERE (`+s.feedback(orgID, &fargs)+`)
		   OR (f.event_id IS NOT NULL AND EXISTS (
		     SELECT 1 FROM events e WHERE e.project_id = f.project_id AND e.event_id = f.event_id AND `+match+`))`, fargs...).
		Scan(&out.Feedback); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT e.id, p.slug, coalesce(i.short_seq, 0), e.title, e.user_label, e.received_at
		FROM events e JOIN projects p ON p.id = e.project_id LEFT JOIN issues i ON i.id = e.issue_id
		WHERE `+match+` ORDER BY e.received_at DESC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ev SubjectEvent
		if err := rows.Scan(&ev.ID, &ev.ProjectSlug, &ev.IssueSeq, &ev.Title, &ev.UserLabel, &ev.ReceivedAt); err != nil {
			return nil, err
		}
		out.Latest = append(out.Latest, ev)
	}
	return &out, rows.Err()
}

// ExportCursor is the position after the last exported event.
type ExportCursor struct {
	ReceivedAt time.Time
	ID         uuid.UUID
}

// ExportedEvent is an event about a subject, as exported.
type ExportedEvent struct {
	ID, EventID            uuid.UUID
	ProjectSlug            string
	IssueSeq               int32
	ReceivedAt, OccurredAt time.Time
	Attachments            []ExportedAttachment
	// Payload is the stored event (protojson).
	Payload   json.RawMessage
	projectID uuid.UUID
}

// ExportedAttachment describes an attachment of an exported event.
type ExportedAttachment struct {
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Type        string    `json:"attachment_type"`
	SizeBytes   int       `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

// ExportSubjectEvents returns up to limit events about a subject after the
// cursor, oldest first, with their attachments.
func ExportSubjectEvents(ctx context.Context, q Querier, orgID uuid.UUID, s Subject, after *ExportCursor, limit int) ([]*ExportedEvent, error) {
	var args argList
	match := s.events(orgID, &args)
	if after != nil {
		match += " AND (e.received_at, e.id) > (" + args.add(after.ReceivedAt) + ", " + args.add(after.ID) + ")"
	}
	rows, err := q.Query(ctx, `
		SELECT e.id, e.event_id, e.project_id, p.slug, coalesce(i.short_seq, 0), e.received_at, e.occurred_at, e.payload
		FROM events e JOIN projects p ON p.id = e.project_id LEFT JOIN issues i ON i.id = e.issue_id
		WHERE `+match+` ORDER BY e.received_at, e.id LIMIT `+args.add(limit), args...)
	if err != nil {
		return nil, err
	}
	var out []*ExportedEvent
	byEvent := map[[2]uuid.UUID]*ExportedEvent{}
	var projects, events []uuid.UUID
	for rows.Next() {
		var ev ExportedEvent
		var payload []byte
		if err := rows.Scan(&ev.ID, &ev.EventID, &ev.projectID, &ev.ProjectSlug, &ev.IssueSeq, &ev.ReceivedAt, &ev.OccurredAt, &payload); err != nil {
			rows.Close()
			return nil, err
		}
		ev.Payload = payload
		out = append(out, &ev)
		byEvent[[2]uuid.UUID{ev.projectID, ev.EventID}] = &ev
		projects, events = append(projects, ev.projectID), append(events, ev.EventID)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	rows, err = q.Query(ctx, `
		SELECT a.project_id, a.event_id, a.name, a.content_type, a.attachment_type, a.size_bytes, a.created_at
		FROM event_attachments a JOIN unnest($1::uuid[], $2::uuid[]) AS v(project_id, event_id)
		  ON a.project_id = v.project_id AND a.event_id = v.event_id
		ORDER BY a.created_at`, projects, events)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key [2]uuid.UUID
		var a ExportedAttachment
		if err := rows.Scan(&key[0], &key[1], &a.Name, &a.ContentType, &a.Type, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		if ev := byEvent[key]; ev != nil {
			ev.Attachments = append(ev.Attachments, a)
		}
	}
	return out, rows.Err()
}

// ExportedFeedback is user feedback about a subject, as exported.
type ExportedFeedback struct {
	ID          uuid.UUID       `json:"id"`
	Project     string          `json:"project"`
	EventID     *uuid.UUID      `json:"event_id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Email       string          `json:"email,omitempty"`
	Message     string          `json:"message"`
	URL         string          `json:"url,omitempty"`
	Environment string          `json:"environment,omitempty"`
	Release     string          `json:"release,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Feedback    json.RawMessage `json:"feedback,omitempty"`
}

// ExportSubjectFeedback returns the user feedback about a subject: sent
// with the subject's events or naming the subject.
func ExportSubjectFeedback(ctx context.Context, q Querier, orgID uuid.UUID, s Subject) ([]*ExportedFeedback, error) {
	var args argList
	match := s.events(orgID, &args)
	rows, err := q.Query(ctx, `
		SELECT f.id, p.slug, f.event_id, f.name, f.email, f.message, f.url, f.environment, f.release, f.created_at, f.payload
		FROM user_feedback f JOIN projects p ON p.id = f.project_id
		WHERE (`+s.feedback(orgID, &args)+`)
		   OR (f.event_id IS NOT NULL AND EXISTS (
		     SELECT 1 FROM events e WHERE e.project_id = f.project_id AND e.event_id = f.event_id AND `+match+`))
		ORDER BY f.created_at LIMIT 10000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ExportedFeedback
	for rows.Next() {
		var f ExportedFeedback
		var payload []byte
		if err := rows.Scan(&f.ID, &f.Project, &f.EventID, &f.Name, &f.Email, &f.Message, &f.URL, &f.Environment, &f.Release,
			&f.CreatedAt, &payload); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			f.Feedback = payload
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// DeletedEvent describes a deleted event: its issue, how it named its user
// and the user object (protojson), for forgetting derived records.
type DeletedEvent struct {
	IssueID   uuid.UUID
	UserLabel string
	User      []byte
}

// EventDeletion counts what a deletion removed.
type EventDeletion struct {
	Events, Attachments, Feedback int64
	Deleted                       []DeletedEvent
}

// deleteEvents deletes the events (alias e) that match cond, at most
// limit, with their attachments and the user feedback sent with them.
func deleteEvents(ctx context.Context, q Querier, cond string, args argList, limit int) (*EventDeletion, error) {
	rows, err := q.Query(ctx, `
		WITH v AS (
		  SELECT e.id, e.received_at, e.project_id, e.event_id FROM events e WHERE `+cond+` LIMIT `+args.add(limit)+`
		), a AS (
		  DELETE FROM event_attachments x USING v WHERE x.project_id = v.project_id AND x.event_id = v.event_id RETURNING 1
		), f AS (
		  DELETE FROM user_feedback x USING v WHERE x.project_id = v.project_id AND x.event_id = v.event_id RETURNING 1
		), d AS (
		  DELETE FROM events x USING v WHERE x.id = v.id AND x.received_at = v.received_at
		  RETURNING x.issue_id, x.user_label, x.payload -> 'user' AS u
		)
		SELECT d.issue_id, d.user_label, d.u, (SELECT count(*) FROM a), (SELECT count(*) FROM f) FROM d`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out EventDeletion
	for rows.Next() {
		var d DeletedEvent
		if err := rows.Scan(&d.IssueID, &d.UserLabel, &d.User, &out.Attachments, &out.Feedback); err != nil {
			return nil, err
		}
		out.Deleted = append(out.Deleted, d)
	}
	out.Events = int64(len(out.Deleted))
	return &out, rows.Err()
}

// DeleteSubjectEvents deletes up to limit events about a subject with their
// attachments and user feedback.
func DeleteSubjectEvents(ctx context.Context, q Querier, orgID uuid.UUID, s Subject, limit int) (*EventDeletion, error) {
	var args argList
	return deleteEvents(ctx, q, s.events(orgID, &args), args, limit)
}

// DeleteEvent deletes one event (by its server id and receive time window)
// with its attachments and user feedback; ErrNotFound when it is unknown.
func DeleteEvent(ctx context.Context, q Querier, id uuid.UUID, from, to time.Time) (*EventDeletion, error) {
	args := argList{id, from, to}
	d, err := deleteEvents(ctx, q, "e.id = $1 AND e.received_at BETWEEN $2 AND $3", args, 1)
	if err == nil && d.Events == 0 {
		err = ErrNotFound
	}
	return d, err
}

// ForgetEventUsers removes the tag values and affected-user records that
// named the users of deleted events in their issues. hashes are the users'
// identity hashes (issue_users.user_hash), one per deleted event (nil when
// the event had no user).
func ForgetEventUsers(ctx context.Context, q Querier, deleted []DeletedEvent, hashes [][]byte) error {
	var tagIssues, hashIssues []uuid.UUID
	var labels []string
	var userHashes [][]byte
	for i, d := range deleted {
		if d.UserLabel != "" {
			tagIssues, labels = append(tagIssues, d.IssueID), append(labels, d.UserLabel)
		}
		if i < len(hashes) && len(hashes[i]) > 0 {
			hashIssues, userHashes = append(hashIssues, d.IssueID), append(userHashes, hashes[i])
		}
	}
	if len(tagIssues) > 0 {
		if _, err := q.Exec(ctx, `
			DELETE FROM issue_tags t USING unnest($1::uuid[], $2::text[]) AS v(issue_id, value)
			WHERE t.issue_id = v.issue_id AND t.key = 'user' AND t.value = v.value`, tagIssues, labels); err != nil {
			return err
		}
	}
	if len(hashIssues) > 0 {
		if _, err := q.Exec(ctx, `
			DELETE FROM issue_users u USING unnest($1::uuid[], $2::bytea[]) AS v(issue_id, h)
			WHERE u.issue_id = v.issue_id AND u.user_hash = v.h`, hashIssues, userHashes); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSubjectRecords deletes what names the subject apart from events:
// its user feedback and, for a customer, its tag values, counters and (when
// the request covers every project) the customer itself.
func DeleteSubjectRecords(ctx context.Context, q Querier, orgID uuid.UUID, s Subject) (feedback int64, err error) {
	var args argList
	tag, err := q.Exec(ctx, `DELETE FROM user_feedback f WHERE `+s.feedback(orgID, &args), args...)
	if err != nil {
		return 0, err
	}
	feedback = tag.RowsAffected()
	if s.Customer == "" {
		return feedback, nil
	}
	project := ""
	args = argList{orgID, s.Customer}
	if s.ProjectID != nil {
		project = " AND project_id = " + args.add(*s.ProjectID)
	}
	for _, sql := range []string{
		`DELETE FROM issue_tags WHERE org_id = $1 AND key = 'customer' AND value = $2` + project,
		`DELETE FROM customer_hourly WHERE org_id = $1 AND customer_key = $2` + project,
	} {
		if _, err := q.Exec(ctx, sql, args...); err != nil {
			return feedback, err
		}
	}
	if s.ProjectID == nil {
		_, err = q.Exec(ctx, `DELETE FROM customers WHERE org_id = $1 AND key = $2`, orgID, s.Customer)
	}
	return feedback, err
}
