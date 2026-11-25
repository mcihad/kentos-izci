package notify_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// smtpSink is a minimal SMTP server that records messages.
type smtpSink struct {
	ln   net.Listener
	mu   sync.Mutex
	msgs []sinkMessage
	// reject answers RCPT with this reply when set, e.g. "550 no such user".
	reject string
}

type sinkMessage struct {
	from string
	to   []string
	data string
}

func newSMTPSink(t *testing.T) *smtpSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpSink{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *smtpSink) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
	w("220 sink ESMTP")
	var m sinkMessage
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			w("250-sink")
			w("250 8BITMIME")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			m = sinkMessage{from: angle(line)}
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			if s.reject != "" {
				w(s.reject)
				continue
			}
			m.to = append(m.to, angle(line))
			w("250 ok")
		case cmd == "DATA":
			w("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			m.data = b.String()
			s.mu.Lock()
			s.msgs = append(s.msgs, m)
			s.mu.Unlock()
			w("250 queued")
		case cmd == "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

// angle returns the address in <…> of an SMTP command.
func angle(line string) string {
	_, rest, _ := strings.Cut(line, "<")
	addr, _, _ := strings.Cut(rest, ">")
	return addr
}

func (s *smtpSink) messages() []sinkMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkMessage(nil), s.msgs...)
}

// fixture is an ingest environment with one issue and a dispatcher.
type fixture struct {
	env      *ingesttest.Env
	box      *secret.Box
	dispatch *notify.Dispatcher
	issueID  uuid.UUID
	eventID  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := ingesttest.New(t, ingesttest.Options{})
	box, _ := secret.NewBox(make([]byte, 32))
	f := &fixture{env: env, box: box, dispatch: notify.NewDispatcher(env.DB, slog.New(slog.DiscardHandler), "https://izci.example.com")}
	// One issue with an event from Sivas.
	client := ingestv1connect.NewIngestServiceClient(http.DefaultClient, env.Server.URL)
	req := connect.NewRequest(&ingestv1.SendEventRequest{Event: &ingestv1.Event{
		Level: ingestv1.Level_LEVEL_ERROR, Environment: "production", Release: "kalem@1.4.2",
		Customer: &ingestv1.Customer{Id: "sivas", Name: "Sivas Belediyesi"},
		Exceptions: []*ingestv1.Exception{{Type: "NullReferenceException", Value: "Object reference not set <b>",
			Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{{Function: "OrderService.Create", Filename: "Services/OrderService.cs",
				Lineno: 42, InApp: new(true)}}}}},
	}})
	req.Header().Set("Authorization", "Bearer "+env.Key)
	res, err := client.SendEvent(context.Background(), req)
	if err != nil || res.Msg.GetResult().GetIssueId() == "" {
		t.Fatalf("ingest: %v %v", res, err)
	}
	f.issueID = uuid.MustParse(res.Msg.GetResult().GetIssueId())
	env.Scalar(t, &f.eventID, `SELECT id::text FROM events LIMIT 1`)
	return f
}

// channel stores a channel of a kind with settings built by build (which
// gets the channel id to seal secrets for).
func (f *fixture) channel(t *testing.T, kind int16, name string, build func(id uuid.UUID) any) *store.Channel {
	t.Helper()
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	cfg, _ := json.Marshal(build(id))
	var ch *store.Channel
	err := f.env.DB.InOrg(ctx, f.env.Org.ID, func(tx pgx.Tx) error {
		var err error
		ch, err = store.InsertChannel(ctx, tx, &store.Channel{ID: id, OrgID: f.env.Org.ID, Kind: kind, Name: name, Config: cfg})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// notify queues a notification of the fixture's issue for a channel.
func (f *fixture) enqueue(t *testing.T, ch *store.Channel, kind int16) {
	t.Helper()
	ctx := context.Background()
	item := store.OutboxItem{OrgID: f.env.Org.ID, ChannelID: ch.ID, Kind: kind, Payload: map[string]any{"event_id": f.eventID}}
	if kind != store.NotifyTest {
		item.IssueID = &f.issueID
	}
	if kind == store.NotifyFrequency {
		item.Payload["threshold"], item.Payload["window_seconds"] = 50, 900
	}
	err := f.env.DB.InOrg(ctx, f.env.Org.ID, func(tx pgx.Tx) error { return store.EnqueueNotifications(ctx, tx, []store.OutboxItem{item}) })
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) dispatchAll(t *testing.T) {
	t.Helper()
	if _, err := f.dispatch.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) lastError(t *testing.T, ch *store.Channel) string {
	var s string
	f.env.Scalar(t, &s, `SELECT last_error FROM notification_channels WHERE id = $1`, ch.ID)
	return s
}

func TestEmail(t *testing.T) {
	f := newFixture(t)
	sink := newSMTPSink(t)
	host, port, _ := net.SplitHostPort(sink.ln.Addr().String())
	p, _ := strconv.Atoi(port)
	email, err := notify.NewEmail(config.SMTPConfig{Host: host, Port: p, From: "İzci <izci@kentos.example>", TLS: "none"})
	if err != nil {
		t.Fatal(err)
	}
	f.dispatch.Register(store.ChannelEmail, email)
	ch := f.channel(t, store.ChannelEmail, "Ekip", func(uuid.UUID) any {
		return notify.EmailConfig{Recipients: []string{"ali@kentos.example", "ayse@kentos.example"}}
	})
	f.enqueue(t, ch, store.NotifyNewIssue)
	f.dispatchAll(t)
	f.enqueue(t, ch, store.NotifyRegression)
	f.dispatchAll(t)

	msgs := sink.messages()
	if len(msgs) != 2 || msgs[0].from != "izci@kentos.example" || len(msgs[0].to) != 2 {
		t.Fatalf("messages: %+v", msgs)
	}
	first, err := mail.ReadMessage(strings.NewReader(msgs[0].data))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(first.Header.Get("Subject"))
	if !strings.HasPrefix(subject, "[İzci] Yeni hata: KALEM-1 NullReferenceException") {
		t.Fatalf("subject: %q", subject)
	}
	body, _ := io.ReadAll(first.Body)
	for _, want := range []string{"Sivas Belediyesi", "kalem@1.4.2", "OrderService.Create", "https://izci.example.com/o/kentos/issues/KALEM-1",
		"Object reference not set &lt;b&gt;"} {
		if !strings.Contains(decodeQP(string(body)), want) {
			t.Errorf("body lacks %q", want)
		}
	}
	second, _ := mail.ReadMessage(strings.NewReader(msgs[1].data))
	if second.Header.Get("In-Reply-To") != first.Header.Get("Message-ID") {
		t.Fatalf("threading: %q vs %q", second.Header.Get("In-Reply-To"), first.Header.Get("Message-ID"))
	}
	if s, _ := new(mime.WordDecoder).DecodeHeader(second.Header.Get("Subject")); !strings.Contains(s, "Çözülmüş hata geri döndü") {
		t.Fatalf("second subject: %q", s)
	}
	var activities int
	f.env.Scalar(t, &activities, `SELECT count(*) FROM issue_activity WHERE issue_id = $1 AND kind = 10`, f.issueID)
	if activities != 2 {
		t.Fatalf("notified activities: %d", activities)
	}

	// A rejected recipient is permanent: the channel shows why.
	sink.reject = "550 5.1.1 no such user"
	f.enqueue(t, ch, store.NotifyRegression)
	f.dispatchAll(t)
	if e := f.lastError(t, ch); !strings.Contains(e, "550") {
		t.Fatalf("last error: %q", e)
	}
}

func decodeQP(s string) string {
	return strings.NewReplacer("=\r\n", "", "=3D", "=", "=C3=A7", "ç", "=C4=B1", "ı").Replace(s)
}

type captured struct {
	mu   sync.Mutex
	reqs []*http.Request
	body [][]byte
}

func (c *captured) handler(status int, header map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.reqs, c.body = append(c.reqs, r), append(c.body, b)
		c.mu.Unlock()
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte("ok"))
	}
}

func TestSlackTeamsWebhook(t *testing.T) {
	f := newFixture(t)
	hooks := notify.NewWebhooks(f.box, notify.NewHTTPClient(true))
	f.dispatch.Register(store.ChannelSlack, hooks.Slack())
	f.dispatch.Register(store.ChannelTeams, hooks.Teams())
	f.dispatch.Register(store.ChannelWebhook, hooks.Generic())
	var slack, teams, hook captured
	slackSrv := httptest.NewServer(slack.handler(200, nil))
	teamsSrv := httptest.NewServer(teams.handler(202, nil))
	hookSrv := httptest.NewServer(hook.handler(204, nil))
	defer slackSrv.Close()
	defer teamsSrv.Close()
	defer hookSrv.Close()
	const signing = "whsec_test"
	cfgFor := func(u string, signed bool) func(id uuid.UUID) any {
		return func(id uuid.UUID) any {
			c := notify.WebhookConfig{URLSealed: notify.Seal(f.box, id, u), URLHint: notify.URLHint(u)}
			if signed {
				c.SecretSealed = notify.Seal(f.box, id, signing)
			}
			return c
		}
	}
	sc := f.channel(t, store.ChannelSlack, "Slack", cfgFor(slackSrv.URL+"/services/T1/B2/abcd", false))
	tc := f.channel(t, store.ChannelTeams, "Teams", cfgFor(teamsSrv.URL+"/workflows/x", false))
	wc := f.channel(t, store.ChannelWebhook, "Webhook", cfgFor(hookSrv.URL+"/izci", true))
	for _, ch := range []*store.Channel{sc, tc, wc} {
		f.enqueue(t, ch, store.NotifyFrequency)
	}
	f.dispatchAll(t)

	var sp struct {
		Text   string `json:"text"`
		Blocks []struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
		} `json:"blocks"`
	}
	_ = json.Unmarshal(slack.body[0], &sp)
	var texts []string
	for _, b := range sp.Blocks {
		texts = append(texts, b.Text.Text)
	}
	all := strings.Join(texts, "\n")
	if !strings.Contains(all, "*Hata sıklaştı: 15 dakika içinde 50 olay*  <https://izci.example.com/o/kentos/issues/KALEM-1|KALEM-1>") ||
		!strings.Contains(all, "Object reference not set &lt;b&gt;") || !strings.Contains(all, "*Müşteri:* Sivas Belediyesi") ||
		!strings.Contains(all, "`OrderService.Create in OrderService.cs:42`") || !strings.Contains(sp.Text, "KALEM-1") {
		t.Fatalf("slack: %s", slack.body[0])
	}
	var tp map[string]any
	_ = json.Unmarshal(teams.body[0], &tp)
	card := tp["attachments"].([]any)[0].(map[string]any)
	content, _ := json.Marshal(card["content"])
	if card["contentType"] != "application/vnd.microsoft.card.adaptive" || !strings.Contains(string(content), `"type":"FactSet"`) ||
		!strings.Contains(string(content), `"url":"https://izci.example.com/o/kentos/issues/KALEM-1"`) {
		t.Fatalf("teams: %s", teams.body[0])
	}
	req := hook.reqs[0]
	ts, _ := strconv.ParseInt(req.Header.Get("X-Izci-Timestamp"), 10, 64)
	if req.Header.Get("X-Izci-Event") != "issue.frequency" || req.Header.Get("X-Izci-Signature") != notify.Signature(signing, ts, hook.body[0]) {
		t.Fatalf("webhook headers: %v", req.Header)
	}
	var wp map[string]any
	_ = json.Unmarshal(hook.body[0], &wp)
	issue := wp["issue"].(map[string]any)
	if issue["short_id"] != "KALEM-1" || issue["level"] != "error" || wp["trigger"].(map[string]any)["threshold"] != float64(50) ||
		wp["latest_event"].(map[string]any)["customer"] != "Sivas Belediyesi" {
		t.Fatalf("webhook payload: %s", hook.body[0])
	}

	// Errors: a revoked Slack webhook gives up; a rate limited one retries.
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no_service"))
	}))
	defer gone.Close()
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer busy.Close()
	g := f.channel(t, store.ChannelSlack, "Kapalı", cfgFor(gone.URL+"/x", false))
	b := f.channel(t, store.ChannelSlack, "Yoğun", cfgFor(busy.URL+"/x", false))
	f.enqueue(t, g, store.NotifyNewIssue)
	f.enqueue(t, b, store.NotifyNewIssue)
	f.dispatchAll(t)
	if e := f.lastError(t, g); !strings.Contains(e, "404") || !strings.Contains(e, "no_service") {
		t.Fatalf("gone: %q", e)
	}
	var next time.Time
	f.env.Scalar(t, &next, `SELECT next_attempt_at FROM notification_outbox WHERE channel_id = $1`, b.ID)
	if d := time.Until(next); d < 100*time.Second || d > 125*time.Second {
		t.Fatalf("retry in %v", d)
	}
}

func TestPrivateNetworksBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	box, _ := secret.NewBox(make([]byte, 32))
	hooks := notify.NewWebhooks(box, notify.NewHTTPClient(false))
	id := uuid.New()
	cfg, _ := json.Marshal(notify.WebhookConfig{URLSealed: notify.Seal(box, id, srv.URL)})
	_, err := hooks.Generic().Send(context.Background(), &notify.Notification{
		Item: store.OutboxItem{ID: uuid.New(), Kind: store.NotifyTest}, Channel: &store.Channel{ID: id, Name: "iç", Config: cfg}})
	var pe *notify.PermanentError
	if !errors.As(err, &pe) || !strings.Contains(pe.Reason, "NOTIFY_ALLOW_PRIVATE_NETWORKS") {
		t.Fatalf("loopback must be refused: %v", err)
	}
}

func TestJira(t *testing.T) {
	f := newFixture(t)
	jira := notify.NewJira(f.box, notify.NewHTTPClient(true))
	f.dispatch.Register(store.ChannelJira, jira)
	var mu sync.Mutex
	var created []map[string]any
	var comments []string
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		auths = append(auths, r.Header.Get("Authorization"))
		switch {
		case r.Method == "GET" && (r.URL.Path == "/rest/api/2/myself" || r.URL.Path == "/rest/api/2/project/KALEM"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "GET":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"errorMessages":["No project could be found with key 'YOK'."]}`))
		case r.URL.Path == "/rest/api/2/issue":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			created = append(created, in)
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"10042","key":"KALEM-123","self":"x"}`))
		case r.URL.Path == "/rest/api/2/issue/KALEM-123/comment":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			comments = append(comments, in["body"])
			w.WriteHeader(201)
		default:
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()
	cfg := notify.JiraConfig{BaseURL: srv.URL, ProjectKey: "KALEM", IssueType: "Bug", Email: "ci@kentos.example", Labels: []string{"kalem"}}
	if err := jira.CheckJira(context.Background(), cfg, "api-token"); err != nil {
		t.Fatal(err)
	}
	bad := cfg
	bad.ProjectKey = "YOK"
	if err := jira.CheckJira(context.Background(), bad, "api-token"); err == nil || !strings.Contains(err.Error(), "No project could be found") {
		t.Fatalf("unknown project: %v", err)
	}
	ch := f.channel(t, store.ChannelJira, "Jira", func(id uuid.UUID) any {
		c := cfg
		c.TokenSealed = notify.Seal(f.box, id, "api-token")
		return c
	})
	f.enqueue(t, ch, store.NotifyTest)
	f.enqueue(t, ch, store.NotifyNewIssue)
	f.enqueue(t, ch, store.NotifyRegression)
	f.dispatchAll(t)

	if len(created) != 1 || len(comments) != 1 {
		t.Fatalf("created %d tickets, %d comments", len(created), len(comments))
	}
	fields := created[0]["fields"].(map[string]any)
	if fields["summary"] != "KALEM-1: NullReferenceException: Object reference not set <b>" ||
		fields["issuetype"].(map[string]any)["name"] != "Bug" || !strings.Contains(fields["description"].(string), "{noformat}") ||
		!strings.Contains(fields["description"].(string), "*Müşteri:* Sivas Belediyesi") {
		t.Fatalf("ticket: %v", fields)
	}
	if labels, _ := json.Marshal(fields["labels"]); string(labels) != `["izci","kalem"]` {
		t.Fatalf("labels: %s", labels)
	}
	if !strings.Contains(comments[0], "Çözülmüş hata geri döndü") {
		t.Fatalf("comment: %q", comments[0])
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("ci@kentos.example:api-token"))
	for _, a := range auths {
		if a != want {
			t.Fatalf("auth header %q", a)
		}
	}
	var key string
	f.env.Scalar(t, &key, `SELECT data ->> 'key' FROM issue_activity WHERE issue_id = $1 AND kind = 11`, f.issueID)
	if key != "KALEM-123" {
		t.Fatalf("ticket activity: %q", key)
	}
	var tickets int
	f.env.Scalar(t, &tickets, `SELECT count(*) FROM notification_threads WHERE channel_id = $1 AND payload ->> 'key' = 'KALEM-123'`, ch.ID)
	if tickets != 1 {
		t.Fatal("ticket thread missing")
	}
}

func TestUnavailableKind(t *testing.T) {
	f := newFixture(t)
	ch := f.channel(t, store.ChannelEmail, "Posta", func(uuid.UUID) any { return notify.EmailConfig{Recipients: []string{"a@b.c"}} })
	f.enqueue(t, ch, store.NotifyNewIssue)
	f.dispatchAll(t)
	if e := f.lastError(t, ch); !strings.Contains(e, "email notifications are not configured") {
		t.Fatalf("last error: %q", e)
	}
}

// TestSendMail checks account e-mails (sign-up links) through SMTP.
func TestSendMail(t *testing.T) {
	sink := newSMTPSink(t)
	host, port, _ := net.SplitHostPort(sink.ln.Addr().String())
	p, _ := strconv.Atoi(port)
	email, err := notify.NewEmail(config.SMTPConfig{Host: host, Port: p, From: "izci@kentos.example", TLS: "none"})
	if err != nil {
		t.Fatal(err)
	}
	link := "https://izci.example.com/verify/izv_abc"
	if err := email.SendMail(context.Background(), "ayse@kentos.example", "İzci hesabınızı etkinleştirin",
		"Bağlantı: "+link, `<a href="`+link+`">Hesabımı etkinleştir</a>`); err != nil {
		t.Fatal(err)
	}
	msgs := sink.messages()
	if len(msgs) != 1 || len(msgs[0].to) != 1 || msgs[0].to[0] != "ayse@kentos.example" {
		t.Fatalf("messages: %+v", msgs)
	}
	m, err := mail.ReadMessage(strings.NewReader(msgs[0].data))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	from, _ := mail.ParseAddress(m.Header.Get("From"))
	if subject != "İzci hesabınızı etkinleştirin" || from == nil || from.Name != "İzci" || m.Header.Get("Message-ID") == "" {
		t.Fatalf("headers: %v", m.Header)
	}
	body, _ := io.ReadAll(m.Body)
	if strings.Count(decodeQP(string(body)), link) != 2 {
		t.Fatalf("body: %s", body)
	}

	sink.reject = "550 no such user"
	if err := email.SendMail(context.Background(), "yok@kentos.example", "x", "x", "x"); err == nil {
		t.Fatal("a rejected recipient must fail")
	}
}
