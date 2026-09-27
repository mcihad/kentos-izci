package ingest_test

import (
	"context"
	"net/http"
	"net/netip"
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

func browserEvent(ua, url, frame string) *ingestv1.Event {
	return &ingestv1.Event{
		Platform: "javascript", Environment: "production", Release: "kalem-web@5.1.0",
		Request: &ingestv1.Request{Url: url, Headers: []*ingestv1.Header{{Name: "User-Agent", Value: ua}}},
		Exceptions: []*ingestv1.Exception{{Type: "TypeError", Value: "Cannot read properties of undefined (reading 'id')",
			Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{{Filename: frame, Function: "x", Lineno: 1, Colno: 10}}}}},
	}
}

const (
	chrome129 = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"
	app       = "https://kalem.example.com/assets/app.js"
)

func TestFilters(t *testing.T) {
	all := ingest.CompileFilters(&apiv1.ProjectSettings{
		IgnoredEnvironments: []string{"development"},
		Filters: &apiv1.InboundFilters{BrowserExtensions: true, WebCrawlers: true, LegacyBrowsers: true, Localhost: true,
			ErrorMessages: []string{"*ResizeObserver loop*", "ChunkLoadError*"},
			IpAddresses:   []string{"10.20.0.0/16", "203.0.113.7"},
			Releases:      []string{"*-dev", "kalem-web@4.*"}},
	})
	none := ingest.CompileFilters(&apiv1.ProjectSettings{})
	public := netip.MustParseAddr("198.51.100.4")
	for _, c := range []struct {
		name   string
		ev     *ingestv1.Event
		from   netip.Addr
		reason ingest.FilterReason
	}{
		{"ordinary browser error", browserEvent(chrome129, "https://kalem.example.com/basvuru", app), public, ingest.FilterNone},
		{"development environment", func() *ingestv1.Event {
			ev := browserEvent(chrome129, "", app)
			ev.Environment = "Development"
			return ev
		}(), public, ingest.FilterEnvironment},
		{"chrome extension frame", browserEvent(chrome129, "", "chrome-extension://abcdef/content.js"), public, ingest.FilterBrowserExtension},
		{"safari masked extension", browserEvent(chrome129, "", "webkit-masked-url://hidden/"), public, ingest.FilterBrowserExtension},
		{"cross-origin script error", func() *ingestv1.Event {
			ev := browserEvent(chrome129, "", app)
			ev.Exceptions = nil
			ev.Message = &ingestv1.Message{Formatted: "Script error."}
			return ev
		}(), public, ingest.FilterBrowserExtension},
		{"googlebot", browserEvent("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "", app), public, ingest.FilterWebCrawler},
		{"uptime robot", browserEvent("Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)", "", app), public, ingest.FilterWebCrawler},
		{"internet explorer 11", browserEvent("Mozilla/5.0 (Windows NT 10.0; Trident/7.0; rv:11.0) like Gecko", "", app), public, ingest.FilterLegacyBrowser},
		{"safari 11", browserEvent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/11.1.2 Safari/605.1.15", "", app), public, ingest.FilterLegacyBrowser},
		{"chrome 79", browserEvent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.88 Safari/537.36", "", app), public, ingest.FilterLegacyBrowser},
		{"safari 17 is fine", browserEvent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "", app), public, ingest.FilterNone},
		{"localhost page", browserEvent(chrome129, "http://localhost:5173/basvuru", app), public, ingest.FilterLocalhost},
		{"loopback sender", browserEvent(chrome129, "https://kalem.example.com/", app), netip.MustParseAddr("127.0.0.1"), ingest.FilterLocalhost},
		{"office network", browserEvent(chrome129, "", app), netip.MustParseAddr("10.20.3.4"), ingest.FilterIPAddress},
		{"single address", browserEvent(chrome129, "", app), netip.MustParseAddr("203.0.113.7"), ingest.FilterIPAddress},
		{"message pattern", func() *ingestv1.Event {
			ev := browserEvent(chrome129, "", app)
			ev.Exceptions[0].Value = "ResizeObserver loop completed with undelivered notifications."
			return ev
		}(), public, ingest.FilterErrorMessage},
		{"type pattern", func() *ingestv1.Event {
			ev := browserEvent(chrome129, "", app)
			ev.Exceptions[0].Type, ev.Exceptions[0].Value = "ChunkLoadError", "Loading chunk 7 failed."
			return ev
		}(), public, ingest.FilterErrorMessage},
		{"release pattern", func() *ingestv1.Event {
			ev := browserEvent(chrome129, "", app)
			ev.Release = "Kalem-Web@4.9.1"
			return ev
		}(), public, ingest.FilterRelease},
		{"dev build", func() *ingestv1.Event {
			ev := browserEvent(chrome129, "", app)
			ev.Release = "5.2.0-dev"
			return ev
		}(), public, ingest.FilterRelease},
	} {
		if got := all.Match(c.ev, c.from, ""); got != c.reason {
			t.Errorf("%s: %v, want %v", c.name, got.Name(), c.reason.Name())
		}
		if got := none.Match(c.ev, c.from, ""); got != ingest.FilterNone && c.reason != ingest.FilterNone {
			t.Errorf("%s: filtered without filters (%v)", c.name, got.Name())
		}
	}
	// Server-side events: extension messages only apply to browsers, and a
	// server's own address is the client address.
	server := &ingestv1.Event{Platform: "dotnet", Environment: "production",
		Message: &ingestv1.Message{Formatted: "Script error."}}
	if got := all.Match(server, netip.MustParseAddr("198.51.100.9"), ""); got != ingest.FilterNone {
		t.Errorf("server message: %v", got.Name())
	}
	if got := all.Match(server, netip.MustParseAddr("10.20.0.5"), ""); got != ingest.FilterIPAddress {
		t.Errorf("server in filtered range: %v", got.Name())
	}
}

func TestGlob(t *testing.T) {
	for pattern, cases := range map[string]map[string]bool{
		"*timeout*":  {"Request TIMEOUT after 30s": true, "time out": false},
		"kalem@1.?":  {"kalem@1.2": true, "kalem@1.23": false},
		"a.b":        {"a.b": true, "axb": false},
		"*loop*":     {"first line\nResizeObserver loop": true},
		"exact text": {"exact text": true, "exact text!": false},
	} {
		re, err := ingest.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for in, want := range cases {
			if re.MatchString(in) != want {
				t.Errorf("Glob(%q).Match(%q) = %v", pattern, in, !want)
			}
		}
	}
	for _, bad := range []string{"10.0.0.0/33", "not-an-ip", "300.1.1.1"} {
		if _, err := ingest.ParseIPFilter(bad); err == nil {
			t.Errorf("ParseIPFilter(%q) accepted", bad)
		}
	}
	if p, _ := ingest.ParseIPFilter("10.1.2.3/8"); p.String() != "10.0.0.0/8" {
		t.Errorf("mask: %v", p)
	}
}

// TestFilteredEventsAreCounted sends events through the service: filtered
// ones are answered FILTERED, not stored and counted per reason.
func TestFilteredEventsAreCounted(t *testing.T) {
	e := ingesttest.New(t, ingesttest.Options{})
	ctx := context.Background()
	settings := store.DefaultProjectSettings()
	settings.Filters = &apiv1.InboundFilters{WebCrawlers: true, Releases: []string{"*-dev"}}
	err := e.DB.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := store.UpdateProject(ctx, tx, e.Project.ID, e.Project.Name, e.Project.Platform, settings)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	e.Keys.InvalidateAll()
	client := ingestv1connect.NewIngestServiceClient(http.DefaultClient, e.Server.URL)
	send := func(ev *ingestv1.Event) ingestv1.EventOutcome {
		req := connect.NewRequest(&ingestv1.SendEventRequest{Event: ev})
		req.Header().Set("Authorization", "Bearer "+e.Key)
		res, err := client.SendEvent(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg.GetResult().GetOutcome()
	}
	bot := browserEvent("Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", "", app)
	dev := browserEvent(chrome129, "", app)
	dev.Release = "5.2.0-dev"
	ok := browserEvent(chrome129, "", app)
	for ev, want := range map[*ingestv1.Event]ingestv1.EventOutcome{
		bot: ingestv1.EventOutcome_EVENT_OUTCOME_FILTERED,
		dev: ingestv1.EventOutcome_EVENT_OUTCOME_FILTERED,
		ok:  ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED,
	} {
		if got := send(ev); got != want {
			t.Errorf("%s: %v, want %v", ev.GetRelease(), got, want)
		}
	}
	e.Service.FlushUsage(ctx)
	var stored, crawlers, releases int
	e.Scalar(t, &stored, `SELECT count(*) FROM events`)
	e.Scalar(t, &crawlers, `SELECT coalesce(sum(count), 0) FROM project_filtered_daily WHERE reason = $1`, int16(ingest.FilterWebCrawler))
	e.Scalar(t, &releases, `SELECT coalesce(sum(count), 0) FROM project_filtered_daily WHERE reason = $1`, int16(ingest.FilterRelease))
	if stored != 1 || crawlers != 1 || releases != 1 {
		t.Fatalf("stored %d, crawlers %d, releases %d", stored, crawlers, releases)
	}
}
