// Package digest sends members a summary of their organizations by
// e-mail, every morning or on Monday mornings (NotificationSettings.digest):
// the events of the period and its new, regressed and fastest growing
// issues.
package digest

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"slices"
	"strings"
	ttemplate "text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

// Mailer sends e-mail (notify.Email).
type Mailer interface {
	SendMail(ctx context.Context, to, subject, text, htmlBody string) error
}

// sendHour is when digests go out, Istanbul time.
const sendHour = 8

// listLength is how many issues each list of a digest shows.
const listLength = 5

// Service sends the digests that are due.
type Service struct {
	db        *db.DB
	mailer    Mailer
	publicURL string
	log       *slog.Logger
	loc       *time.Location
	// Now is the clock; tests set it.
	Now func() time.Time
}

// New creates the service; it sends nothing without a mailer.
func New(database *db.DB, mailer Mailer, publicURL string, log *slog.Logger) *Service {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		loc = time.UTC
	}
	return &Service{db: database, mailer: mailer, publicURL: strings.TrimRight(publicURL, "/"), log: log, loc: loc, Now: time.Now}
}

// Slot returns the latest send time of a frequency at or before now and
// the period that digest covers; ok is false for no digest.
func (s *Service) Slot(freq apiv1.DigestFrequency, now time.Time) (start, slot time.Time, ok bool) {
	local := now.In(s.loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), sendHour, 0, 0, 0, s.loc)
	switch freq {
	case apiv1.DigestFrequency_DIGEST_FREQUENCY_DAILY:
		slot = today
		if local.Before(slot) {
			slot = slot.AddDate(0, 0, -1)
		}
		return slot.AddDate(0, 0, -1), slot, true
	case apiv1.DigestFrequency_DIGEST_FREQUENCY_WEEKLY:
		monday := (int(local.Weekday()) + 6) % 7
		slot = today.AddDate(0, 0, -monday)
		if local.Before(slot) {
			slot = slot.AddDate(0, 0, -7)
		}
		return slot.AddDate(0, 0, -7), slot, true
	}
	return time.Time{}, time.Time{}, false
}

// Run sends every digest that is due. One server instance works at a time.
func (s *Service) Run(ctx context.Context) error {
	if s.mailer == nil {
		return nil
	}
	conn, err := s.db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('izci:digest'))`).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('izci:digest'))`) }()

	type member struct {
		id    uuid.UUID
		email string
		freq  apiv1.DigestFrequency
		last  *time.Time
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, email, notification_settings ->> 'digest', digest_sent_at FROM users
		WHERE NOT disabled AND notification_settings ->> 'digest' IN ('DIGEST_FREQUENCY_DAILY', 'DIGEST_FREQUENCY_WEEKLY')`)
	if err != nil {
		return err
	}
	var members []member
	for rows.Next() {
		var m member
		var freq string
		if err := rows.Scan(&m.id, &m.email, &freq, &m.last); err != nil {
			rows.Close()
			return err
		}
		m.freq = apiv1.DigestFrequency(apiv1.DigestFrequency_value[freq])
		members = append(members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := s.Now()
	for _, m := range members {
		start, slot, ok := s.Slot(m.freq, now)
		if !ok || (m.last != nil && !m.last.Before(slot)) {
			continue
		}
		if err := s.send(ctx, m.id, m.email, m.freq, start, slot); err != nil {
			s.log.Warn("digest not sent", slog.Any("err", err))
			continue
		}
		if _, err := s.db.Pool.Exec(ctx, `UPDATE users SET digest_sent_at = $2 WHERE id = $1`, m.id, now); err != nil {
			return err
		}
	}
	return nil
}

// Issue is a line of a digest.
type Issue struct {
	ShortID, Title, URL string
	Events, Previous    int64
}

// Org is one organization's part of a digest.
type Org struct {
	Name, URL               string
	Events                  int64
	NewCount                int
	New, Regressed, Growing []Issue
}

func (o *Org) empty() bool {
	return o.Events == 0 && len(o.New) == 0 && len(o.Regressed) == 0
}

// Build gathers a member's digest for [start, end).
func (s *Service) Build(ctx context.Context, userID uuid.UUID, start, end time.Time) ([]*Org, error) {
	memberships, err := store.ListMemberships(ctx, s.db.Pool, userID)
	if err != nil {
		return nil, err
	}
	prev := start.Add(-end.Sub(start))
	var out []*Org
	for _, m := range memberships {
		o := &Org{Name: m.Organization.Name, URL: s.publicURL + "/o/" + m.Organization.Slug + "/issues"}
		err := s.db.InOrg(ctx, m.Organization.ID, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT coalesce(sum(count), 0) FROM project_hourly WHERE org_id = $1 AND hour >= $2 AND hour < $3`,
				m.Organization.ID, start, end).Scan(&o.Events); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT p.slug, coalesce(i.short_seq, 0), i.title, i.first_seen >= $2,
				  EXISTS (SELECT 1 FROM issue_activity a WHERE a.issue_id = i.id AND a.kind = $5 AND a.created_at >= $2 AND a.created_at < $3),
				  coalesce((SELECT sum(count) FROM issue_hourly h WHERE h.issue_id = i.id AND h.hour >= $2 AND h.hour < $3), 0),
				  coalesce((SELECT sum(count) FROM issue_hourly h WHERE h.issue_id = i.id AND h.hour >= $4 AND h.hour < $2), 0)
				FROM issues i JOIN projects p ON p.id = i.project_id AND p.deleted_at IS NULL
				WHERE i.org_id = $1 AND i.last_seen >= $2
				ORDER BY i.last_seen DESC LIMIT 5000`,
				m.Organization.ID, start, end, prev, int16(apiv1.ActivityKind_ACTIVITY_KIND_REGRESSED))
			if err != nil {
				return err
			}
			defer rows.Close()
			var fresh, back, growing []Issue
			for rows.Next() {
				var slug, title string
				var seq int32
				var isNew, regressed bool
				var cur, before int64
				if err := rows.Scan(&slug, &seq, &title, &isNew, &regressed, &cur, &before); err != nil {
					return err
				}
				short := ingest.ShortID(slug, seq)
				line := Issue{ShortID: short, Title: title, URL: s.publicURL + "/o/" + m.Organization.Slug + "/issues/" + short,
					Events: cur, Previous: before}
				switch {
				case isNew:
					fresh = append(fresh, line)
				case regressed:
					back = append(back, line)
				case cur >= 10 && cur >= 2*before:
					growing = append(growing, line)
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
			byEvents := func(a, b Issue) int { return int(b.Events - a.Events) }
			slices.SortFunc(fresh, byEvents)
			slices.SortFunc(back, byEvents)
			slices.SortFunc(growing, func(a, b Issue) int { return int((b.Events - b.Previous) - (a.Events - a.Previous)) })
			o.NewCount = len(fresh)
			o.New, o.Regressed, o.Growing = first(fresh), first(back), first(growing)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !o.empty() {
			out = append(out, o)
		}
	}
	return out, nil
}

func first(list []Issue) []Issue {
	if len(list) > listLength {
		return list[:listLength]
	}
	return list
}

var months = [...]string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}

func (s *Service) day(t time.Time) string {
	t = t.In(s.loc)
	return fmt.Sprintf("%d %s", t.Day(), months[t.Month()-1])
}

func (s *Service) send(ctx context.Context, userID uuid.UUID, to string, freq apiv1.DigestFrequency, start, end time.Time) error {
	orgs, err := s.Build(ctx, userID, start, end)
	if err != nil || len(orgs) == 0 {
		return err
	}
	kind, period := "günlük", s.day(start)
	if freq == apiv1.DigestFrequency_DIGEST_FREQUENCY_WEEKLY {
		kind, period = "haftalık", s.day(start)+" – "+s.day(end.Add(-time.Hour))
	}
	fresh := 0
	for _, o := range orgs {
		fresh += o.NewCount
	}
	subject := fmt.Sprintf("İzci %s özeti, %s", kind, period)
	if fresh > 0 {
		subject = fmt.Sprintf("İzci %s özeti, %s: %d yeni hata", kind, period, fresh)
	}
	data := map[string]any{"Kind": kind, "Period": period, "Orgs": orgs, "Settings": s.publicURL + "/account?tab=notifications"}
	var text, html bytes.Buffer
	if err := textTemplate.Execute(&text, data); err != nil {
		return err
	}
	if err := htmlTemplate.Execute(&html, data); err != nil {
		return err
	}
	return s.mailer.SendMail(ctx, to, subject, strings.ReplaceAll(text.String(), "\n", "\r\n"), html.String())
}

var textTemplate = ttemplate.Must(ttemplate.New("text").Parse(`İzci {{.Kind}} özeti ({{.Period}})
{{range .Orgs}}
{{.Name}}: {{.Events}} olay
{{if .New}}Yeni hatalar ({{.NewCount}}):
{{range .New}}  {{.ShortID}} {{.Title}} ({{.Events}} olay) {{.URL}}
{{end}}{{end}}{{if .Regressed}}Geri dönen hatalar:
{{range .Regressed}}  {{.ShortID}} {{.Title}} ({{.Events}} olay) {{.URL}}
{{end}}{{end}}{{if .Growing}}En çok artan hatalar:
{{range .Growing}}  {{.ShortID}} {{.Title}} ({{.Previous}} → {{.Events}} olay) {{.URL}}
{{end}}{{end}}{{end}}
Özet e-postalarını kapatmak için: {{.Settings}}
`))

var htmlTemplate = template.Must(template.New("html").Parse(`<!doctype html>
<html lang="tr"><body style="margin:0;padding:24px;background:#f6f7f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#1f2328">
<div style="max-width:600px;margin:0 auto;background:#ffffff;border:1px solid #e3e6ea;border-radius:8px;padding:28px">
<p style="margin:0 0 4px;font-size:18px;font-weight:600">İzci {{.Kind}} özeti</p>
<p style="margin:0 0 20px;font-size:13px;color:#59636e">{{.Period}}</p>
{{range .Orgs}}<div style="margin:0 0 24px">
<p style="margin:0 0 8px;font-size:15px"><a href="{{.URL}}" style="color:#1f2328;font-weight:600;text-decoration:none">{{.Name}}</a>
<span style="color:#59636e"> · {{.Events}} olay</span></p>
{{if .New}}<p style="margin:12px 0 6px;font-size:13px;font-weight:600">Yeni hatalar ({{.NewCount}})</p>
{{range .New}}<p style="margin:0 0 6px;font-size:14px;line-height:1.4"><a href="{{.URL}}" style="color:#1d4ed8;text-decoration:none;font-family:ui-monospace,Menlo,monospace;font-size:12px">{{.ShortID}}</a> {{.Title}} <span style="color:#59636e">· {{.Events}} olay</span></p>
{{end}}{{end}}{{if .Regressed}}<p style="margin:12px 0 6px;font-size:13px;font-weight:600">Geri dönen hatalar</p>
{{range .Regressed}}<p style="margin:0 0 6px;font-size:14px;line-height:1.4"><a href="{{.URL}}" style="color:#1d4ed8;text-decoration:none;font-family:ui-monospace,Menlo,monospace;font-size:12px">{{.ShortID}}</a> {{.Title}} <span style="color:#59636e">· {{.Events}} olay</span></p>
{{end}}{{end}}{{if .Growing}}<p style="margin:12px 0 6px;font-size:13px;font-weight:600">En çok artan hatalar</p>
{{range .Growing}}<p style="margin:0 0 6px;font-size:14px;line-height:1.4"><a href="{{.URL}}" style="color:#1d4ed8;text-decoration:none;font-family:ui-monospace,Menlo,monospace;font-size:12px">{{.ShortID}}</a> {{.Title}} <span style="color:#59636e">· {{.Previous}} → {{.Events}} olay</span></p>
{{end}}{{end}}</div>
{{end}}<p style="margin:16px 0 0;font-size:12px;color:#59636e">Özet e-postalarını <a href="{{.Settings}}" style="color:#59636e">Hesabım › Bildirimler</a> bölümünden kapatabilirsiniz.</p>
</div></body></html>
`))
