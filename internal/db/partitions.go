package db

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Events are stored in daily RANGE partitions on received_at (UTC days)
// named events_pYYYYMMDD. Creating them ahead of time keeps inserts off the
// default partition; dropping whole partitions makes retention cheap.

const partitionPrefix = "events_p"

// EventPartition is one daily partition of the events table.
type EventPartition struct {
	Name string
	Day  time.Time // UTC midnight of the covered day
}

// PartitionName returns the partition covering day.
func PartitionName(day time.Time) string {
	return partitionPrefix + day.UTC().Format("20060102")
}

func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// EnsureEventPartitions creates the daily partitions covering [from, to]
// that do not exist yet and returns the names it created.
func (d *DB) EnsureEventPartitions(ctx context.Context, from, to time.Time) ([]string, error) {
	existing, err := d.ListEventPartitions(ctx)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(existing))
	for _, p := range existing {
		have[p.Name] = true
	}

	var created []string
	for day := truncateDay(from); !day.After(truncateDay(to)); day = day.AddDate(0, 0, 1) {
		name := PartitionName(day)
		if have[name] {
			continue
		}
		ident := pgx.Identifier{name}.Sanitize()
		stmt := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF events FOR VALUES FROM ('%s') TO ('%s')`,
			ident, day.Format(time.RFC3339), day.AddDate(0, 0, 1).Format(time.RFC3339))
		if _, err := d.Pool.Exec(ctx, stmt); err != nil {
			return created, fmt.Errorf("create partition %s: %w", name, err)
		}
		// Column compression is set on the parent; repeat it on the partition
		// so the setting does not depend on inheritance rules.
		if _, err := d.Pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN payload SET COMPRESSION lz4`, ident)); err != nil {
			return created, fmt.Errorf("set compression on %s: %w", name, err)
		}
		created = append(created, name)
	}
	return created, nil
}

// ListEventPartitions returns the daily partitions, oldest first. The
// default partition is not included.
func (d *DB) ListEventPartitions(ctx context.Context) ([]EventPartition, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'events'::regclass`)
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	var out []EventPartition
	for _, name := range names {
		if !strings.HasPrefix(name, partitionPrefix) {
			continue
		}
		day, err := time.Parse("20060102", strings.TrimPrefix(name, partitionPrefix))
		if err != nil {
			continue
		}
		out = append(out, EventPartition{Name: name, Day: day})
	}
	slices.SortFunc(out, func(a, b EventPartition) int { return a.Day.Compare(b.Day) })
	return out, nil
}

// DropEventPartitionsBefore drops every daily partition whose whole day lies
// before cutoff and returns the dropped names. Dropping briefly locks the
// parent table, so it runs with a lock timeout and is meant for a quiet hour.
func (d *DB) DropEventPartitionsBefore(ctx context.Context, cutoff time.Time) ([]string, error) {
	parts, err := d.ListEventPartitions(ctx)
	if err != nil {
		return nil, err
	}
	limit := truncateDay(cutoff)
	var dropped []string
	for _, p := range parts {
		if !p.Day.AddDate(0, 0, 1).After(limit) {
			err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{p.Name}.Sanitize())
				return err
			})
			if err != nil {
				return dropped, fmt.Errorf("drop partition %s: %w", p.Name, err)
			}
			dropped = append(dropped, p.Name)
		}
	}
	return dropped, nil
}
