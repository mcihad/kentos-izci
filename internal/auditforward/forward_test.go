package auditforward_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/auditforward"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

func TestForwarder(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()

	var mu sync.Mutex
	var bodies [][]byte
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.Header.Get("X-Izci-Signature") != auditforward.Sign("gizli", body) {
			t.Errorf("bad signature %q", r.Header.Get("X-Izci-Signature"))
		}
		bodies = append(bodies, body)
	}))
	defer srv.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	messages := make(chan string, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					n, err := r.ReadString(' ')
					if err != nil {
						return
					}
					size, _ := strconv.Atoi(strings.TrimSpace(n))
					buf := make([]byte, size)
					if _, err := io.ReadFull(r, buf); err != nil {
						return
					}
					messages <- string(buf)
				}
			}()
		}
	}()

	f := auditforward.New(database, config.AuditConfig{WebhookURL: srv.URL, WebhookSecret: "gizli", SyslogAddr: "tcp://" + ln.Addr().String()},
		slog.New(slog.DiscardHandler))
	f.Grace = -time.Minute
	// The first run only marks where forwarding starts.
	if err := f.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 0 {
		t.Fatalf("first run sent %d batches", len(bodies))
	}
	insert := func(action string) {
		err := database.InSystem(ctx, func(tx pgx.Tx) error {
			return store.InsertAudit(ctx, tx, store.AuditEntry{Action: action, TargetType: "user", TargetName: "ayse@example.com"})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("auth.login")
	insert("auth.login_failed")
	if err := f.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Entries []auditforward.Entry `json:"entries"`
	}
	if len(bodies) != 1 || json.Unmarshal(bodies[0], &got) != nil || len(got.Entries) != 2 || got.Entries[0].Action != "auth.login" ||
		got.Entries[1].Target.Name != "ayse@example.com" {
		t.Fatalf("webhook: %s", bodies)
	}
	for _, want := range []string{"<109>1 ", "<108>1 "} {
		select {
		case m := <-messages:
			if !strings.HasPrefix(m, want) || !strings.Contains(m, " izci - auth.login") || !strings.Contains(m, `"target":{"type":"user"`) {
				t.Fatalf("syslog: %q", m)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no syslog message")
		}
	}

	// A failed delivery is retried: nothing is skipped.
	fail = true
	insert("user.password.change")
	if err := f.Run(ctx); err == nil {
		t.Fatal("failed delivery reported no error")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	if err := f.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || !strings.Contains(string(bodies[1]), "user.password.change") {
		t.Fatalf("retry: %d batches", len(bodies))
	}
	if err := f.Run(ctx); err != nil || len(bodies) != 2 {
		t.Fatalf("nothing new must be sent: %d %v", len(bodies), err)
	}
}
