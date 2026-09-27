package ownership_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/ownership"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// fakeGitHub serves the tree and blame of kentos/kalem.
type fakeGitHub struct {
	srv    *httptest.Server
	blames atomic.Int32
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/kentos/kalem/git/trees/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token ghp_test" {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tree": []map[string]string{
			{"path": "src", "type": "tree"},
			{"path": "src/Billing/Invoice.cs", "type": "blob"},
			{"path": "src/Tahsilat/Api/OdemeController.cs", "type": "blob"},
			{"path": "README.md", "type": "blob"},
		}})
	})
	commit := func(sha, msg, name, email, login string, at time.Time) map[string]any {
		c := map[string]any{"oid": sha, "messageHeadline": msg, "url": "https://github.com/kentos/kalem/commit/" + sha,
			"committedDate": at.Format(time.RFC3339), "author": map[string]any{"name": name, "email": email}}
		if login != "" {
			c["author"].(map[string]any)["user"] = map[string]string{"login": login}
		}
		return c
	}
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		f.blames.Add(1)
		var body struct {
			Variables map[string]string `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var ranges []map[string]any
		switch body.Variables["path"] {
		case "src/Billing/Invoice.cs":
			ranges = []map[string]any{
				{"startingLine": 1, "endingLine": 40, "commit": commit("aaa111", "Add invoices", "Ayşe", "ayse@kentos.com.tr", "", time.Now().AddDate(-1, 0, 0))},
				{"startingLine": 41, "endingLine": 60, "commit": commit("bbb222", "Round tax per line", "Mehmet", "m.personal@example.com", "mehmet-gh", time.Now().Add(-48*time.Hour))},
			}
		case "src/Tahsilat/Api/OdemeController.cs":
			ranges = []map[string]any{{"startingLine": 1, "endingLine": 100, "commit": commit("ccc333", "Payment API", "Ayşe", "ayse@kentos.com.tr", "", time.Now().AddDate(0, -3, 0))}}
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"object": map[string]any{"blame": nil}}},
				"errors": []map[string]string{{"message": "Could not resolve file for path"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"object": map[string]any{
			"blame": map[string]any{"ranges": ranges}}}}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func paymentEvent(message, function string) *ingestv1.Event {
	return &ingestv1.Event{
		Level: ingestv1.Level_LEVEL_ERROR, Platform: "csharp", Environment: "production", Release: "kalem@2.4.0",
		Exceptions: []*ingestv1.Exception{{Type: "InvalidOperationException", Value: message, Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{
			{Module: "Kentos.Tahsilat.Api", Function: "Tamamla", AbsPath: "/app/src/Tahsilat/Api/OdemeController.cs", Lineno: 12, InApp: proto.Bool(true)},
			{Module: "Kentos.Billing", Function: function, AbsPath: "/app/src/Billing/Invoice.cs", Lineno: 42, InApp: proto.Bool(true)},
		}}}},
	}
}

func TestAssignmentAndSuspectCommits(t *testing.T) {
	var svc *ownership.Service
	e := ingesttest.New(t, ingesttest.Options{InTx: func(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error {
		return svc.AssignNew(ctx, tx, b)
	}})
	gh := newFakeGitHub(t)
	box, _ := secret.NewBox(make([]byte, 32))
	access := github.NewAccess(gh.srv.URL, "https://github.com", nil, box, nil)
	svc = ownership.NewService(e.DB, slog.New(slog.DiscardHandler), access)
	ctx := context.Background()

	// Members: Ayşe by e-mail, Mehmet through his GitHub login.
	var ayse, mehmet *store.User
	err := e.DB.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if ayse, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "ayse@kentos.com.tr", Name: "Ayşe"}); err != nil {
			return err
		}
		if mehmet, err = store.CreateUser(ctx, tx, store.NewUserInput{Email: "mehmet@kentos.com.tr", Name: "Mehmet"}); err != nil {
			return err
		}
		for _, u := range []*store.User{ayse, mehmet} {
			if err := store.AddMember(ctx, tx, e.Org.ID, u.ID, apiv1.Role_ROLE_MEMBER); err != nil {
				return err
			}
		}
		if err := store.SetOwnerAlias(ctx, tx, e.Org.ID, "@mehmet-gh", []uuid.UUID{mehmet.ID}); err != nil {
			return err
		}
		id := uuid.New()
		if _, err := store.CreateIntegration(ctx, tx, &store.Integration{ID: id, OrgID: e.Org.ID, Auth: store.IntegrationToken, Name: "kentos",
			AccountLogin: "kentos", Secret: access.SealToken(id, "ghp_test")}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO project_repositories (project_id, org_id, integration_id, provider, owner, name, url, default_branch, path_mappings)
			VALUES ($1, $2, $3, 'github', 'kentos', 'kalem', 'https://github.com/kentos/kalem', 'main', '[]')`, e.Project.ID, e.Org.ID, id); err != nil {
			return err
		}
		return store.SaveOwnership(ctx, tx, &store.Ownership{ProjectID: e.Project.ID, OrgID: e.Org.ID, AutoAssign: store.AutoAssignOwners,
			Rules: "path:src/billing/* ayse@kentos.com.tr", Codeowners: "/src/Tahsilat/ @mehmet-gh\n/src/ @nobody"})
	})
	if err != nil {
		t.Fatal(err)
	}

	client := ingestv1connect.NewIngestServiceClient(http.DefaultClient, e.Server.URL)
	send := func(ev *ingestv1.Event) uuid.UUID {
		t.Helper()
		req := connect.NewRequest(&ingestv1.SendEventRequest{Event: ev})
		req.Header().Set("Authorization", "Bearer "+e.Key)
		res, err := client.SendEvent(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := uuid.Parse(res.Msg.GetResult().GetIssueId())
		return id
	}
	assignee := func(issue uuid.UUID) (string, map[string]string) {
		t.Helper()
		var email string
		var data map[string]string
		err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `SELECT coalesce(u.email, '') FROM issues i LEFT JOIN users u ON u.id = i.assignee_id WHERE i.id = $1`, issue).Scan(&email)
			if err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `SELECT data FROM issue_activity WHERE issue_id = $1 AND kind = $2 AND actor_id IS NULL`, issue, store.ActivityAssigned).Scan(&data)
			if err == pgx.ErrNoRows {
				return nil
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return email, data
	}

	// The project's own rule comes after CODEOWNERS and decides.
	first := send(paymentEvent("Fatura kapatılamadı", "Total"))
	if email, data := assignee(first); email != "ayse@kentos.com.tr" || data["auto"] != "rule" || data["rule"] != "path:src/billing/*" {
		t.Fatalf("assigned by rule: %q %v", email, data)
	}
	owners, err := svc.IssueOwners(ctx, e.Org.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners.Owners) != 2 || owners.Owners[0].User.ID != ayse.ID || owners.Owners[1].User.ID != mehmet.ID ||
		owners.Winner.Text() != "path:src/billing/*" || owners.Assignee.ID != ayse.ID || len(owners.Unmapped) != 1 || owners.Unmapped[0] != "@nobody" {
		t.Fatalf("owners: %+v", owners)
	}

	// Blame: the innermost frame's line changed two days ago by Mehmet
	// (known by his GitHub login, not his e-mail address).
	sc, err := svc.FindSuspectCommit(ctx, e.Org.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if sc.State != store.SuspectFound || sc.SHA != "bbb222" || sc.Path != "src/Billing/Invoice.cs" || sc.Line != 42 || sc.AuthorLogin != "mehmet-gh" {
		t.Fatalf("suspect commit: %+v", sc)
	}
	var author *store.User
	_ = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		author, err = svc.CommitAuthor(ctx, tx, e.Org.ID, sc)
		return err
	})
	if author == nil || author.ID != mehmet.ID {
		t.Fatalf("commit author: %+v", author)
	}

	// Projects assigning to suspect commit authors do it in the background.
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		o, err := store.GetOwnership(ctx, tx, e.Org.ID, e.Project.ID)
		if err != nil {
			return err
		}
		o.AutoAssign = store.AutoAssignSuspectCommit
		return store.SaveOwnership(ctx, tx, o)
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go svc.Run(runCtx)
	second := send(paymentEvent("Ödeme sağlayıcısı yanıt vermedi", "Tax"))
	if second == first {
		t.Fatal("expected a new issue")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		email, data := assignee(second)
		if email == "mehmet@kentos.com.tr" && data["auto"] == "suspect_commit" && data["commit"] == "bbb222" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("assigned by suspect commit: %q %v", email, data)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Without a repository there is nothing to blame.
	_ = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM project_repositories WHERE project_id = $1`, e.Project.ID)
		return err
	})
	if _, err := svc.FindSuspectCommit(ctx, e.Org.ID, first); err != ownership.ErrNotConfigured {
		t.Fatalf("without repository: %v", err)
	}
	if n := gh.blames.Load(); n < 2 || strings.Contains(sc.Detail, "error") {
		t.Fatalf("blames %d, detail %q", n, sc.Detail)
	}
}
