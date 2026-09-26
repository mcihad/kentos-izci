package telegram

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Callback actions carried by the triage buttons.
const (
	actionResolve = "r"
	actionIgnore  = "i"
	actionReopen  = "o"
	actionAnalyze = "a"
)

const (
	// editWindow is how long repeated triggers edit the existing message
	// instead of posting a new one.
	editWindow = 12 * time.Hour
	// maxAttempts bounds delivery retries of one notification.
	maxAttempts = 8
	// linkCodeTTL is the validity of chat and account link codes.
	linkCodeTTL = 30 * time.Minute
	// pollerLock is the advisory lock that lets one instance poll updates.
	pollerLock = "izci.telegram.poller"
)

// ErrNotReady means the bot has not identified itself yet (or the token is
// wrong).
var ErrNotReady = errors.New("telegram bot is not reachable")

// Bot delivers notifications and handles incoming updates.
type Bot struct {
	client    *Client
	db        *db.DB
	box       *secret.Box
	cfg       config.TelegramConfig
	publicURL string
	log       *slog.Logger

	// OnTriage runs after an issue was triaged from Telegram (live updates).
	OnTriage func(ctx context.Context, orgID uuid.UUID, issueIDs []uuid.UUID)
	// OnRulesChanged runs after the bot created alert rules.
	OnRulesChanged func(orgID uuid.UUID)
	// Analyze starts an AI analysis for a member and returns the reply
	// shown to them; nil hides the AI button.
	Analyze func(ctx context.Context, orgID, issueID, userID uuid.UUID) string

	me   atomic.Pointer[User]
	wake chan struct{}

	mu       sync.Mutex
	limiters map[int64]*rate.Limiter
}

// New creates a bot. hc may be nil.
func New(cfg config.TelegramConfig, publicURL string, database *db.DB, box *secret.Box, log *slog.Logger, hc *http.Client) *Bot {
	return &Bot{
		client:    NewClient(cfg.APIURL, cfg.BotToken.Value(), hc),
		db:        database,
		box:       box,
		cfg:       cfg,
		publicURL: strings.TrimRight(publicURL, "/"),
		log:       log.With(slog.String("component", "telegram")),
		wake:      make(chan struct{}, 1),
		limiters:  map[int64]*rate.Limiter{},
	}
}

// Username is the bot's @username (empty until identified).
func (b *Bot) Username() string {
	if u := b.me.Load(); u != nil {
		return u.Username
	}
	return ""
}

// Mode is "polling" or "webhook".
func (b *Bot) Mode() string { return b.cfg.Mode }

// Wake starts a delivery round without waiting for the next tick.
func (b *Bot) Wake() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Identify asks Telegram who the bot is.
func (b *Bot) Identify(ctx context.Context) error {
	u, err := b.client.GetMe(ctx)
	if err != nil {
		return err
	}
	b.me.Store(u)
	return nil
}

// Run identifies the bot, then delivers notifications and processes updates
// until ctx ends.
func (b *Bot) Run(ctx context.Context) {
	for delay := time.Second; ; delay = min(delay*2, 5*time.Minute) {
		err := b.Identify(ctx)
		if err == nil {
			break
		}
		b.log.Warn("telegram bot not reachable", slog.Any("err", err), slog.Duration("retry_in", delay))
		if !sleep(ctx, delay) {
			return
		}
	}
	b.log.Info("telegram bot ready", slog.String("username", b.Username()), slog.String("mode", b.cfg.Mode))

	var wg sync.WaitGroup
	wg.Go(func() { b.dispatchLoop(ctx) })
	if b.cfg.Mode == "webhook" {
		wg.Go(func() {
			for delay := time.Second; ; delay = min(delay*2, 5*time.Minute) {
				err := b.client.SetWebhook(ctx, b.publicURL+"/telegram/webhook", b.cfg.WebhookSecret.Value())
				if err == nil {
					return
				}
				b.log.Error("telegram setWebhook failed", slog.Any("err", err))
				if !sleep(ctx, delay) {
					return
				}
			}
		})
	} else {
		wg.Go(func() { b.pollLoop(ctx) })
	}
	wg.Wait()
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ── Delivery ─────────────────────────────────────────────────────────────

func (b *Bot) dispatchLoop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		n, err := b.DispatchOnce(ctx)
		if err != nil && ctx.Err() == nil {
			b.log.Error("notification dispatch failed", slog.Any("err", err))
		}
		if n > 0 {
			continue // there may be more due
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-b.wake:
		}
	}
}

// DispatchOnce delivers due notifications and returns how many it claimed.
func (b *Bot) DispatchOnce(ctx context.Context) (int, error) {
	var items []store.OutboxItem
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		items, err = store.ClaimNotifications(ctx, tx, 20, 2*time.Minute)
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		b.deliver(ctx, it)
	}
	return len(items), nil
}

func (b *Bot) deliver(ctx context.Context, it store.OutboxItem) {
	var ch *store.Channel
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		ch, err = store.GetChannel(ctx, tx, it.ChannelID)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		b.finish(ctx, it, nil, "channel deleted")
		return
	case err != nil:
		b.retry(ctx, it, time.Now().Add(30*time.Second), err.Error())
		return
	case ch.Muted:
		b.finish(ctx, it, nil, "channel muted")
		return
	}

	if it.Kind == store.NotifyTest {
		var orgName string
		_ = b.db.InSystem(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT name FROM organizations WHERE id = $1`, ch.OrgID).Scan(&orgName)
		})
		if err := b.send(ctx, ch, RenderTest(ch.Name, orgName), nil, nil); err != nil {
			b.fail(ctx, it, ch, err)
			return
		}
		b.finish(ctx, it, ch, "")
		return
	}
	if it.IssueID == nil {
		b.finish(ctx, it, nil, "no issue")
		return
	}
	view, err := b.loadView(ctx, *it.IssueID, it.Payload)
	if errors.Is(err, store.ErrNotFound) {
		b.finish(ctx, it, nil, "issue deleted")
		return
	}
	if err != nil {
		b.retry(ctx, it, time.Now().Add(30*time.Second), err.Error())
		return
	}
	link := b.issueURL(view)
	text := RenderIssue(it.Kind, it.Payload, *view, link)
	kb := Keyboard(*view, link, b.sign, b.Analyze != nil)

	var thread *store.Thread
	_ = b.db.InSystem(ctx, func(tx pgx.Tx) error {
		thread, _ = store.GetThread(ctx, tx, ch.ID, *it.IssueID)
		return nil
	})
	// Repeated triggers update the existing message; a regression is news
	// and gets a new one.
	if thread != nil && it.Kind != store.NotifyRegression && time.Since(thread.LastSentAt) < editWindow {
		err := b.client.EditMessageText(ctx, ch.Telegram.ChatID, thread.MessageID, text, kb)
		if err == nil {
			thread.LastSentAt, thread.LastCount = time.Now(), view.TimesSeen
			thread.Kind, thread.Payload = it.Kind, it.Payload
			b.saveThread(ctx, ch.OrgID, *thread)
			b.finish(ctx, it, ch, "")
			return
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != http.StatusBadRequest || apiErr.MigrateToChatID != 0 {
			b.fail(ctx, it, ch, err)
			return
		}
		// The message is gone or too old to edit: post a new one.
	}
	var msg *Message
	if err := b.send(ctx, ch, text, kb, &msg); err != nil {
		b.fail(ctx, it, ch, err)
		return
	}
	b.saveThread(ctx, ch.OrgID, store.Thread{
		ChannelID: ch.ID, IssueID: *it.IssueID, MessageID: msg.MessageID, LastSentAt: time.Now(), LastCount: view.TimesSeen,
		Kind: it.Kind, Payload: it.Payload,
	})
	b.finish(ctx, it, ch, "")
}

// send posts to a channel, pacing messages per chat as Telegram requires.
func (b *Bot) send(ctx context.Context, ch *store.Channel, text string, kb *InlineKeyboard, out **Message) error {
	if err := b.limiter(ch.Telegram.ChatID).Wait(ctx); err != nil {
		return err
	}
	msg, err := b.client.SendMessage(ctx, OutgoingMessage{ChatID: ch.Telegram.ChatID, ThreadID: ch.Telegram.ThreadID, Text: text, Keyboard: kb})
	if err == nil && out != nil {
		*out = msg
	}
	return err
}

// limiter keeps under Telegram's limits: about one message per second per
// chat and twenty per minute per group.
func (b *Bot) limiter(chatID int64) *rate.Limiter {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.limiters[chatID]
	if !ok {
		every := time.Second
		if chatID < 0 {
			every = 3 * time.Second
		}
		l = rate.NewLimiter(rate.Every(every), 3)
		b.limiters[chatID] = l
	}
	return l
}

func (b *Bot) saveThread(ctx context.Context, orgID uuid.UUID, t store.Thread) {
	if err := b.db.InSystem(ctx, func(tx pgx.Tx) error { return store.SaveThread(ctx, tx, orgID, t) }); err != nil {
		b.log.Error("save notification thread", slog.Any("err", err))
	}
}

// finish marks a notification handled; with a channel it also records the
// delivery on the channel and the issue.
func (b *Bot) finish(ctx context.Context, it store.OutboxItem, ch *store.Channel, note string) {
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		if err := store.MarkNotificationSent(ctx, tx, it.ID); err != nil {
			return err
		}
		if ch == nil {
			if note != "" {
				_, err := tx.Exec(ctx, `UPDATE notification_outbox SET error = $2 WHERE id = $1`, it.ID, note)
				return err
			}
			return nil
		}
		if err := store.RecordDelivery(ctx, tx, ch.ID, ""); err != nil {
			return err
		}
		if it.IssueID != nil {
			return store.AddIssueActivity(ctx, tx, it.OrgID, []uuid.UUID{*it.IssueID}, store.ActivityNotified, nil,
				map[string]string{"channel": ch.Name})
		}
		return nil
	})
	if err != nil {
		b.log.Error("record notification delivery", slog.Any("err", err))
	}
}

func (b *Bot) retry(ctx context.Context, it store.OutboxItem, at time.Time, msg string) {
	if err := b.db.InSystem(ctx, func(tx pgx.Tx) error { return store.MarkNotificationFailed(ctx, tx, it.ID, &at, msg) }); err != nil {
		b.log.Error("reschedule notification", slog.Any("err", err))
	}
}

// fail handles a delivery error: flood control and chat migrations are
// retried, permanent errors give up and show on the channel, anything else
// backs off exponentially.
func (b *Bot) fail(ctx context.Context, it store.OutboxItem, ch *store.Channel, err error) {
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.MigrateToChatID != 0:
		_ = b.db.InSystem(ctx, func(tx pgx.Tx) error {
			return store.MigrateTelegramChat(ctx, tx, ch.Telegram.ChatID, apiErr.MigrateToChatID)
		})
		b.retry(ctx, it, time.Now(), "chat migrated to a supergroup")
	case errors.As(err, &apiErr) && apiErr.RetryAfter > 0:
		b.retry(ctx, it, time.Now().Add(apiErr.RetryAfter), apiErr.Description)
	case errors.As(err, &apiErr) && apiErr.Permanent():
		b.giveUp(ctx, it, ch, apiErr.Description)
	case int(it.Attempts)+1 >= maxAttempts:
		b.giveUp(ctx, it, ch, err.Error())
	default:
		backoff := min(10*time.Second<<it.Attempts, time.Hour)
		b.retry(ctx, it, time.Now().Add(backoff), err.Error())
	}
}

func (b *Bot) giveUp(ctx context.Context, it store.OutboxItem, ch *store.Channel, msg string) {
	b.log.Warn("notification dropped", slog.String("channel", ch.Name), slog.String("reason", msg))
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		if err := store.MarkNotificationFailed(ctx, tx, it.ID, nil, msg); err != nil {
			return err
		}
		return store.RecordDelivery(ctx, tx, ch.ID, msg)
	})
	if err != nil {
		b.log.Error("record failed notification", slog.Any("err", err))
	}
}

// loadView reads an issue and the event that triggered the notification.
func (b *Bot) loadView(ctx context.Context, issueID uuid.UUID, payload map[string]any) (*IssueView, error) {
	v := &IssueView{ID: issueID.String()}
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		var (
			seq         int32
			projectSlug string
			meta        []byte
			resolvedBy  *string
		)
		err := tx.QueryRow(ctx, `
			SELECT coalesce(i.short_seq, 0), p.slug, p.name, o.slug, i.title, i.type, i.value, i.culprit, i.metadata, i.level,
			       i.status, i.unhandled, i.times_seen, i.users_count, i.customers_count, nullif(coalesce(u.name, u.email), '')
			FROM issues i
			JOIN projects p ON p.id = i.project_id
			JOIN organizations o ON o.id = i.org_id
			LEFT JOIN users u ON u.id = i.resolved_by
			WHERE i.id = $1`, issueID).Scan(&seq, &projectSlug, &v.ProjectName, &v.OrgSlug, &v.Title, &v.Type, &v.Value, &v.Culprit,
			&meta, &v.Level, &v.Status, &v.Unhandled, &v.TimesSeen, &v.Users, &v.Customers, &resolvedBy)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		if err != nil {
			return err
		}
		v.ShortID = ingest.ShortID(projectSlug, seq)
		if v.Status == store.IssueResolved && resolvedBy != nil {
			v.StatusBy = *resolvedBy
		}
		var loc ingest.Location
		_ = json.Unmarshal(meta, &loc)
		v.Filename, v.Lineno = loc.Filename, loc.Lineno

		raw, _ := payload["event_id"].(string)
		eventID, err := uuid.Parse(raw)
		if err != nil {
			return nil
		}
		at := ingest.ReceivedAt(eventID)
		err = tx.QueryRow(ctx, `
			SELECT e.environment, e.release, CASE WHEN e.customer_key = '' THEN '' ELSE coalesce(nullif(c.name, ''), e.customer_key) END,
			       e.tenant_key, e.user_label
			FROM events e
			LEFT JOIN customers c ON e.customer_key <> '' AND c.org_id = e.org_id AND c.key = e.customer_key
			WHERE e.id = $1 AND e.received_at BETWEEN $2 AND $3`, eventID, at.Add(-time.Second), at.Add(time.Second)).Scan(
			&v.Environment, &v.Release, &v.Customer, &v.Tenant, &v.User)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return v, err
}

func (b *Bot) issueURL(v *IssueView) string {
	return b.publicURL + "/o/" + v.OrgSlug + "/issues/" + v.ShortID
}

// sign builds the callback data of a triage button: action, issue id and a
// truncated HMAC, well under Telegram's 64-byte limit.
func (b *Bot) sign(action, issueID string) string {
	id, err := uuid.Parse(issueID)
	if err != nil {
		return ""
	}
	enc := base64.RawURLEncoding.EncodeToString(id[:])
	return action + ":" + enc + ":" + b.box.Sign("tg:"+action+":"+enc, 8)
}

func (b *Bot) verify(data string) (string, uuid.UUID, bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || !b.box.Verify("tg:"+parts[0]+":"+parts[1], parts[2], 8) {
		return "", uuid.Nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(raw) != 16 {
		return "", uuid.Nil, false
	}
	return parts[0], uuid.UUID(raw), true
}

// ── Link codes ───────────────────────────────────────────────────────────

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	for i := range buf {
		buf[i] = codeAlphabet[int(buf[i])%len(codeAlphabet)]
	}
	return string(buf)
}

// ChatLinkCode creates a code that links a chat to an organization (and
// optionally a project) when sent to the bot.
func (b *Bot) ChatLinkCode(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, userID uuid.UUID) (*apiv1.CreateTelegramLinkCodeResponse, error) {
	username := b.Username()
	if username == "" {
		return nil, ErrNotReady
	}
	c := store.LinkCode{Code: newCode(), Kind: store.LinkTelegramChat, OrgID: &orgID, ProjectID: projectID, UserID: &userID,
		ExpiresAt: time.Now().Add(linkCodeTTL)}
	if err := store.CreateLinkCode(ctx, b.db.Pool, c); err != nil {
		return nil, err
	}
	return &apiv1.CreateTelegramLinkCodeResponse{
		Code: c.Code, BotUsername: username,
		GroupLink:   "https://t.me/" + username + "?startgroup=" + c.Code,
		PrivateLink: "https://t.me/" + username + "?start=" + c.Code,
		ExpiresAt:   timestamppb.New(c.ExpiresAt),
	}, nil
}

// AccountLinkCode creates a code that links the sender's Telegram account to
// an izci user.
func (b *Bot) AccountLinkCode(ctx context.Context, userID uuid.UUID) (*apiv1.CreateTelegramAccountLinkCodeResponse, error) {
	username := b.Username()
	if username == "" {
		return nil, ErrNotReady
	}
	c := store.LinkCode{Code: newCode(), Kind: store.LinkTelegramAccount, UserID: &userID, ExpiresAt: time.Now().Add(linkCodeTTL)}
	if err := store.CreateLinkCode(ctx, b.db.Pool, c); err != nil {
		return nil, err
	}
	return &apiv1.CreateTelegramAccountLinkCodeResponse{
		Code: c.Code, BotUsername: username, Link: "https://t.me/" + username + "?start=" + c.Code,
		ExpiresAt: timestamppb.New(c.ExpiresAt),
	}, nil
}

// ── Updates ──────────────────────────────────────────────────────────────

// HandleUpdate processes one update from polling or the webhook.
func (b *Bot) HandleUpdate(ctx context.Context, u Update) {
	switch {
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	}
}

const helpText = `<b>İzci bildirim botu</b>

<code>/baglan KOD</code> bu sohbeti bir İzci organizasyonunun bildirimlerine bağlar. Kodu İzci'de Uyarılar sayfasından alın.
<code>/ben KOD</code> Telegram hesabınızı İzci hesabınıza bağlar; böylece bildirimlerdeki düğmeleri kullanabilirsiniz. Kodu Hesabım › Telegram bölümünden alın.`

func (b *Bot) handleMessage(ctx context.Context, m *Message) {
	if m.MigrateToChatID != 0 {
		if err := b.db.InSystem(ctx, func(tx pgx.Tx) error { return store.MigrateTelegramChat(ctx, tx, m.Chat.ID, m.MigrateToChatID) }); err != nil {
			b.log.Error("migrate telegram chat", slog.Any("err", err))
		}
		return
	}
	text := strings.TrimSpace(m.Text)
	if !strings.HasPrefix(text, "/") {
		return
	}
	cmd, arg, _ := strings.Cut(text, " ")
	if name, target, ok := strings.Cut(cmd, "@"); ok {
		if !strings.EqualFold(target, b.Username()) {
			return // a command for another bot in the group
		}
		cmd = name
	}
	switch strings.ToLower(cmd) {
	case "/start", "/baglan", "/bağlan", "/ben":
		if strings.TrimSpace(arg) == "" {
			b.reply(ctx, m, helpText)
			return
		}
		b.redeem(ctx, m, arg)
	case "/yardim", "/yardım", "/help":
		b.reply(ctx, m, helpText)
	}
}

func (b *Bot) reply(ctx context.Context, m *Message, text string) {
	out := OutgoingMessage{ChatID: m.Chat.ID, Text: text}
	if m.IsTopicMessage {
		out.ThreadID = m.MessageThreadID
	}
	if _, err := b.client.SendMessage(ctx, out); err != nil {
		b.log.Warn("telegram reply failed", slog.Any("err", err))
	}
}

func normalizeCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return strings.NewReplacer("-", "", " ", "").Replace(s)
}

func (b *Bot) redeem(ctx context.Context, m *Message, arg string) {
	code, err := store.RedeemLinkCode(ctx, b.db.Pool, normalizeCode(arg))
	if errors.Is(err, store.ErrNotFound) {
		b.reply(ctx, m, "Kod geçersiz ya da süresi dolmuş. İzci'den yeni bir kod alıp tekrar gönderin.")
		return
	}
	if err != nil {
		b.log.Error("redeem link code", slog.Any("err", err))
		b.reply(ctx, m, "Kod şu anda doğrulanamadı. Biraz sonra yeniden deneyin.")
		return
	}
	switch code.Kind {
	case store.LinkTelegramChat:
		b.linkChat(ctx, m, code)
	case store.LinkTelegramAccount:
		b.linkAccount(ctx, m, code)
	}
}

func (b *Bot) linkChat(ctx context.Context, m *Message, code *store.LinkCode) {
	if code.OrgID == nil {
		return
	}
	target := store.TelegramTarget{ChatID: m.Chat.ID, Title: m.Chat.Name(), Type: m.Chat.Type, Username: m.Chat.Username}
	if m.IsTopicMessage {
		target.ThreadID = m.MessageThreadID
	}
	var orgName, projectName string
	var created bool
	err := b.db.InOrg(ctx, *code.OrgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT name FROM organizations WHERE id = $1`, *code.OrgID).Scan(&orgName); err != nil {
			return err
		}
		if code.ProjectID != nil {
			if err := tx.QueryRow(ctx, `SELECT name FROM projects WHERE id = $1`, *code.ProjectID).Scan(&projectName); err != nil {
				return err
			}
		}
		ch, isNew, err := store.UpsertTelegramChannel(ctx, tx, *code.OrgID, code.ProjectID, code.UserID, target.Title, target)
		if err != nil {
			return err
		}
		created = isNew
		return ensureDefaultRules(ctx, tx, *code.OrgID, ch.ID)
	})
	if err != nil {
		b.log.Error("link telegram chat", slog.Any("err", err))
		b.reply(ctx, m, "Sohbet bağlanamadı. Biraz sonra yeniden deneyin.")
		return
	}
	if b.OnRulesChanged != nil {
		b.OnRulesChanged(*code.OrgID)
	}
	scope := "tüm projelerinin"
	if projectName != "" {
		scope = "<b>" + html.EscapeString(projectName) + "</b> projesinin"
	}
	verb := "bağlandı"
	if !created {
		verb = "yeniden bağlandı"
	}
	b.reply(ctx, m, fmt.Sprintf("✅ Bu sohbet <b>%s</b> organizasyonunun %s bildirimlerine %s.\n\nHangi hataların bildirileceğini İzci'de Uyarılar sayfasından ayarlayabilirsiniz.",
		html.EscapeString(orgName), scope, verb))
}

// ensureDefaultRules gives an organization's first channel two sensible
// rules: new and regressed errors in production.
func ensureDefaultRules(ctx context.Context, tx pgx.Tx, orgID, channelID uuid.UUID) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alert_rules WHERE org_id = $1`, orgID).Scan(&n); err != nil || n > 0 {
		return err
	}
	filters := store.RuleFilters{MinLevel: 4, Environments: []string{"production"}}
	for _, r := range []store.AlertRule{
		{Name: "Üretimde yeni hatalar", Trigger: store.RuleTrigger{Kind: 1}},
		{Name: "Üretimde geri dönen hatalar", Trigger: store.RuleTrigger{Kind: 2}},
	} {
		r.OrgID, r.Enabled, r.Filters, r.ChannelIDs, r.ThrottleSeconds = orgID, true, filters, []uuid.UUID{channelID}, 1800
		if _, err := store.SaveAlertRule(ctx, tx, &r); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) linkAccount(ctx context.Context, m *Message, code *store.LinkCode) {
	if code.UserID == nil || m.From == nil {
		return
	}
	var email string
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		// A Telegram account belongs to one izci user at a time.
		if _, err := tx.Exec(ctx, `UPDATE users SET telegram_user_id = NULL, telegram_username = '', telegram_linked_at = NULL
			WHERE telegram_user_id = $1 AND id <> $2`, m.From.ID, *code.UserID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE users SET telegram_user_id = $2, telegram_username = $3, telegram_linked_at = now()
			WHERE id = $1 RETURNING email`, *code.UserID, m.From.ID, m.From.Username).Scan(&email)
	})
	if err != nil {
		b.log.Error("link telegram account", slog.Any("err", err))
		b.reply(ctx, m, "Hesap bağlanamadı. Biraz sonra yeniden deneyin.")
		return
	}
	b.reply(ctx, m, fmt.Sprintf("✅ Telegram hesabınız <b>%s</b> İzci hesabına bağlandı. Bildirimlerdeki düğmeleri artık kullanabilirsiniz.",
		html.EscapeString(email)))
}

func (b *Bot) answer(ctx context.Context, cq *CallbackQuery, text string, alert bool) {
	if err := b.client.AnswerCallbackQuery(ctx, cq.ID, text, alert); err != nil {
		b.log.Warn("answer callback failed", slog.Any("err", err))
	}
}

func (b *Bot) handleCallback(ctx context.Context, cq *CallbackQuery) {
	action, issueID, ok := b.verify(cq.Data)
	if !ok {
		b.answer(ctx, cq, "Bu düğme artık geçerli değil.", false)
		return
	}
	var (
		userID    uuid.UUID
		userName  string
		orgID     uuid.UUID
		role      int16
		errNoUser = errors.New("no linked user")
	)
	err := b.db.InSystem(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT id, coalesce(nullif(name, ''), email) FROM users WHERE telegram_user_id = $1 AND NOT disabled`,
			cq.From.ID).Scan(&userID, &userName)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNoUser
		}
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT org_id FROM issues WHERE id = $1`, issueID).Scan(&orgID); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	switch {
	case errors.Is(err, errNoUser):
		b.answer(ctx, cq, "Önce Telegram hesabınızı İzci'ye bağlayın: İzci'de Hesabım › Telegram bölümünden bir kod alıp bana /ben KOD yazın.", true)
		return
	case errors.Is(err, pgx.ErrNoRows):
		b.answer(ctx, cq, "Bu hata artık yok.", false)
		return
	case err != nil:
		b.log.Error("telegram callback", slog.Any("err", err))
		b.answer(ctx, cq, "İşlem şu anda yapılamadı. Biraz sonra yeniden deneyin.", false)
		return
	case role < int16(apiv1.Role_ROLE_MEMBER):
		b.answer(ctx, cq, "Bu işlem için yetkiniz yok.", true)
		return
	}

	if action == actionAnalyze {
		if b.Analyze == nil {
			b.answer(ctx, cq, "Yapay zekâ analizi bu sunucuda kapalı.", true)
			return
		}
		b.answer(ctx, cq, b.Analyze(ctx, orgID, issueID, userID), false)
		return
	}

	ids := []uuid.UUID{issueID}
	var status int16
	var done string
	err = b.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var err error
		switch action {
		case actionResolve:
			status, done = store.IssueResolved, "Çözüldü olarak işaretlendi"
			err = store.ResolveIssues(ctx, tx, orgID, ids, userID, "", false)
		case actionIgnore:
			status, done = store.IssueIgnored, "Yoksayıldı"
			err = store.IgnoreIssues(ctx, tx, orgID, ids, store.IgnoreOptions{})
		case actionReopen:
			status, done = store.IssueUnresolved, "Yeniden açıldı"
			err = store.ReopenIssues(ctx, tx, orgID, ids)
		default:
			return errors.New("unknown action")
		}
		if err != nil {
			return err
		}
		return store.AddIssueActivity(ctx, tx, orgID, ids, store.ActivityStatusChanged, &userID,
			map[string]string{"to": store.StatusName(status), "via": "telegram"})
	})
	if err != nil {
		b.log.Error("telegram triage", slog.Any("err", err))
		b.answer(ctx, cq, "İşlem şu anda yapılamadı. Biraz sonra yeniden deneyin.", false)
		return
	}
	b.answer(ctx, cq, done, false)
	if b.OnTriage != nil {
		b.OnTriage(ctx, orgID, ids)
	}
	if cq.Message != nil {
		b.refreshMessage(ctx, cq.Message, issueID, userName)
	}
}

// refreshMessage re-renders a notification after its issue changed.
func (b *Bot) refreshMessage(ctx context.Context, m *Message, issueID uuid.UUID, actor string) {
	kind, payload := store.NotifyNewIssue, map[string]any{}
	_ = b.db.InSystem(ctx, func(tx pgx.Tx) error {
		threads, err := store.ThreadsOfMessage(ctx, tx, m.Chat.ID, m.MessageID)
		if err == nil && len(threads) > 0 {
			kind, payload = threads[0].Kind, threads[0].Payload
		}
		return nil
	})
	view, err := b.loadView(ctx, issueID, payload)
	if err != nil {
		return
	}
	if view.Status != store.IssueUnresolved {
		view.StatusBy = actor
	}
	link := b.issueURL(view)
	if err := b.client.EditMessageText(ctx, m.Chat.ID, m.MessageID, RenderIssue(kind, payload, *view, link), Keyboard(*view, link, b.sign, b.Analyze != nil)); err != nil {
		b.log.Warn("refresh telegram message", slog.Any("err", err))
	}
}

// ── Polling and webhook ──────────────────────────────────────────────────

func (b *Bot) pollLoop(ctx context.Context) {
	if err := b.client.DeleteWebhook(ctx); err != nil && ctx.Err() == nil {
		b.log.Warn("telegram deleteWebhook failed", slog.Any("err", err))
	}
	for ctx.Err() == nil {
		conn, err := b.db.Pool.Acquire(ctx)
		if err != nil {
			sleep(ctx, 5*time.Second)
			continue
		}
		var locked bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, pollerLock).Scan(&locked); err != nil || !locked {
			conn.Release()
			// Another instance polls; check again later in case it stops.
			sleep(ctx, 30*time.Second)
			continue
		}
		b.poll(ctx)
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, pollerLock)
		conn.Release()
	}
}

func (b *Bot) poll(ctx context.Context) {
	offset := b.loadOffset(ctx)
	backoff := time.Second
	for ctx.Err() == nil {
		updates, err := b.client.GetUpdates(ctx, offset, 25*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
				b.log.Warn("telegram getUpdates conflict: a webhook or another poller is active")
				sleep(ctx, 30*time.Second)
				continue
			}
			b.log.Warn("telegram getUpdates failed", slog.Any("err", err))
			sleep(ctx, backoff)
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		for _, u := range updates {
			b.HandleUpdate(ctx, u)
			offset = u.UpdateID + 1
		}
		if len(updates) > 0 {
			b.saveOffset(ctx, offset)
		}
	}
}

const offsetKey = "telegram.update_offset"

func (b *Bot) loadOffset(ctx context.Context) int64 {
	var raw []byte
	if err := b.db.Pool.QueryRow(ctx, `SELECT value FROM instance_settings WHERE key = $1`, offsetKey).Scan(&raw); err != nil {
		return 0
	}
	var n int64
	_ = json.Unmarshal(raw, &n)
	return n
}

func (b *Bot) saveOffset(ctx context.Context, offset int64) {
	js, _ := json.Marshal(offset)
	if _, err := b.db.Pool.Exec(ctx, `
		INSERT INTO instance_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = now()`, offsetKey, string(js)); err != nil {
		b.log.Warn("save telegram offset", slog.Any("err", err))
	}
}

// WebhookHandler receives updates in webhook mode. Telegram authenticates
// with the secret token given to setWebhook.
func (b *Bot) WebhookHandler() http.Handler {
	want := []byte(b.cfg.WebhookSecret.Value())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"))
		if len(want) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var u Update
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&u); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		b.HandleUpdate(r.Context(), u)
		w.WriteHeader(http.StatusOK)
	})
}
