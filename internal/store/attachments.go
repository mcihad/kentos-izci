package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Attachment is a file sent with an event.
type Attachment struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	ProjectID      uuid.UUID
	EventID        uuid.UUID
	Name           string
	ContentType    string
	AttachmentType string
	Size           int
	// Data is only filled by GetAttachment and read by InsertAttachments.
	Data      []byte
	CreatedAt time.Time
}

// InsertAttachments stores attachments with their content.
func InsertAttachments(ctx context.Context, q Querier, list []Attachment) error {
	b := &pgx.Batch{}
	for _, a := range list {
		b.Queue(`
			INSERT INTO event_attachments (org_id, project_id, event_id, name, content_type, attachment_type, size_bytes, data)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			a.OrgID, a.ProjectID, a.EventID, a.Name, a.ContentType, a.AttachmentType, len(a.Data), a.Data)
	}
	return q.SendBatch(ctx, b).Close()
}

// ListEventAttachments returns the attachments of an event without their
// content, oldest first.
func ListEventAttachments(ctx context.Context, q Querier, projectID, eventID uuid.UUID) ([]Attachment, error) {
	rows, err := q.Query(ctx, `
		SELECT id, org_id, project_id, event_id, name, content_type, attachment_type, size_bytes, created_at
		FROM event_attachments WHERE project_id = $1 AND event_id = $2 ORDER BY id`, projectID, eventID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Attachment, error) {
		var a Attachment
		err := r.Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.EventID, &a.Name, &a.ContentType, &a.AttachmentType, &a.Size, &a.CreatedAt)
		return a, err
	})
}

// GetAttachment returns an attachment with its content.
func GetAttachment(ctx context.Context, q Querier, id uuid.UUID) (*Attachment, error) {
	var a Attachment
	err := q.QueryRow(ctx, `
		SELECT id, org_id, project_id, event_id, name, content_type, attachment_type, size_bytes, data, created_at
		FROM event_attachments WHERE id = $1`, id).
		Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.EventID, &a.Name, &a.ContentType, &a.AttachmentType, &a.Size, &a.Data, &a.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}
