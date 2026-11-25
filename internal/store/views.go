package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// View is a saved search or a dashboard: Body is its protojson (the search
// or the widget list).
type View struct {
	ID        uuid.UUID
	OwnerID   *uuid.UUID
	Name      string
	Body      []byte
	Shared    bool
	UpdatedAt time.Time
}

// viewTables maps view kinds to their table and body column.
var viewTables = map[string][2]string{
	"search":    {"saved_searches", "search"},
	"dashboard": {"dashboards", "widgets"},
}

// ListViews lists the user's and the shared views of a kind ("search" or
// "dashboard") in an organization, by name.
func ListViews(ctx context.Context, q Querier, kind string, orgID, userID uuid.UUID) ([]View, error) {
	t := viewTables[kind]
	rows, err := q.Query(ctx, `SELECT id, owner_id, name, `+t[1]+`, shared, updated_at FROM `+t[0]+`
		WHERE org_id = $1 AND (shared OR owner_id = $2) ORDER BY lower(name), id`, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []View
	for rows.Next() {
		var v View
		if err := rows.Scan(&v.ID, &v.OwnerID, &v.Name, &v.Body, &v.Shared, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetView returns a view the user may see; ErrNotFound otherwise.
func GetView(ctx context.Context, q Querier, kind string, orgID, userID, id uuid.UUID) (*View, error) {
	t := viewTables[kind]
	var v View
	err := q.QueryRow(ctx, `SELECT id, owner_id, name, `+t[1]+`, shared, updated_at FROM `+t[0]+`
		WHERE org_id = $1 AND id = $3 AND (shared OR owner_id = $2)`, orgID, userID, id).
		Scan(&v.ID, &v.OwnerID, &v.Name, &v.Body, &v.Shared, &v.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &v, nil
}

// CreateView stores a new view owned by the user.
func CreateView(ctx context.Context, q Querier, kind string, orgID, userID uuid.UUID, name string, body []byte, shared bool) (*View, error) {
	t := viewTables[kind]
	v := View{OwnerID: &userID, Name: name, Body: body, Shared: shared}
	err := q.QueryRow(ctx, `INSERT INTO `+t[0]+` (org_id, owner_id, name, `+t[1]+`, shared) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, updated_at`, orgID, userID, name, body, shared).Scan(&v.ID, &v.UpdatedAt)
	return &v, err
}

// UpdateView changes a view.
func UpdateView(ctx context.Context, q Querier, kind string, id uuid.UUID, name string, body []byte, shared bool) (*View, error) {
	t := viewTables[kind]
	var v View
	err := q.QueryRow(ctx, `UPDATE `+t[0]+` SET name = $2, `+t[1]+` = $3, shared = $4, updated_at = now() WHERE id = $1
		RETURNING id, owner_id, name, `+t[1]+`, shared, updated_at`, id, name, body, shared).
		Scan(&v.ID, &v.OwnerID, &v.Name, &v.Body, &v.Shared, &v.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &v, nil
}

// DeleteView deletes a view.
func DeleteView(ctx context.Context, q Querier, kind string, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM `+viewTables[kind][0]+` WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	return err
}
