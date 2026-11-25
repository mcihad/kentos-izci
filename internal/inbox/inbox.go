// Package inbox delivers personal notifications: issues assigned to a
// member, mentions in comments and news about the issues they follow.
// Every notification lands in the member's inbox in the web app; the
// delivery worker also sends it by e-mail and Telegram, as the member
// chose (izci.api.v1.NotificationSettings).
package inbox

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

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

// Telegram sends messages from the bot to a member's private chat
// (telegram.Bot).
type Telegram interface {
	SendDirect(ctx context.Context, userID int64, html string) error
}

// maxAttempts bounds the delivery attempts of a notification.
const maxAttempts = 5

// Service delivers personal notifications by e-mail and Telegram.
type Service struct {
	db        *db.DB
	log       *slog.Logger
	mailer    Mailer
	telegram  Telegram
	publicURL string
	wake      chan struct{}
}

// New creates the service. mailer and telegram may be nil when the server
// has no SMTP or bot.
func New(database *db.DB, log *slog.Logger, mailer Mailer, telegram Telegram, publicURL string) *Service {
	return &Service{db: database, log: log, mailer: mailer, telegram: telegram, publicURL: strings.TrimRight(publicURL, "/"),
		wake: make(chan struct{}, 1)}
}

// EmailAvailable reports whether notifications can go by e-mail.
func (s *Service) EmailAvailable() bool { return s != nil && s.mailer != nil }

// TelegramAvailable reports whether notifications can go by Telegram.
func (s *Service) TelegramAvailable() bool { return s != nil && s.telegram != nil }

// Wake asks for a delivery pass soon (after notifications were stored).
func (s *Service) Wake() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run delivers notifications until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		if _, err := s.DeliverOnce(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("deliver personal notifications", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		case <-s.wake:
		}
	}
}

// DeliverOnce sends the notifications that are due, grouped into one
// message per member and channel, and reports how many it handled.
func (s *Service) DeliverOnce(ctx context.Context) (int, error) {
	var entries []*store.InboxEntry
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		entries, err = store.ClaimUndelivered(ctx, tx, 200)
		return err
	})
	if err != nil || len(entries) == 0 {
		return 0, err
	}
	byUser := map[uuid.UUID][]*store.InboxEntry{}
	var users []uuid.UUID
	for _, e := range entries {
		if _, ok := byUser[e.UserID]; !ok {
			users = append(users, e.UserID)
		}
		byUser[e.UserID] = append(byUser[e.UserID], e)
	}
	var recipients map[uuid.UUID]*store.Recipient
	if err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		recipients, err = store.Recipients(ctx, tx, users)
		return err
	}); err != nil {
		return 0, err
	}
	var done []uuid.UUID
	for _, u := range users {
		list := byUser[u]
		if s.send(ctx, recipients[u], list) || list[0].Attempts >= maxAttempts {
			for _, e := range list {
				done = append(done, e.ID)
			}
		}
	}
	if len(done) > 0 {
		err = s.db.InSystem(ctx, func(tx pgx.Tx) error { return store.MarkDelivered(ctx, tx, done) })
	}
	return len(entries), err
}

// send delivers a member's notifications on the channels they chose and
// reports whether they are done with: sent on at least one channel, or
// meant for none. A channel that fails while another succeeds is not
// retried, so nobody gets the same e-mail twice.
func (s *Service) send(ctx context.Context, r *store.Recipient, list []*store.InboxEntry) bool {
	if r == nil {
		return true // disabled or deleted account
	}
	var mail, tg []*store.InboxEntry
	for _, e := range list {
		ch := channels(r.Settings, e.Kind)
		if ch.GetEmail() && s.mailer != nil && r.Email != "" {
			mail = append(mail, e)
		}
		if ch.GetTelegram() && s.telegram != nil && r.TelegramUserID != nil {
			tg = append(tg, e)
		}
	}
	if len(mail) == 0 && len(tg) == 0 {
		return true
	}
	sent := false
	if len(mail) > 0 {
		m := s.render(mail)
		if err := s.mailer.SendMail(ctx, r.Email, m.subject, m.text, m.html); err != nil {
			s.log.Warn("personal notification e-mail failed", slog.String("user", r.ID.String()), slog.Any("err", err))
		} else {
			sent = true
		}
	}
	if len(tg) > 0 {
		if err := s.telegram.SendDirect(ctx, *r.TelegramUserID, s.renderTelegram(tg)); err != nil {
			s.log.Warn("personal notification Telegram message failed", slog.String("user", r.ID.String()), slog.Any("err", err))
		} else {
			sent = true
		}
	}
	return sent
}

func channels(s *apiv1.NotificationSettings, kind int16) *apiv1.NotificationChannels {
	switch kind {
	case store.NotifyAssigned:
		return s.GetAssigned()
	case store.NotifyMentioned:
		return s.GetMentioned()
	case store.NotifyRegressed:
		return s.GetRegressed()
	case store.NotifyCommented:
		return s.GetCommented()
	}
	return nil
}

// Regressions notifies the followers of the issues of a batch that came
// back after they were resolved. It runs in the batch transaction.
func Regressions(ctx context.Context, tx pgx.Tx, b *ingest.Batch) (int, error) {
	org := map[uuid.UUID]uuid.UUID{}
	var ids []uuid.UUID
	for _, o := range b.Issues {
		if o.Regressed {
			ids = append(ids, o.ID)
			org[o.ID] = o.OrgID
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	followers, err := store.IssueFollowers(ctx, tx, ids)
	if err != nil {
		return 0, err
	}
	var ns []store.PersonalNotification
	for issue, users := range followers {
		for _, u := range users {
			ns = append(ns, store.PersonalNotification{OrgID: org[issue], UserID: u, IssueID: issue, Kind: store.NotifyRegressed})
		}
	}
	return len(ns), store.AddPersonalNotifications(ctx, tx, ns)
}

// Member is a member who can be named in a comment.
type Member struct {
	ID    uuid.UUID
	Email string
}

// mentionRe finds "@name" and "@name@example.com" that do not belong to an
// e-mail address written in the text.
var mentionRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}._%+@-])@([\p{L}\p{N}._%+-]+(?:@[\p{L}\p{N}.-]+\.\p{L}{2,})?)`)

// Mentions returns the members a comment names: "@" with an e-mail address,
// or with the part of an address before "@" when only one member has it.
func Mentions(text string, members []Member) []uuid.UUID {
	byEmail := map[string]uuid.UUID{}
	byLocal := map[string][]uuid.UUID{}
	for _, m := range members {
		email := strings.ToLower(m.Email)
		byEmail[email] = m.ID
		if local, _, ok := strings.Cut(email, "@"); ok {
			byLocal[local] = append(byLocal[local], m.ID)
		}
	}
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	add := func(id uuid.UUID) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(strings.TrimRight(m[1], "."))
		if strings.Contains(name, "@") {
			if id, ok := byEmail[name]; ok {
				add(id)
			}
			continue
		}
		if ids := byLocal[name]; len(ids) == 1 {
			add(ids[0])
		}
	}
	return out
}

// Excerpt shortens a comment for notifications.
func Excerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= 280 {
		return text
	}
	r := []rune(text)
	return string(r[:279]) + "…"
}
