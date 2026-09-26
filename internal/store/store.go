// Package store holds the hand-written SQL of izci.
//
// Functions take a Querier — normally the pgx.Tx handed out by db.InOrg or
// db.InSystem — so that callers decide the tenant scope and can compose
// several statements into one transaction. The store never opens its own
// tenant scope. JSON columns that mirror API messages (settings, limits) are
// stored as protojson of those messages.
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Querier is the subset of pgx.Tx (and *pgxpool.Pool) the store needs.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// ErrNotFound is returned when a looked-up row does not exist (or is not
// visible in the current tenant scope).
var ErrNotFound = errors.New("store: not found")

// notFound maps pgx.ErrNoRows to ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// UniqueViolation reports whether err is a unique-constraint violation, on
// the named constraint or index when constraint is not empty.
func UniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

var (
	jsonOut = protojson.MarshalOptions{UseProtoNames: true}
	jsonIn  = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// marshalJSON encodes a proto message for a jsonb column ("{}" for nil).
func marshalJSON(m proto.Message) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return jsonOut.Marshal(m)
}

// unmarshalJSON decodes a jsonb column into m; empty input leaves m zero.
func unmarshalJSON(data []byte, m proto.Message) error {
	if len(data) == 0 {
		return nil
	}
	return jsonIn.Unmarshal(data, m)
}
