package api_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/digest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// TestDigest sends the daily summary of a member's organizations once.
func TestDigest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := setupKentos(t, e)
	sendEvent(t, e, "kalem", "a", func(ev *ingestv1.Event) {})
	sendEvent(t, e, "kalem", "a", func(ev *ingestv1.Event) {})
	box := &mailbox{}
	d := digest.New(e.db, box, "http://izci.test", slog.New(slog.DiscardHandler))
	// The next morning: the period covers the events just sent.
	now := time.Now()
	_, slot, _ := d.Slot(apiv1.DigestFrequency_DIGEST_FREQUENCY_DAILY, now)
	d.Now = func() time.Time { return slot.Add(24*time.Hour + time.Minute) }

	if err := d.Run(ctx); err != nil || len(box.sent) != 0 {
		t.Fatalf("digest without the preference: %d %v", len(box.sent), err)
	}
	settings, err := owner.user.GetNotificationSettings(ctx, req(&apiv1.GetNotificationSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Msg.GetSettings()
	s.Digest = apiv1.DigestFrequency_DIGEST_FREQUENCY_DAILY
	if _, err := owner.user.UpdateNotificationSettings(ctx, req(&apiv1.UpdateNotificationSettingsRequest{Settings: s})); err != nil {
		t.Fatal(err)
	}
	if err := d.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 1 {
		t.Fatalf("digests sent: %d", len(box.sent))
	}
	m := box.sent[0]
	if m.to != "owner@example.com" || !strings.Contains(m.subject, "günlük özeti") || !strings.Contains(m.subject, "1 yeni hata") ||
		!strings.Contains(m.text, "KALEM-1") || !strings.Contains(m.text, "2 olay") || !strings.Contains(m.text, "http://izci.test/o/kentos/issues/KALEM-1") {
		t.Fatalf("digest: %+v", m)
	}
	if err := d.Run(ctx); err != nil || len(box.sent) != 1 {
		t.Fatalf("the same digest twice: %d %v", len(box.sent), err)
	}
	// Weekly digests go out on Monday mornings.
	monday := time.Date(2026, 11, 23, 9, 0, 0, 0, time.FixedZone("TRT", 3*3600))
	start, slot, ok := d.Slot(apiv1.DigestFrequency_DIGEST_FREQUENCY_WEEKLY, monday.Add(50*time.Hour))
	if !ok || !slot.Equal(monday.Add(-time.Hour)) || !start.Equal(slot.AddDate(0, 0, -7)) {
		t.Fatalf("weekly slot: %v %v", start, slot)
	}
}
