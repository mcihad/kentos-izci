package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Issue status and substatus values (izci.api.v1.IssueStatus/IssueSubstatus).
const (
	statusUnresolved  int16 = 1
	statusResolved    int16 = 2
	statusIgnored     int16 = 3
	substatusOngoing  int16 = 2
	substatusRegressd int16 = 3
)

type issueKey struct {
	project     uuid.UUID
	fingerprint [16]byte
}

func issueKeyOf(p *Processed) issueKey {
	return issueKey{project: p.ProjectID, fingerprint: p.Grouping.Hash}
}

// IssueOutcome describes what a batch did to one issue.
type IssueOutcome struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	OrgSlug     string
	ProjectID   uuid.UUID
	ProjectSlug string
	ShortSeq    int32
	ShortID     string
	Created     bool
	Regressed   bool
	Unignored   bool
	Status      int16
	TimesSeen   int64
	// NewEnvironments lists environments the issue was seen in for the first
	// time with this batch (all of them for a new issue).
	NewEnvironments []string
	// Events of this issue in the batch, oldest first.
	Events []*Processed
}

// Latest returns the most recent event of the issue in the batch.
func (o *IssueOutcome) Latest() *Processed { return o.Events[len(o.Events)-1] }

// Batch is one committed group of events.
type Batch struct {
	Events []*Processed
	Issues map[issueKey]*IssueOutcome
	// Order lists issue keys in lock order.
	Order []issueKey

	// transitions holds resolved/ignored issues that received events and
	// may change status.
	transitions map[uuid.UUID]issueRow

	// Notifications is set by hooks to the number of notifications the
	// batch queued, so that delivery can start right after the commit.
	Notifications int
	// PersonalNotifications is the same for members' personal
	// notifications (followers of issues that came back).
	PersonalNotifications int
}

// writeBatch persists one batch in a single system-scoped transaction.
func (pl *Pipeline) writeBatch(ctx context.Context, jobs []*job) (*Batch, error) {
	b := &Batch{Issues: map[issueKey]*IssueOutcome{}}
	for _, j := range jobs {
		p := j.p
		b.Events = append(b.Events, p)
		k := issueKeyOf(p)
		o, ok := b.Issues[k]
		if !ok {
			o = &IssueOutcome{OrgID: p.OrgID, OrgSlug: p.OrgSlug, ProjectID: p.ProjectID, ProjectSlug: p.ProjectSlug}
			b.Issues[k] = o
			b.Order = append(b.Order, k)
		}
		o.Events = append(o.Events, p)
	}
	// Consistent lock order across writers and instances prevents deadlocks.
	slices.SortFunc(b.Order, func(a, c issueKey) int {
		if r := bytes.Compare(a.project[:], c.project[:]); r != 0 {
			return r
		}
		return bytes.Compare(a.fingerprint[:], c.fingerprint[:])
	})
	for _, o := range b.Issues {
		slices.SortStableFunc(o.Events, func(a, c *Processed) int { return a.OccurredAt.Compare(c.OccurredAt) })
	}

	err := pl.db.InSystem(ctx, func(tx pgx.Tx) error {
		releases, err := upsertDimensions(ctx, tx, b)
		if err != nil {
			return err
		}
		if err := upsertIssues(ctx, tx, b); err != nil {
			return err
		}
		if err := applyTransitions(ctx, tx, b, releases); err != nil {
			return err
		}
		if err := assignShortIDs(ctx, tx, b); err != nil {
			return err
		}
		if err := insertEvents(ctx, tx, b); err != nil {
			return err
		}
		if err := writeCounters(ctx, tx, b); err != nil {
			return err
		}
		if pl.hooks.InTx != nil {
			return pl.hooks.InTx(ctx, tx, b)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// upsertDimensions records environments, releases and customers, and
// returns the first_seen time of every release in the batch (used to order
// releases for regression detection).
func upsertDimensions(ctx context.Context, tx pgx.Tx, b *Batch) (map[relKey]time.Time, error) {
	type envKey struct {
		project uuid.UUID
		name    string
	}
	type custKey struct {
		org uuid.UUID
		key string
	}
	envs := map[envKey]*Processed{}
	rels := map[relKey]*Processed{}
	custs := map[custKey]*Processed{}
	for _, p := range b.Events {
		envs[envKey{p.ProjectID, p.Environment}] = p
		if p.Release != "" {
			rels[relKey{p.ProjectID, p.Release}] = p
		}
		if p.CustomerKey != "" {
			custs[custKey{p.OrgID, p.CustomerKey}] = p
		}
	}

	var (
		eProj, eOrg []uuid.UUID
		eName       []string
		eSeen       []time.Time
	)
	for k, p := range envs {
		eProj, eOrg, eName, eSeen = append(eProj, k.project), append(eOrg, p.OrgID), append(eName, k.name), append(eSeen, p.ReceivedAt)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO environments AS e (project_id, name, org_id, first_seen, last_seen)
		SELECT p, n, o, s, s FROM unnest($1::uuid[], $2::text[], $3::uuid[], $4::timestamptz[]) AS t(p, n, o, s)
		ORDER BY p, n
		ON CONFLICT (project_id, name) DO UPDATE SET last_seen = excluded.last_seen
		WHERE e.last_seen < excluded.last_seen - interval '1 minute'`, eProj, eName, eOrg, eSeen); err != nil {
		return nil, fmt.Errorf("upsert environments: %w", err)
	}

	firstSeen := map[relKey]time.Time{}
	if len(rels) > 0 {
		var (
			rProj, rOrg []uuid.UUID
			rVer        []string
			rSeen       []time.Time
		)
		for k, p := range rels {
			rProj, rOrg, rVer, rSeen = append(rProj, k.project), append(rOrg, p.OrgID), append(rVer, k.version), append(rSeen, p.ReceivedAt)
		}
		rows, err := tx.Query(ctx, `
			INSERT INTO releases AS r (project_id, version, org_id, first_seen, last_seen)
			SELECT p, v, o, s, s FROM unnest($1::uuid[], $2::text[], $3::uuid[], $4::timestamptz[]) AS t(p, v, o, s)
			ORDER BY p, v
			ON CONFLICT (project_id, version) DO UPDATE SET last_seen = greatest(r.last_seen, excluded.last_seen)
			RETURNING r.project_id, r.version, r.first_seen`, rProj, rVer, rOrg, rSeen)
		if err != nil {
			return nil, fmt.Errorf("upsert releases: %w", err)
		}
		for rows.Next() {
			var k relKey
			var t time.Time
			if err := rows.Scan(&k.project, &k.version, &t); err != nil {
				rows.Close()
				return nil, err
			}
			firstSeen[k] = t
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("upsert releases: %w", err)
		}
	}

	if len(custs) > 0 {
		var (
			cOrg                []uuid.UUID
			cKey, cName, cAttrs []string
			cSeen               []time.Time
		)
		for k, p := range custs {
			attrs := "{}"
			if len(p.CustomerAttr) > 0 {
				js, _ := json.Marshal(p.CustomerAttr)
				attrs = string(js)
			}
			cOrg, cKey, cName, cAttrs, cSeen = append(cOrg, k.org), append(cKey, k.key), append(cName, p.CustomerName), append(cAttrs, attrs), append(cSeen, p.ReceivedAt)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO customers AS c (org_id, key, name, attributes, first_seen, last_seen)
			SELECT o, k, n, a::jsonb, s, s FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::timestamptz[]) AS t(o, k, n, a, s)
			ORDER BY o, k
			ON CONFLICT (org_id, key) DO UPDATE SET
			  name = CASE WHEN c.name_locked OR excluded.name = '' THEN c.name ELSE excluded.name END,
			  attributes = c.attributes || excluded.attributes,
			  last_seen = greatest(c.last_seen, excluded.last_seen)
			WHERE c.last_seen < excluded.last_seen - interval '1 minute'
			   OR (NOT c.name_locked AND excluded.name <> '' AND c.name IS DISTINCT FROM excluded.name)
			   OR NOT (c.attributes @> excluded.attributes)`, cOrg, cKey, cName, cAttrs, cSeen); err != nil {
			return nil, fmt.Errorf("upsert customers: %w", err)
		}
	}
	return firstSeen, nil
}

type relKey struct {
	project uuid.UUID
	version string
}

// issueRow carries the pre-update state needed for status transitions.
type issueRow struct {
	prevStatus      *int16
	resolvedIn      string
	resolvedNext    bool
	snoozeUntil     *time.Time
	snoozeCount     *int64
	snoozeBase      *int64
	latestRelease   string
	transitionCheck bool
}

func upsertIssues(ctx context.Context, tx pgx.Tx, b *Batch) error {
	n := len(b.Order)
	var (
		org, proj, lastEvent          = make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n)
		fp                            = make([][]byte, n)
		grouping, meta                = make([]string, n), make([]string, n)
		title, culprit, typ, val, plt = make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		firstRel, lastRel             = make([]string, n), make([]string, n)
		level                         = make([]int16, n)
		unhandled                     = make([]bool, n)
		first, last                   = make([]time.Time, n), make([]time.Time, n)
		count                         = make([]int64, n)
	)
	for i, k := range b.Order {
		o := b.Issues[k]
		oldest, newest := o.Events[0], o.Latest()
		org[i], proj[i], lastEvent[i] = o.OrgID, o.ProjectID, newest.ID
		fp[i] = k.fingerprint[:]
		g, _ := json.Marshal(map[string]any{
			"version": GroupingVersion, "strategy": newest.Grouping.Strategy, "components": newest.Grouping.Components,
		})
		grouping[i] = string(g)
		m, _ := json.Marshal(newest.Location)
		meta[i] = string(m)
		title[i], culprit[i], typ[i], val[i], plt[i] = newest.Title, newest.Culprit, newest.Type, newest.Value, newest.Platform
		firstRel[i], lastRel[i] = oldest.Release, newest.Release
		first[i], last[i] = oldest.OccurredAt, newest.OccurredAt
		count[i] = int64(len(o.Events))
		for _, p := range o.Events {
			level[i] = max(level[i], int16(p.Level))
			unhandled[i] = unhandled[i] || p.Unhandled
		}
	}

	rows, err := tx.Query(ctx, `
		INSERT INTO issues AS i (org_id, project_id, fingerprint, grouping, title, culprit, type, value, platform,
		                         level, unhandled, first_seen, last_seen, times_seen, first_release, last_release,
		                         last_event_id, metadata)
		SELECT o, p, f, g::jsonb, ti, cu, ty, va, pl, lv, un, fs, ls, ct, fr, lr, le, me::jsonb
		FROM unnest($1::uuid[], $2::uuid[], $3::bytea[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[],
		            $9::text[], $10::smallint[], $11::bool[], $12::timestamptz[], $13::timestamptz[], $14::bigint[],
		            $15::text[], $16::text[], $17::uuid[], $18::text[])
		     AS t(o, p, f, g, ti, cu, ty, va, pl, lv, un, fs, ls, ct, fr, lr, le, me)
		ON CONFLICT (project_id, fingerprint) DO UPDATE SET
		  level         = greatest(i.level, excluded.level),
		  unhandled     = i.unhandled OR excluded.unhandled,
		  first_seen    = least(i.first_seen, excluded.first_seen),
		  last_seen     = greatest(i.last_seen, excluded.last_seen),
		  times_seen    = i.times_seen + excluded.times_seen,
		  last_release  = CASE WHEN excluded.last_seen >= i.last_seen AND excluded.last_release <> ''
		                       THEN excluded.last_release ELSE i.last_release END,
		  last_event_id = CASE WHEN excluded.last_seen >= i.last_seen THEN excluded.last_event_id ELSE i.last_event_id END,
		  updated_at    = now()
		RETURNING i.project_id, i.fingerprint, i.id, coalesce(i.short_seq, 0), i.status, i.times_seen,
		          old.id IS NULL, old.status, i.resolved_in_release, i.resolved_in_next_release,
		          i.snooze_until, i.snooze_count, i.snooze_base`,
		org, proj, fp, grouping, title, culprit, typ, val, plt, level, unhandled, first, last, count, firstRel, lastRel, lastEvent, meta)
	if err != nil {
		return fmt.Errorf("upsert issues: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k issueKey
		var fpb []byte
		var id uuid.UUID
		var seq int32
		var status int16
		var times int64
		var created bool
		var r issueRow
		if err := rows.Scan(&k.project, &fpb, &id, &seq, &status, &times, &created, &r.prevStatus,
			&r.resolvedIn, &r.resolvedNext, &r.snoozeUntil, &r.snoozeCount, &r.snoozeBase); err != nil {
			return fmt.Errorf("scan issue: %w", err)
		}
		copy(k.fingerprint[:], fpb)
		o := b.Issues[k]
		if o == nil {
			return fmt.Errorf("upsert issues: unexpected returned issue %s", id)
		}
		o.ID, o.ShortSeq, o.Status, o.TimesSeen, o.Created = id, seq, status, times, created
		if !created && r.prevStatus != nil && (*r.prevStatus == statusResolved || *r.prevStatus == statusIgnored) {
			r.latestRelease = o.Latest().Release
			r.transitionCheck = true
			pendingTransitions(b)[o.ID] = r
		}
	}
	return rows.Err()
}

func pendingTransitions(b *Batch) map[uuid.UUID]issueRow {
	if b.transitions == nil {
		b.transitions = map[uuid.UUID]issueRow{}
	}
	return b.transitions
}

// applyTransitions reopens resolved issues that regressed and unignores
// issues whose ignore condition expired.
func applyTransitions(ctx context.Context, tx pgx.Tx, b *Batch, releases map[relKey]time.Time) error {
	if len(b.transitions) == 0 {
		return nil
	}
	// Release ordering needs the first_seen of the resolved-in releases too.
	byProject := map[uuid.UUID][]string{}
	for id, r := range b.transitions {
		if r.resolvedIn == "" {
			continue
		}
		proj := b.issueByID(id).ProjectID
		if _, ok := releases[relKey{proj, r.resolvedIn}]; !ok {
			byProject[proj] = append(byProject[proj], r.resolvedIn)
		}
	}
	for proj, versions := range byProject {
		rows, err := tx.Query(ctx, `SELECT version, first_seen FROM releases WHERE project_id = $1 AND version = ANY($2)`, proj, versions)
		if err != nil {
			return fmt.Errorf("load releases: %w", err)
		}
		for rows.Next() {
			var v string
			var t time.Time
			if err := rows.Scan(&v, &t); err != nil {
				rows.Close()
				return err
			}
			releases[relKey{proj, v}] = t
		}
		rows.Close()
	}

	now := time.Now()
	var regressed, unignored []uuid.UUID
	for id, r := range b.transitions {
		o := b.issueByID(id)
		switch *r.prevStatus {
		case statusResolved:
			if isRegression(o.ProjectID, r.resolvedIn, r.resolvedNext, r.latestRelease, releases) {
				regressed = append(regressed, id)
				o.Regressed, o.Status = true, statusUnresolved
			}
		case statusIgnored:
			if snoozeExpired(now, r.snoozeUntil, r.snoozeCount, r.snoozeBase, o.TimesSeen) {
				unignored = append(unignored, id)
				o.Unignored, o.Status = true, statusUnresolved
			}
		}
	}
	slices.SortFunc(regressed, func(a, c uuid.UUID) int { return bytes.Compare(a[:], c[:]) })
	slices.SortFunc(unignored, func(a, c uuid.UUID) int { return bytes.Compare(a[:], c[:]) })
	if len(regressed) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE issues SET status = $2, substatus = $3, resolved_at = NULL, resolved_by = NULL,
			  resolved_in_release = '', resolved_in_next_release = false, updated_at = now()
			WHERE id = ANY($1)`, regressed, statusUnresolved, substatusRegressd); err != nil {
			return fmt.Errorf("reopen regressed issues: %w", err)
		}
		if err := insertActivity(ctx, tx, b, regressed, 6); err != nil {
			return err
		}
	}
	if len(unignored) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE issues SET status = $2, substatus = $3, snooze_until = NULL, snooze_count = NULL,
			  snooze_base = NULL, snooze_until_escalating = false, updated_at = now()
			WHERE id = ANY($1)`, unignored, statusUnresolved, substatusOngoing); err != nil {
			return fmt.Errorf("unignore issues: %w", err)
		}
	}
	return nil
}

func (b *Batch) issueByID(id uuid.UUID) *IssueOutcome {
	for _, o := range b.Issues {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// isRegression decides whether an event in eventRelease reopens an issue
// resolved in resolvedIn (see ResolveOptions in the API):
//   - plain resolution: any new event regresses;
//   - "in next release": only events from a release newer than resolvedIn;
//   - "in release R": events from R or newer.
func isRegression(project uuid.UUID, resolvedIn string, next bool, eventRelease string, firstSeen map[relKey]time.Time) bool {
	switch {
	case resolvedIn == "":
		return true
	case eventRelease == "":
		return !next
	case eventRelease == resolvedIn:
		return !next
	}
	e, eok := firstSeen[relKey{project, eventRelease}]
	r, rok := firstSeen[relKey{project, resolvedIn}]
	if !eok || !rok {
		return true
	}
	if next {
		return e.After(r)
	}
	return !e.Before(r)
}

// snoozeExpired reports whether an ignored issue must come back.
func snoozeExpired(now time.Time, until *time.Time, count, base *int64, timesSeen int64) bool {
	if until != nil && !until.After(now) {
		return true
	}
	if count != nil {
		b := int64(0)
		if base != nil {
			b = *base
		}
		return timesSeen-b >= *count
	}
	return false
}

// assignShortIDs hands out per-project sequence numbers to new issues.
func assignShortIDs(ctx context.Context, tx pgx.Tx, b *Batch) error {
	perProject := map[uuid.UUID][]*IssueOutcome{}
	var projects []uuid.UUID
	for _, k := range b.Order {
		o := b.Issues[k]
		if o.Created {
			if _, ok := perProject[o.ProjectID]; !ok {
				projects = append(projects, o.ProjectID)
			}
			perProject[o.ProjectID] = append(perProject[o.ProjectID], o)
		}
	}
	if len(projects) == 0 {
		for _, o := range b.Issues {
			o.ShortID = ShortID(o.ProjectSlug, o.ShortSeq)
		}
		return nil
	}
	slices.SortFunc(projects, func(a, c uuid.UUID) int { return bytes.Compare(a[:], c[:]) })
	counts := make([]int32, len(projects))
	for i, p := range projects {
		counts[i] = int32(len(perProject[p]))
	}
	rows, err := tx.Query(ctx, `
		UPDATE projects AS p SET issue_seq = p.issue_seq + t.n
		FROM unnest($1::uuid[], $2::int[]) AS t(id, n)
		WHERE p.id = t.id
		RETURNING p.id, p.issue_seq`, projects, counts)
	if err != nil {
		return fmt.Errorf("reserve issue numbers: %w", err)
	}
	var ids []uuid.UUID
	var seqs []int32
	for rows.Next() {
		var proj uuid.UUID
		var last int32
		if err := rows.Scan(&proj, &last); err != nil {
			rows.Close()
			return err
		}
		list := perProject[proj]
		for i, o := range list {
			o.ShortSeq = last - int32(len(list)) + 1 + int32(i)
			ids, seqs = append(ids, o.ID), append(seqs, o.ShortSeq)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reserve issue numbers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues AS i SET short_seq = t.seq FROM unnest($1::uuid[], $2::int[]) AS t(id, seq)
		WHERE i.id = t.id`, ids, seqs); err != nil {
		return fmt.Errorf("assign issue numbers: %w", err)
	}
	for _, o := range b.Issues {
		o.ShortID = ShortID(o.ProjectSlug, o.ShortSeq)
	}
	if err := insertActivity(ctx, tx, b, ids, 1); err != nil {
		return err
	}
	return nil
}

// insertActivity records system activity (created, regressed) for issues.
func insertActivity(ctx context.Context, tx pgx.Tx, b *Batch, ids []uuid.UUID, kind int16) error {
	if len(ids) == 0 {
		return nil
	}
	orgs := make([]uuid.UUID, len(ids))
	data := make([]string, len(ids))
	for i, id := range ids {
		o := b.issueByID(id)
		orgs[i] = o.OrgID
		js, _ := json.Marshal(map[string]string{"release": o.Latest().Release, "event_id": o.Latest().ID.String()})
		data[i] = string(js)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO issue_activity (org_id, issue_id, kind, data)
		SELECT o, i, $3, d::jsonb FROM unnest($1::uuid[], $2::uuid[], $4::text[]) AS t(o, i, d)`,
		orgs, ids, kind, data)
	if err != nil {
		return fmt.Errorf("record issue activity: %w", err)
	}
	return nil
}

func insertEvents(ctx context.Context, tx pgx.Tx, b *Batch) error {
	n := len(b.Events)
	var (
		id, org, proj, issue, eventID                = make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n)
		received, occurred                           = make([]time.Time, n), make([]time.Time, n)
		level, score                                 = make([]int16, n), make([]int16, n)
		env, rel, cust, tenant, user, ip, plt, srv   = make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		txn, url, browser, osName, title, warn, body = make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		unhandled                                    = make([]bool, n)
		size                                         = make([]int32, n)
	)
	for i, p := range b.Events {
		id[i], org[i], proj[i], eventID[i] = p.ID, p.OrgID, p.ProjectID, p.EventID
		issue[i] = b.Issues[issueKeyOf(p)].ID
		received[i], occurred[i] = p.ReceivedAt, p.OccurredAt
		level[i], score[i] = int16(p.Level), p.Score
		env[i], rel[i], cust[i], tenant[i], user[i] = p.Environment, p.Release, p.CustomerKey, p.TenantKey, p.UserLabel
		if p.ClientIP.IsValid() {
			ip[i] = p.ClientIP.String()
		}
		plt[i], srv[i], txn[i], url[i] = p.Platform, p.ServerName, p.Transaction, p.URL
		browser[i], osName[i], title[i] = p.Browser, p.OS, p.Title
		w := p.Warnings
		if w == nil {
			w = []string{}
		}
		js, _ := json.Marshal(w)
		warn[i] = string(js)
		body[i] = string(p.Payload)
		unhandled[i] = p.Unhandled
		size[i] = int32(len(p.Payload))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO events (id, received_at, org_id, project_id, issue_id, event_id, occurred_at, level,
		                    environment, release, customer_key, tenant_key, user_label, client_ip, platform,
		                    server_name, transaction, url, browser, os, title, score, unhandled, size_bytes,
		                    warnings, payload)
		SELECT id, ra, o, p, i, e, oa, lv, en, rl, cu, te, us, nullif(ip, '')::inet, pl, sv, tx, ur, br, os, ti,
		       sc, un, sz, wa::jsonb, pa::jsonb
		FROM unnest($1::uuid[], $2::timestamptz[], $3::uuid[], $4::uuid[], $5::uuid[], $6::uuid[], $7::timestamptz[],
		            $8::smallint[], $9::text[], $10::text[], $11::text[], $12::text[], $13::text[], $14::text[],
		            $15::text[], $16::text[], $17::text[], $18::text[], $19::text[], $20::text[], $21::text[],
		            $22::smallint[], $23::bool[], $24::int[], $25::text[], $26::text[])
		     AS t(id, ra, o, p, i, e, oa, lv, en, rl, cu, te, us, ip, pl, sv, tx, ur, br, os, ti, sc, un, sz, wa, pa)`,
		id, received, org, proj, issue, eventID, occurred, level, env, rel, cust, tenant, user, ip, plt,
		srv, txn, url, browser, osName, title, score, unhandled, size, warn, body)
	if err != nil {
		return fmt.Errorf("insert events: %w", err)
	}
	return nil
}

// writeCounters updates hourly series, facets, distinct users, usage and
// project activity in one pipelined round trip, then adjusts the per-issue
// distinct counters.
func writeCounters(ctx context.Context, tx pgx.Tx, b *Batch) error {
	type hourKey struct {
		issue uuid.UUID
		hour  time.Time
	}
	type projHourKey struct {
		project uuid.UUID
		hour    time.Time
		level   int16
	}
	type custHourKey struct {
		org      uuid.UUID
		customer string
		project  uuid.UUID
		hour     time.Time
	}
	type tagKey struct {
		issue      uuid.UUID
		key, value string
	}
	type tagAgg struct {
		org, project uuid.UUID
		count        int64
		first, last  time.Time
	}
	type userKey struct {
		issue uuid.UUID
		hash  string
	}
	type usageKey struct {
		org uuid.UUID
		day time.Time
	}

	issueHours := map[hourKey]int64{}
	issueHourMeta := map[uuid.UUID][2]uuid.UUID{}
	projHours := map[projHourKey]int64{}
	projOrg := map[uuid.UUID]uuid.UUID{}
	custHours := map[custHourKey]int64{}
	tags := map[tagKey]*tagAgg{}
	users := map[userKey]uuid.UUID{}
	subjects := map[userKey]uuid.UUID{}
	usage := map[usageKey]int64{}
	lastEvent := map[uuid.UUID]time.Time{}

	for _, p := range b.Events {
		issue := b.Issues[issueKeyOf(p)].ID
		hour := p.OccurredAt.Truncate(time.Hour)
		issueHours[hourKey{issue, hour}]++
		issueHourMeta[issue] = [2]uuid.UUID{p.OrgID, p.ProjectID}
		projHours[projHourKey{p.ProjectID, hour, int16(p.Level)}]++
		projOrg[p.ProjectID] = p.OrgID
		if p.CustomerKey != "" {
			custHours[custHourKey{p.OrgID, p.CustomerKey, p.ProjectID, hour}]++
		}
		for k, v := range p.Tags {
			tk := tagKey{issue, k, v}
			a := tags[tk]
			if a == nil {
				a = &tagAgg{org: p.OrgID, project: p.ProjectID, first: p.OccurredAt, last: p.OccurredAt}
				tags[tk] = a
			}
			a.count++
			if p.OccurredAt.Before(a.first) {
				a.first = p.OccurredAt
			}
			if p.OccurredAt.After(a.last) {
				a.last = p.OccurredAt
			}
		}
		if len(p.UserHash) > 0 {
			users[userKey{issue, string(p.UserHash)}] = p.OrgID
		}
		for _, h := range p.SubjectHashes {
			subjects[userKey{issue, string(h)}] = p.OrgID
		}
		day := time.Date(p.ReceivedAt.Year(), p.ReceivedAt.Month(), p.ReceivedAt.Day(), 0, 0, 0, 0, time.UTC)
		usage[usageKey{p.OrgID, day}]++
		if p.ReceivedAt.After(lastEvent[p.ProjectID]) {
			lastEvent[p.ProjectID] = p.ReceivedAt
		}
	}

	batch := &pgx.Batch{}

	{
		keys := sortedMapKeys(issueHours, func(a, c hourKey) int {
			if r := bytes.Compare(a.issue[:], c.issue[:]); r != 0 {
				return r
			}
			return a.hour.Compare(c.hour)
		})
		var iss, org, proj []uuid.UUID
		var hrs []time.Time
		var cnt []int64
		for _, k := range keys {
			m := issueHourMeta[k.issue]
			iss, hrs, org, proj, cnt = append(iss, k.issue), append(hrs, k.hour), append(org, m[0]), append(proj, m[1]), append(cnt, issueHours[k])
		}
		batch.Queue(`
			INSERT INTO issue_hourly AS h (issue_id, hour, org_id, project_id, count)
			SELECT * FROM unnest($1::uuid[], $2::timestamptz[], $3::uuid[], $4::uuid[], $5::bigint[])
			ON CONFLICT (issue_id, hour) DO UPDATE SET count = h.count + excluded.count`, iss, hrs, org, proj, cnt)
	}
	{
		keys := sortedMapKeys(projHours, func(a, c projHourKey) int {
			if r := bytes.Compare(a.project[:], c.project[:]); r != 0 {
				return r
			}
			if r := a.hour.Compare(c.hour); r != 0 {
				return r
			}
			return int(a.level - c.level)
		})
		var proj, org []uuid.UUID
		var hrs []time.Time
		var lvl []int16
		var cnt []int64
		for _, k := range keys {
			proj, hrs, lvl, org, cnt = append(proj, k.project), append(hrs, k.hour), append(lvl, k.level), append(org, projOrg[k.project]), append(cnt, projHours[k])
		}
		batch.Queue(`
			INSERT INTO project_hourly AS h (project_id, hour, level, org_id, count)
			SELECT * FROM unnest($1::uuid[], $2::timestamptz[], $3::smallint[], $4::uuid[], $5::bigint[])
			ON CONFLICT (project_id, hour, level) DO UPDATE SET count = h.count + excluded.count`, proj, hrs, lvl, org, cnt)
	}
	if len(custHours) > 0 {
		keys := sortedMapKeys(custHours, func(a, c custHourKey) int {
			if r := bytes.Compare(a.org[:], c.org[:]); r != 0 {
				return r
			}
			if a.customer != c.customer {
				return compareStrings(a.customer, c.customer)
			}
			if r := bytes.Compare(a.project[:], c.project[:]); r != 0 {
				return r
			}
			return a.hour.Compare(c.hour)
		})
		var org, proj []uuid.UUID
		var cust []string
		var hrs []time.Time
		var cnt []int64
		for _, k := range keys {
			org, cust, proj, hrs, cnt = append(org, k.org), append(cust, k.customer), append(proj, k.project), append(hrs, k.hour), append(cnt, custHours[k])
		}
		batch.Queue(`
			INSERT INTO customer_hourly AS h (org_id, customer_key, project_id, hour, count)
			SELECT * FROM unnest($1::uuid[], $2::text[], $3::uuid[], $4::timestamptz[], $5::bigint[])
			ON CONFLICT (org_id, customer_key, project_id, hour) DO UPDATE SET count = h.count + excluded.count`, org, cust, proj, hrs, cnt)
	}

	var newCustomerIssues []uuid.UUID
	if len(tags) > 0 {
		keys := sortedMapKeys(tags, func(a, c tagKey) int {
			if r := bytes.Compare(a.issue[:], c.issue[:]); r != 0 {
				return r
			}
			if a.key != c.key {
				return compareStrings(a.key, c.key)
			}
			return compareStrings(a.value, c.value)
		})
		var iss, org, proj []uuid.UUID
		var key, val []string
		var cnt []int64
		var first, last []time.Time
		for _, k := range keys {
			a := tags[k]
			iss, key, val, org, proj = append(iss, k.issue), append(key, k.key), append(val, k.value), append(org, a.org), append(proj, a.project)
			cnt, first, last = append(cnt, a.count), append(first, a.first), append(last, a.last)
		}
		batch.Queue(`
			INSERT INTO issue_tags AS g (issue_id, key, value, org_id, project_id, count, first_seen, last_seen)
			SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[], $4::uuid[], $5::uuid[], $6::bigint[],
			                     $7::timestamptz[], $8::timestamptz[])
			ON CONFLICT (issue_id, key, value) DO UPDATE SET
			  count = g.count + excluded.count,
			  first_seen = least(g.first_seen, excluded.first_seen),
			  last_seen = greatest(g.last_seen, excluded.last_seen)
			RETURNING g.issue_id, g.key, g.value, old.issue_id IS NULL`, iss, key, val, org, proj, cnt, first, last).
			Query(func(rows pgx.Rows) error {
				for rows.Next() {
					var id uuid.UUID
					var k, v string
					var created bool
					if err := rows.Scan(&id, &k, &v, &created); err != nil {
						return err
					}
					if !created {
						continue
					}
					switch k {
					case "customer":
						newCustomerIssues = append(newCustomerIssues, id)
					case "environment":
						if o := b.issueByID(id); o != nil {
							o.NewEnvironments = append(o.NewEnvironments, v)
						}
					}
				}
				return rows.Err()
			})
	}

	var newUserIssues []uuid.UUID
	if len(users) > 0 {
		keys := sortedMapKeys(users, func(a, c userKey) int {
			if r := bytes.Compare(a.issue[:], c.issue[:]); r != 0 {
				return r
			}
			return compareStrings(a.hash, c.hash)
		})
		var iss, org []uuid.UUID
		var hashes [][]byte
		for _, k := range keys {
			iss, hashes, org = append(iss, k.issue), append(hashes, []byte(k.hash)), append(org, users[k])
		}
		batch.Queue(`
			INSERT INTO issue_users (issue_id, user_hash, org_id)
			SELECT * FROM unnest($1::uuid[], $2::bytea[], $3::uuid[])
			ON CONFLICT (issue_id, user_hash) DO NOTHING
			RETURNING issue_id`, iss, hashes, org).
			Query(func(rows pgx.Rows) error {
				for rows.Next() {
					var id uuid.UUID
					if err := rows.Scan(&id); err != nil {
						return err
					}
					newUserIssues = append(newUserIssues, id)
				}
				return rows.Err()
			})
	}
	if len(subjects) > 0 {
		// Sorted, like every upsert here, so concurrent batches lock rows in
		// the same order.
		keys := sortedMapKeys(subjects, func(a, c userKey) int {
			if r := bytes.Compare(a.issue[:], c.issue[:]); r != 0 {
				return r
			}
			return compareStrings(a.hash, c.hash)
		})
		var iss, org []uuid.UUID
		var hashes [][]byte
		for _, k := range keys {
			iss, hashes, org = append(iss, k.issue), append(hashes, []byte(k.hash)), append(org, subjects[k])
		}
		batch.Queue(`
			INSERT INTO issue_subjects (issue_id, key_hash, org_id)
			SELECT * FROM unnest($1::uuid[], $2::bytea[], $3::uuid[])
			ON CONFLICT (issue_id, key_hash) DO NOTHING`, iss, hashes, org)
	}

	{
		var org []uuid.UUID
		var days []time.Time
		var cnt []int64
		for k, n := range usage {
			org, days, cnt = append(org, k.org), append(days, k.day), append(cnt, n)
		}
		batch.Queue(`
			INSERT INTO usage_daily AS u (org_id, day, accepted)
			SELECT o, d::date, c FROM unnest($1::uuid[], $2::timestamptz[], $3::bigint[]) AS t(o, d, c)
			ORDER BY o, d
			ON CONFLICT (org_id, day) DO UPDATE SET accepted = u.accepted + excluded.accepted`, org, days, cnt)
	}
	{
		var proj []uuid.UUID
		var at []time.Time
		for p, t := range lastEvent {
			proj, at = append(proj, p), append(at, t)
		}
		batch.Queue(`
			UPDATE projects AS p SET last_event_at = t.at
			FROM unnest($1::uuid[], $2::timestamptz[]) AS t(id, at)
			WHERE p.id = t.id AND (p.last_event_at IS NULL OR p.last_event_at < t.at - interval '10 seconds')`, proj, at)
	}
	{
		// New releases gain the issues first seen in them.
		type k struct {
			project uuid.UUID
			version string
		}
		counts := map[k]int32{}
		for _, o := range b.Issues {
			if o.Created && o.Events[0].Release != "" {
				counts[k{o.ProjectID, o.Events[0].Release}]++
			}
		}
		if len(counts) > 0 {
			var proj []uuid.UUID
			var ver []string
			var cnt []int32
			for key, n := range counts {
				proj, ver, cnt = append(proj, key.project), append(ver, key.version), append(cnt, n)
			}
			batch.Queue(`
				UPDATE releases AS r SET new_issues = r.new_issues + t.n
				FROM unnest($1::uuid[], $2::text[], $3::int[]) AS t(p, v, n)
				WHERE r.project_id = t.p AND r.version = t.v`, proj, ver, cnt)
		}
	}

	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("write counters: %w", err)
	}

	if len(newCustomerIssues) > 0 || len(newUserIssues) > 0 {
		userInc := countIDs(newUserIssues)
		custInc := countIDs(newCustomerIssues)
		ids := unionIDs(userInc, custInc)
		uc, cc := make([]int32, len(ids)), make([]int32, len(ids))
		for i, id := range ids {
			uc[i], cc[i] = userInc[id], custInc[id]
		}
		if _, err := tx.Exec(ctx, `
			UPDATE issues AS i SET users_count = i.users_count + t.u, customers_count = i.customers_count + t.c
			FROM unnest($1::uuid[], $2::int[], $3::int[]) AS t(id, u, c)
			WHERE i.id = t.id`, ids, uc, cc); err != nil {
			return fmt.Errorf("update distinct counters: %w", err)
		}
	}
	return nil
}

func sortedMapKeys[K comparable, V any](m map[K]V, cmp func(a, b K) int) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, cmp)
	return keys
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func countIDs(ids []uuid.UUID) map[uuid.UUID]int32 {
	m := map[uuid.UUID]int32{}
	for _, id := range ids {
		m[id]++
	}
	return m
}

func unionIDs(a, b map[uuid.UUID]int32) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, m := range []map[uuid.UUID]int32{a, b} {
		for id := range m {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	slices.SortFunc(out, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
	return out
}
