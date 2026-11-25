package ingest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// TestUploadKeyUsedByBrowser checks that a key which can upload loses that
// permission once a browser sends events with it: it is public then.
func TestUploadKeyUsedByBrowser(t *testing.T) {
	e := ingesttest.New(t, ingesttest.Options{})
	ctx := context.Background()
	client := ingestv1connect.NewIngestServiceClient(http.DefaultClient, e.Server.URL)
	call := func(key, origin string, release bool) error {
		var err error
		if release {
			req := connect.NewRequest(&ingestv1.RegisterReleaseRequest{Version: "kalem-web@5.1.0"})
			req.Header().Set("Authorization", "Bearer "+key)
			if origin != "" {
				req.Header().Set("Origin", origin)
			}
			_, err = client.RegisterRelease(ctx, req)
		} else {
			req := connect.NewRequest(&ingestv1.SendEventRequest{Event: browserEvent(chrome129, "https://kalem.example.com/", app)})
			req.Header().Set("Authorization", "Bearer "+key)
			if origin != "" {
				req.Header().Set("Origin", origin)
			}
			_, err = client.SendEvent(ctx, req)
		}
		return err
	}
	denied := func(err error, want string) bool {
		var cerr *connect.Error
		return errors.As(err, &cerr) && cerr.Code() == connect.CodePermissionDenied && strings.Contains(cerr.Message(), want)
	}

	// The test key has both scopes; CI uses it without an Origin header.
	if err := call(e.Key, "", true); err != nil {
		t.Fatalf("release from CI: %v", err)
	}
	if err := call(e.Key, "https://kalem.example.com", true); !denied(err, "from a browser") {
		t.Fatalf("release from a browser: %v", err)
	}
	// A browser sends events with it: events are accepted, uploads end.
	if err := call(e.Key, "https://kalem.example.com", false); err != nil {
		t.Fatalf("browser event: %v", err)
	}
	if err := call(e.Key, "", true); !denied(err, ingest.ErrKeyExposed) {
		t.Fatalf("release after the key was used by a browser: %v", err)
	}
	if err := call(e.Key, "", false); err != nil {
		t.Fatalf("server event after exposure: %v", err)
	}
	var seen bool
	e.Scalar(t, &seen, `SELECT browser_seen_at IS NOT NULL FROM api_keys WHERE id = $1`, e.KeyID)
	if !seen {
		t.Fatal("browser_seen_at not recorded")
	}

	// Ingest-only keys are not affected.
	var ingestOnly string
	err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		_, secret, err := store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: e.Org.ID, ProjectID: e.Project.ID, Name: "web",
			Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}})
		ingestOnly = secret
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := call(ingestOnly, "https://kalem.example.com", false); err != nil {
		t.Fatalf("browser event with an ingest-only key: %v", err)
	}
	if err := call(ingestOnly, "", true); !denied(err, "lacks the RELEASES scope") {
		t.Fatalf("release with an ingest-only key: %v", err)
	}
}
