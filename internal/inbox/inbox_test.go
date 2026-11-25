package inbox_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/inbox"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func TestMentions(t *testing.T) {
	ayse, mehmet, ali1, ali2 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members := []inbox.Member{{ID: ayse, Email: "Ayse@kentos.com.tr"}, {ID: mehmet, Email: "mehmet@kentos.com.tr"},
		{ID: ali1, Email: "ali@kentos.com.tr"}, {ID: ali2, Email: "ali@example.com"}}
	got := inbox.Mentions("@mehmet bakar mısın? @ayse@kentos.com.tr da baksın. @MEHMET. ali@example.com yazdı, @ali belirsiz, @ali@example.com değil", members)
	if want := []uuid.UUID{mehmet, ayse, ali2}; !slices.Equal(got, want) {
		t.Fatalf("mentions: %v, want %v", got, want)
	}
	if got := inbox.Mentions("e-posta: mehmet@kentos.com.tr", members); len(got) != 0 {
		t.Fatalf("an address in text is not a mention: %v", got)
	}
}

type sentMail struct{ to, subject, text, html string }

type outbox struct {
	mu       sync.Mutex
	mails    []sentMail
	telegram map[int64][]string
	fail     bool
}

func (o *outbox) SendMail(_ context.Context, to, subject, text, html string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return errors.New("smtp down")
	}
	o.mails = append(o.mails, sentMail{to, subject, text, html})
	return nil
}

func (o *outbox) SendDirect(_ context.Context, userID int64, html string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return errors.New("telegram down")
	}
	o.telegram[userID] = append(o.telegram[userID], html)
	return nil
}

func TestDelivery(t *testing.T) {
	e := ingesttest.New(t, ingesttest.Options{InTx: func(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error {
		_, err := inbox.Regressions(ctx, tx, b)
		return err
	}})
	ctx := context.Background()
	var ayse, mehmet *store.User
	telegramID := int64(1001)
	err := e.DB.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if ayse, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@kentos.com.tr", Name: "Ayşe"}); err != nil {
			return err
		}
		if mehmet, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "mehmet@kentos.com.tr", Name: "Mehmet"}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET telegram_user_id = $2 WHERE id = $1`, ayse.ID, telegramID); err != nil {
			return err
		}
		for _, u := range []*store.User{ayse, mehmet} {
			if err := store.AddMember(ctx, tx, e.Org.ID, u.ID, apiv1.Role_ROLE_MEMBER); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	issue := submit(t, e)

	box := &outbox{telegram: map[int64][]string{}}
	svc := inbox.New(e.DB, slog.New(slog.DiscardHandler), box, box, "https://izci.test")
	notify := func(ns ...store.PersonalNotification) {
		t.Helper()
		if err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error { return store.AddPersonalNotifications(ctx, tx, ns) }); err != nil {
			t.Fatal(err)
		}
	}
	notify(
		store.PersonalNotification{OrgID: e.Org.ID, UserID: ayse.ID, IssueID: issue, Kind: store.NotifyAssigned, ActorID: &mehmet.ID},
		store.PersonalNotification{OrgID: e.Org.ID, UserID: ayse.ID, IssueID: issue, Kind: store.NotifyCommented, ActorID: &mehmet.ID,
			Data: map[string]string{"text": "Bakıyorum <b>hemen</b>"}},
		store.PersonalNotification{OrgID: e.Org.ID, UserID: mehmet.ID, IssueID: issue, Kind: store.NotifyMentioned, ActorID: &ayse.ID,
			Data: map[string]string{"text": "@mehmet bakar mısın?"}},
	)
	n, err := svc.DeliverOnce(ctx)
	if err != nil || n != 3 {
		t.Fatalf("deliver: %d %v", n, err)
	}
	// By default the assignment goes by e-mail and Telegram, comments on
	// followed issues stay in the inbox; mentions go by e-mail (Mehmet has
	// no Telegram).
	if len(box.mails) != 2 || len(box.telegram[telegramID]) != 1 {
		t.Fatalf("sent: %d mails, telegram %v", len(box.mails), box.telegram)
	}
	for _, m := range box.mails {
		switch m.to {
		case "ayse@kentos.com.tr":
			if !strings.Contains(m.subject, "Mehmet size") || strings.Contains(m.text, "Bakıyorum") ||
				!strings.Contains(m.text, "https://izci.test/o/"+e.Org.Slug+"/issues/") {
				t.Fatalf("assignment mail: %+v", m)
			}
		case "mehmet@kentos.com.tr":
			if !strings.Contains(m.subject, "Ayşe sizi") || !strings.Contains(m.html, "@mehmet bakar mısın?") {
				t.Fatalf("mention mail: %+v", m)
			}
		default:
			t.Fatalf("mail to %s", m.to)
		}
	}
	if tg := box.telegram[telegramID][0]; !strings.Contains(tg, "Mehmet size") || strings.Contains(tg, "<b>hemen</b>") {
		t.Fatalf("telegram: %s", tg)
	}
	if n, _ := svc.DeliverOnce(ctx); n != 0 {
		t.Fatalf("delivered twice: %d", n)
	}

	// Failures are retried later, not at once.
	box.fail = true
	notify(store.PersonalNotification{OrgID: e.Org.ID, UserID: mehmet.ID, IssueID: issue, Kind: store.NotifyAssigned, ActorID: &ayse.ID})
	if n, _ := svc.DeliverOnce(ctx); n != 1 {
		t.Fatalf("first attempt: %d", n)
	}
	if n, _ := svc.DeliverOnce(ctx); n != 0 {
		t.Fatalf("retried at once: %d", n)
	}
	var pending int
	e.Scalar(t, &pending, `SELECT count(*) FROM user_notifications WHERE delivered_at IS NULL AND attempts = 1`)
	if pending != 1 {
		t.Fatalf("pending after a failure: %d", pending)
	}

	// Followers hear about an issue that came back.
	if err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		if err := store.FollowIssues(ctx, tx, e.Org.ID, []uuid.UUID{issue}, ayse.ID); err != nil {
			return err
		}
		return store.ResolveIssues(ctx, tx, e.Org.ID, []uuid.UUID{issue}, mehmet.ID, "", false)
	}); err != nil {
		t.Fatal(err)
	}
	if again := submit(t, e); again != issue {
		t.Fatal("the event opened another issue")
	}
	var regressed int
	e.Scalar(t, &regressed, `SELECT count(*) FROM user_notifications WHERE user_id = $1 AND kind = $2`, ayse.ID, store.NotifyRegressed)
	if regressed != 1 {
		t.Fatalf("regression notifications: %d", regressed)
	}
}

// submit ingests the same error and returns its issue.
func submit(t *testing.T, e *ingesttest.Env) uuid.UUID {
	t.Helper()
	ev := &ingestv1.Event{Level: ingestv1.Level_LEVEL_ERROR, Platform: "go",
		Exceptions: []*ingestv1.Exception{{Type: "runtime.Error", Value: "integer divide by zero", Stacktrace: &ingestv1.Stacktrace{
			Frames: []*ingestv1.Frame{{Function: "calc.Divide", Filename: "/app/calc.go", Lineno: 5, InApp: proto.Bool(true)}}}}}}
	p, err := ingest.Normalize(ev, ingest.NormalizeOptions{Project: ingest.ProjectContext{OrgID: e.Org.ID, OrgSlug: e.Org.Slug,
		ProjectID: e.Project.ID, ProjectSlug: e.Project.Slug, Settings: e.Project.Settings}, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	chans, err := e.Pipeline.Submit(p)
	if err != nil {
		t.Fatal(err)
	}
	r := <-chans[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return r.IssueID
}
