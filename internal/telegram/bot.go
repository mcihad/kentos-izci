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
	"github.com/mcihad/kentos-izci/internal/notify"
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

	me atomic.Pointer[User]

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

// Identify asks Telegram who the bot is.
func (b *Bot) Identify(ctx context.Context) error {
	u, err := b.client.GetMe(ctx)
	if err != nil {
		return err
	}
	b.me.Store(u)
	return nil
}

// Run identifies the bot, then processes updates
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

// Send delivers a notification to a Telegram chat (notify.Sender).
// Repeated triggers of an issue edit the message posted before; a
// regression is news and gets a new message.
func (b *Bot) Send(ctx context.Context, n *notify.Notification) (*notify.Receipt, error) {
	if b.Username() == "" {
		return nil, &notify.RetryError{After: 30 * time.Second, Reason: ErrNotReady.Error()}
	}
	ch := n.Channel
	if n.Test() {
		return nil, b.classify(ctx, ch, b.send(ctx, ch, RenderTest(ch.Name, n.OrgName), nil, nil))
	}
	view := *n.Issue
	text := RenderIssue(n.Item.Kind, n.Item.Payload, view, n.Link)
	kb := Keyboard(view, n.Link, b.sign, b.Analyze != nil)
	if t := n.Thread; t != nil && n.Item.Kind != store.NotifyRegression && time.Since(t.LastSentAt) < editWindow {
		err := b.client.EditMessageText(ctx, ch.Telegram.ChatID, t.MessageID, text, kb)
		if err == nil {
			t.LastSentAt, t.LastCount, t.Kind, t.Payload = time.Now(), view.TimesSeen, n.Item.Kind, n.Item.Payload
			return &notify.Receipt{Thread: t}, nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != http.StatusBadRequest || apiErr.MigrateToChatID != 0 {
			return nil, b.classify(ctx, ch, err)
		}
		// The message is gone or too old to edit: post a new one.
	}
	var msg *Message
	if err := b.send(ctx, ch, text, kb, &msg); err != nil {
		return nil, b.classify(ctx, ch, err)
	}
	return &notify.Receipt{Thread: &store.Thread{
		ChannelID: ch.ID, IssueID: *n.Item.IssueID, MessageID: msg.MessageID, LastSentAt: time.Now(), LastCount: view.TimesSeen,
		Kind: n.Item.Kind, Payload: n.Item.Payload,
	}}, nil
}

// classify turns a Bot API error into the dispatcher's terms: chat
// migrations are followed and retried at once, flood control waits as
// told, permanent errors give up.
func (b *Bot) classify(ctx context.Context, ch *store.Channel, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.MigrateToChatID != 0:
		_ = b.db.InSystem(ctx, func(tx pgx.Tx) error {
			return store.MigrateTelegramChat(ctx, tx, ch.Telegram.ChatID, apiErr.MigrateToChatID)
		})
		return &notify.RetryError{Reason: "chat migrated to a supergroup"}
	case errors.As(err, &apiErr) && apiErr.RetryAfter > 0:
		return &notify.RetryError{After: apiErr.RetryAfter, Reason: apiErr.Description}
	case errors.As(err, &apiErr) && apiErr.Permanent():
		return &notify.PermanentError{Reason: apiErr.Description}
	}
	return err
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

// loadView reads an issue and the event that triggered the notification.
func (b *Bot) loadView(ctx context.Context, issueID uuid.UUID, payload map[string]any) (*IssueView, error) {
	return notify.LoadIssueView(ctx, b.db, issueID, payload)
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
		return store.EnsureDefaultRules(ctx, tx, *code.OrgID, ch.ID)
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
		needs2FA  bool
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
		err = tx.QueryRow(ctx, `
			SELECT m.role, coalesce((o.settings->>'require_two_factor')::boolean, false) AND u.totp_enabled_at IS NULL
			FROM org_members m JOIN organizations o ON o.id = m.org_id JOIN users u ON u.id = m.user_id
			WHERE m.org_id = $1 AND m.user_id = $2`, orgID, userID).Scan(&role, &needs2FA)
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
	case needs2FA:
		b.answer(ctx, cq, "Bu organizasyon iki adımlı doğrulama istiyor. Önce İzci'de Hesabım › İki adımlı doğrulama bölümünden açın.", true)
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
