package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User is an izci account.
type User struct {
	ID               uuid.UUID
	Email            string
	Name             string
	AvatarURL        string
	PasswordHash     *string
	Superadmin       bool
	Disabled         bool
	TelegramUserID   *int64
	TelegramUsername string
	TelegramLinkedAt *time.Time
	LastLoginAt      *time.Time
	LastSeenAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const userColumns = `id, email, name, avatar_url, password_hash, superadmin, disabled,
	telegram_user_id, telegram_username, telegram_linked_at, last_login_at, last_seen_at, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.PasswordHash, &u.Superadmin, &u.Disabled,
		&u.TelegramUserID, &u.TelegramUsername, &u.TelegramLinkedAt, &u.LastLoginAt, &u.LastSeenAt,
		&u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

// NewUserInput describes a user to create.
type NewUserInput struct {
	Email        string
	Name         string
	AvatarURL    string
	PasswordHash *string
	Superadmin   bool
}

// CreateUser inserts a user. A taken email yields a unique violation on
// "users_email_key".
func CreateUser(ctx context.Context, q Querier, in NewUserInput) (*User, error) {
	return scanUser(q.QueryRow(ctx, `
		INSERT INTO users (email, name, avatar_url, password_hash, superadmin)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+userColumns, in.Email, in.Name, in.AvatarURL, in.PasswordHash, in.Superadmin))
}

// GetUser returns a user by id.
func GetUser(ctx context.Context, q Querier, id uuid.UUID) (*User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

// GetUserByEmail returns a user by email (case-insensitive).
func GetUserByEmail(ctx context.Context, q Querier, email string) (*User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email))
}

// CountUsers returns the number of accounts (0 means setup is pending).
func CountUsers(ctx context.Context, q Querier) (int64, error) {
	var n int64
	err := q.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
