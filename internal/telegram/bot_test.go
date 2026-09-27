package telegram_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/alert"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/telegram"
	"github.com/mcihad/kentos-izci/internal/testutil"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// fakeAPI is a minimal Telegram Bot API that records calls.
type fakeAPI struct {
	mu     sync.Mutex
	calls  []call
	nextID int64
	fail   map[string]string // method → raw error response for the next call
}

type call struct {
	method string
	params map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := path.Base(r.URL.Path)
	var params map[string]any
	_ = json.NewDecoder(r.Body).Decode(&params)
	f.mu.Lock()
	f.calls = append(f.calls, call{method, params})
	failure, failing := f.fail[method]
	delete(f.fail, method)
	f.nextID++
	id := f.nextID
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if failing {
		_, _ = w.Write([]byte(failure))
		return
	}
	switch method {
	case "getMe":
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"İzci","username":"izci_test_bot"}}`))
	case "sendMessage":
		res, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{
			"message_id": id, "chat": map[string]any{"id": params["chat_id"], "type": "supergroup"}, "text": params["text"],
		}})
		_, _ = w.Write(res)
	case "getUpdates":
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}
}

// take returns and clears the recorded calls of a method.
func (f *fakeAPI) take(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out, rest []call
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		} else {
			rest = append(rest, c)
		}
	}
	f.calls = rest
	return out
}

func (f *fakeAPI) failNext(method, response string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = map[string]string{}
	}
	f.fail[method] = response
}

type fixture struct {
	db       *db.DB
	api      *fakeAPI
	bot      *telegram.Bot
	dispatch *notify.Dispatcher
	org      *store.Organization
	project  *store.Project
	user     *store.User
	pipeline *ingest.Pipeline
	triaged  chan uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	f := &fixture{db: testutil.NewDB(t), api: &fakeAPI{}, triaged: make(chan uuid.UUID, 8)}
	if _, err := f.db.EnsureEventPartitions(ctx, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if f.org, err = store.CreateOrganization(ctx, tx, "kentos", "KentOS"); err != nil {
			return err
		}
		if f.project, err = store.CreateProject(ctx, tx, f.org.ID, "kalem", "KentOS Kalem", "dotnet"); err != nil {
			return err
		}
		if f.user, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@example.com", Name: "Ayşe Yılmaz"}); err != nil {
			return err
		}
		return store.AddMember(ctx, tx, f.org.ID, f.user.ID, apiv1.Role_ROLE_MEMBER)
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f.api)
	t.Cleanup(srv.Close)
	box, _ := secret.NewBox(make([]byte, 32))
	log := slog.New(slog.DiscardHandler)
	f.bot = telegram.New(config.TelegramConfig{BotToken: "42:test", Mode: "polling", APIURL: srv.URL, WebhookSecret: "s3cret-s3cret-s3cret"},
		"https://izci.example.com", f.db, box, log, srv.Client())
	f.bot.OnTriage = func(_ context.Context, _ uuid.UUID, ids []uuid.UUID) { f.triaged <- ids[0] }
	f.dispatch = notify.NewDispatcher(f.db, log, "https://izci.example.com")
	f.dispatch.Register(store.ChannelTelegram, f.bot)
	if err := f.bot.Identify(ctx); err != nil {
		t.Fatal(err)
	}

	engine := alert.NewEngine(time.Minute)
	f.bot.OnRulesChanged = engine.Invalidate
	f.pipeline = ingest.NewPipeline(f.db, log, ingest.PipelineConfig{Shards: 1, BatchSize: 50, PublicURL: "https://izci.example.com"},
		ingest.Hooks{InTx: func(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error {
			n, err := engine.Evaluate(ctx, tx, b)
			b.Notifications += n
			return err
		}})
	f.pipeline.Start()
	t.Cleanup(func() { _ = f.pipeline.Close(context.Background()) })
	return f
}

// ingestError stores one error event of the fixture's project.
func (f *fixture) ingestError(t *testing.T, env string) ingest.Result {
	t.Helper()
	ev := &ingestv1.Event{
		Level: ingestv1.Level_LEVEL_ERROR, Environment: env, Release: "1.4.2",
		Customer: &ingestv1.Customer{Id: "sivas-belediyesi", Name: "Sivas Belediyesi"},
		Exceptions: []*ingestv1.Exception{{Type: "System.TimeoutException", Value: "SMS sağlayıcısı 30 sn içinde yanıt vermedi",
			Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{{Function: "SmsSender.SendAsync", Filename: "src/Sms/SmsSender.cs", Lineno: 57, InApp: ptr(true)}}}}},
	}
	p, err := ingest.Normalize(ev, ingest.NormalizeOptions{Project: ingest.ProjectContext{
		OrgID: f.org.ID, OrgSlug: f.org.Slug, ProjectID: f.project.ID, ProjectSlug: f.project.Slug, Settings: f.project.Settings,
	}, Retention: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	chans, err := f.pipeline.Submit(p)
	if err != nil {
		t.Fatal(err)
	}
	r := <-chans[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return r
}

func ptr[T any](v T) *T { return &v }

func (f *fixture) scalar(t *testing.T, dest any, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error { return tx.QueryRow(ctx, sql, args...).Scan(dest) }); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func text(c call) string { s, _ := c.params["text"].(string); return s }

// buttons returns the callback data of the inline keyboard, by button text.
func buttons(t *testing.T, c call) map[string]string {
	t.Helper()
	out := map[string]string{}
	kb, _ := c.params["reply_markup"].(map[string]any)
	rows, _ := kb["inline_keyboard"].([]any)
	for _, row := range rows {
		for _, b := range row.([]any) {
			m := b.(map[string]any)
			if data, ok := m["callback_data"].(string); ok {
				out[m["text"].(string)] = data
			} else {
				out[m["text"].(string)] = m["url"].(string)
			}
		}
	}
	return out
}

const groupChat = -1001234567890

func TestLinkNotifyAndTriage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// An admin links a group chat with a code from the Alerts page.
	chatCode, err := f.bot.ChatLinkCode(ctx, f.org.ID, nil, f.user.ID)
	if err != nil || chatCode.GetBotUsername() != "izci_test_bot" || !strings.HasSuffix(chatCode.GetGroupLink(), "startgroup="+chatCode.GetCode()) {
		t.Fatalf("chat link code: %v %v", chatCode, err)
	}
	group := telegram.Chat{ID: groupChat, Type: "supergroup", Title: "KentOS Hatalar"}
	f.bot.HandleUpdate(ctx, telegram.Update{Message: &telegram.Message{Chat: group, From: &telegram.User{ID: 555},
		Text: "/baglan@izci_test_bot " + strings.ToLower(chatCode.GetCode()[:4]+"-"+chatCode.GetCode()[4:])}})
	replies := f.api.take("sendMessage")
	if len(replies) != 1 || !strings.Contains(text(replies[0]), "bağlandı") || replies[0].params["chat_id"].(float64) != groupChat {
		t.Fatalf("link reply: %+v", replies)
	}
	var channels, rules int
	f.scalar(t, &channels, `SELECT count(*) FROM notification_channels WHERE org_id = $1`, f.org.ID)
	f.scalar(t, &rules, `SELECT count(*) FROM alert_rules WHERE org_id = $1`, f.org.ID)
	if channels != 1 || rules != 2 {
		t.Fatalf("after linking: %d channels, %d rules", channels, rules)
	}
	// Codes are single-use; commands for other bots are ignored.
	f.bot.HandleUpdate(ctx, telegram.Update{Message: &telegram.Message{Chat: group, Text: "/baglan " + chatCode.GetCode()}})
	f.bot.HandleUpdate(ctx, telegram.Update{Message: &telegram.Message{Chat: group, Text: "/baglan@other_bot " + chatCode.GetCode()}})
	if replies := f.api.take("sendMessage"); len(replies) != 1 || !strings.Contains(text(replies[0]), "geçersiz") {
		t.Fatalf("reused code: %+v", replies)
	}

	// The developer links their Telegram account through /start.
	acc, err := f.bot.AccountLinkCode(ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	private := telegram.Chat{ID: 555, Type: "private", FirstName: "Ayşe"}
	f.bot.HandleUpdate(ctx, telegram.Update{Message: &telegram.Message{Chat: private, From: &telegram.User{ID: 555, Username: "ayse"},
		Text: "/start " + acc.GetCode()}})
	var tgUser int64
	f.scalar(t, &tgUser, `SELECT telegram_user_id FROM users WHERE id = $1`, f.user.ID)
	if tgUser != 555 {
		t.Fatalf("telegram account not linked: %d", tgUser)
	}
	f.api.take("sendMessage")

	// Staging errors do not match the default production rules.
	f.ingestError(t, "staging")
	if n, _ := f.dispatch.DispatchOnce(ctx); n != 0 {
		t.Fatalf("staging event notified %d times", n)
	}

	// A new production error is announced with triage buttons.
	res := f.ingestError(t, "production")
	if n, err := f.dispatch.DispatchOnce(ctx); n != 1 || err != nil {
		t.Fatalf("dispatch: %d %v", n, err)
	}
	sent := f.api.take("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("expected one notification, got %d", len(sent))
	}
	msg := text(sent[0])
	for _, want := range []string{"Yeni hata", res.IssueShortID, "System.TimeoutException", "SmsSender.SendAsync", "Sivas Belediyesi", "production", "1.4.2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("notification lacks %q:\n%s", want, msg)
		}
	}
	btn := buttons(t, sent[0])
	if btn["Aç"] != "https://izci.example.com/o/kentos/issues/"+res.IssueShortID || btn["✅ Çöz"] == "" || len(btn["✅ Çöz"]) > 64 {
		t.Fatalf("buttons: %v", btn)
	}
	var notified int
	f.scalar(t, &notified, `SELECT count(*) FROM issue_activity WHERE issue_id = $1 AND kind = 10`, res.IssueID)
	if notified != 1 {
		t.Fatalf("notified activity: %d", notified)
	}

	// A frequency rule re-announces the same issue by editing the message.
	err = f.db.InOrg(ctx, f.org.ID, func(tx pgx.Tx) error {
		var ch uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM notification_channels`).Scan(&ch); err != nil {
			return err
		}
		_, err := store.SaveAlertRule(ctx, tx, &store.AlertRule{OrgID: f.org.ID, Name: "Sık tekrar", Enabled: true,
			Trigger: store.RuleTrigger{Kind: alert.TriggerFrequency, Threshold: 2, WindowSeconds: 3600}, ChannelIDs: []uuid.UUID{ch}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f.bot.OnRulesChanged(f.org.ID)
	f.ingestError(t, "production")
	if n, _ := f.dispatch.DispatchOnce(ctx); n != 1 {
		t.Fatalf("frequency dispatch: %d", n)
	}
	var messageID int64
	f.scalar(t, &messageID, `SELECT message_id FROM notification_threads WHERE issue_id = $1`, res.IssueID)
	edits := f.api.take("editMessageText")
	if len(f.api.take("sendMessage")) != 0 || len(edits) != 1 || int64(edits[0].params["message_id"].(float64)) != messageID ||
		!strings.Contains(text(edits[0]), "Hata sıklaştı: 1 saat içinde 2 olay") {
		t.Fatalf("frequency should edit message %d in place: %+v", messageID, edits)
	}

	// Pressing "Çöz" resolves the issue for the linked member.
	press := func(from int64, data string) {
		f.bot.HandleUpdate(ctx, telegram.Update{CallbackQuery: &telegram.CallbackQuery{ID: "cb", From: telegram.User{ID: from},
			Message: &telegram.Message{MessageID: messageID, Chat: group}, Data: data}})
	}
	press(777, btn["✅ Çöz"])
	answers := f.api.take("answerCallbackQuery")
	if len(answers) != 1 || !strings.Contains(answers[0].params["text"].(string), "bağlayın") || answers[0].params["show_alert"] != true {
		t.Fatalf("unlinked user answer: %+v", answers)
	}
	press(555, strings.Replace(btn["✅ Çöz"], "r:", "o:", 1))
	if answers := f.api.take("answerCallbackQuery"); len(answers) != 1 || !strings.Contains(answers[0].params["text"].(string), "geçerli değil") {
		t.Fatalf("forged button answer: %+v", answers)
	}
	press(555, btn["✅ Çöz"])
	if answers := f.api.take("answerCallbackQuery"); len(answers) != 1 || answers[0].params["text"] != "Çözüldü olarak işaretlendi" {
		t.Fatalf("resolve answer: %+v", answers)
	}
	var status int16
	f.scalar(t, &status, `SELECT status FROM issues WHERE id = $1`, res.IssueID)
	if status != store.IssueResolved {
		t.Fatalf("issue status after button: %d", status)
	}
	edited := f.api.take("editMessageText")
	if len(edited) != 1 || !strings.Contains(text(edited[0]), "Hata sıklaştı") || !strings.Contains(text(edited[0]), "Çözüldü") ||
		!strings.Contains(text(edited[0]), "Ayşe Yılmaz") ||
		buttons(t, edited[0])["↩️ Yeniden aç"] == "" {
		t.Fatalf("message after resolve: %+v", edited)
	}
	select {
	case id := <-f.triaged:
		if id != res.IssueID {
			t.Fatalf("triage hook got %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("triage hook not called")
	}
}

func TestDeliveryFailures(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var channelID uuid.UUID
	err := f.db.InOrg(ctx, f.org.ID, func(tx pgx.Tx) error {
		ch, _, err := store.UpsertTelegramChannel(ctx, tx, f.org.ID, nil, nil, "Test", store.TelegramTarget{ChatID: groupChat})
		if err != nil {
			return err
		}
		channelID = ch.ID
		return store.EnqueueNotifications(ctx, tx, []store.OutboxItem{{OrgID: f.org.ID, ChannelID: ch.ID, Kind: store.NotifyTest}})
	})
	if err != nil {
		t.Fatal(err)
	}

	// Flood control: retry after the delay Telegram asks for.
	f.api.failNext("sendMessage", `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`)
	if n, _ := f.dispatch.DispatchOnce(ctx); n != 1 {
		t.Fatalf("claimed %d", n)
	}
	var wait float64
	var attempts int
	f.scalar(t, &wait, `SELECT extract(epoch FROM next_attempt_at - now()) FROM notification_outbox`)
	f.scalar(t, &attempts, `SELECT attempts FROM notification_outbox`)
	if wait < 5 || wait > 8 || attempts != 1 {
		t.Fatalf("retry after: %.1fs, %d attempts", wait, attempts)
	}

	// The bot was removed from the group: give up and show it on the channel.
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE notification_outbox SET next_attempt_at = now()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.api.failNext("sendMessage", `{"ok":false,"error_code":403,"description":"Forbidden: bot was kicked from the supergroup chat"}`)
	f.dispatch.DispatchOnce(ctx)
	var lastError string
	var infinite bool
	f.scalar(t, &lastError, `SELECT last_error FROM notification_channels WHERE id = $1`, channelID)
	f.scalar(t, &infinite, `SELECT next_attempt_at = 'infinity' FROM notification_outbox`)
	if !strings.Contains(lastError, "kicked") || !infinite {
		t.Fatalf("permanent failure: %q, given up %v", lastError, infinite)
	}
	if n, _ := f.dispatch.DispatchOnce(ctx); n != 0 {
		t.Fatal("a given-up notification was retried")
	}
}

func TestWebhookAuthentication(t *testing.T) {
	f := newFixture(t)
	h := f.bot.WebhookHandler()
	body := `{"update_id":1,"message":{"message_id":1,"chat":{"id":555,"type":"private"},"text":"/yardim"}}`
	for _, tc := range []struct {
		secret string
		want   int
	}{{"", http.StatusUnauthorized}, {"wrong", http.StatusUnauthorized}, {"s3cret-s3cret-s3cret", http.StatusOK}} {
		req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", tc.secret)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("secret %q: status %d, want %d", tc.secret, rec.Code, tc.want)
		}
	}
	if replies := f.api.take("sendMessage"); len(replies) != 1 || !strings.Contains(text(replies[0]), "/baglan") {
		t.Fatalf("help reply: %+v", replies)
	}
}
