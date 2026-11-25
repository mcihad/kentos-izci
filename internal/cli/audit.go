package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/store"
)

// cliAudit records an action taken with the CLI in the instance-level
// audit log, with the operating system user and host that ran it. Changes
// to a user's account security (userID set) also go to the audit log of
// the user's organizations. A failure is reported, not returned: the
// action is done.
func cliAudit(ctx context.Context, database *db.DB, errOut io.Writer, e store.AuditEntry, userID *uuid.UUID) {
	meta := map[string]string{"via": "cli"}
	for k, v := range e.Metadata {
		meta[k] = v
	}
	if u, err := user.Current(); err == nil {
		meta["os_user"] = u.Username
	}
	if h, err := os.Hostname(); err == nil {
		meta["host"] = h
	}
	e.Metadata = meta
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		if userID != nil {
			return store.InsertUserAudit(ctx, tx, *userID, e)
		}
		return store.InsertAudit(ctx, tx, e)
	})
	if err != nil {
		fmt.Fprintf(errOut, "warning: the audit log could not record this: %v\n", err)
	}
}

// userEntry is the audit entry of an action on a user.
func userEntry(action string, u *store.User) store.AuditEntry {
	return store.AuditEntry{Action: action, TargetType: "user", TargetID: u.ID.String(), TargetName: u.Email}
}
