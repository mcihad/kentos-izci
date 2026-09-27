package notify

import (
	"fmt"
	"path"
	"strings"

	"github.com/mcihad/kentos-izci/internal/store"
)

// Headline is the first line of a notification per outbox kind.
func Headline(kind int16, payload map[string]any) string {
	switch kind {
	case store.NotifyRegression:
		return "Çözülmüş hata geri döndü"
	case store.NotifyFrequency:
		n, _ := payload["threshold"].(float64)
		w, _ := payload["window_seconds"].(float64)
		return fmt.Sprintf("Hata sıklaştı: %s içinde %d olay", Window(int(w)), int(n))
	case store.NotifyTest:
		return "Test bildirimi"
	default:
		return "Yeni hata"
	}
}

// Window names a frequency window in Turkish ("15 dakika", "1 saat").
func Window(seconds int) string {
	switch {
	case seconds > 0 && seconds%86400 == 0:
		return fmt.Sprintf("%d gün", seconds/86400)
	case seconds > 0 && seconds%3600 == 0:
		return fmt.Sprintf("%d saat", seconds/3600)
	default:
		return fmt.Sprintf("%d dakika", max(1, seconds/60))
	}
}

// LevelMark is the colored mark of a level, as in Telegram messages.
func LevelMark(level int16) string {
	switch level {
	case 1:
		return "⚪"
	case 2:
		return "🔵"
	case 3:
		return "🟠"
	case 5:
		return "🟣"
	}
	return "🔴"
}

// LevelName is the level in English for machine-readable payloads.
func LevelName(level int16) string {
	switch level {
	case 1:
		return "debug"
	case 2:
		return "info"
	case 3:
		return "warning"
	case 5:
		return "fatal"
	}
	return "error"
}

// StatusName is the status in English for machine-readable payloads.
func StatusName(status int16) string {
	switch status {
	case store.IssueResolved:
		return "resolved"
	case store.IssueIgnored:
		return "ignored"
	}
	return "unresolved"
}

// StatusText describes a triaged issue ("Çözüldü (Ayşe Yılmaz)"); empty
// for unresolved ones.
func StatusText(v *IssueView) string {
	var s string
	switch v.Status {
	case store.IssueResolved:
		s = "Çözüldü"
	case store.IssueIgnored:
		s = "Yoksayıldı"
	default:
		return ""
	}
	if v.StatusBy != "" {
		s += " (" + v.StatusBy + ")"
	}
	return s
}

// Summary is the error in one line: "type: value", or the title.
func Summary(v *IssueView) string {
	if v.Type != "" && v.Value != "" {
		return v.Type + ": " + v.Value
	}
	if v.Type != "" {
		return v.Type
	}
	return v.Title
}

// Location is where the error happened: the culprit with the line
// ("OrderService.Create in OrderService.cs:42"), or the file and line.
func Location(v *IssueView) string {
	loc := v.Filename
	if loc != "" && v.Lineno > 0 {
		loc = fmt.Sprintf("%s:%d", loc, v.Lineno)
	}
	switch {
	case v.Culprit == "":
		return loc
	case v.Filename != "" && strings.HasSuffix(v.Culprit, path.Base(v.Filename)):
		if v.Lineno > 0 {
			return fmt.Sprintf("%s:%d", v.Culprit, v.Lineno)
		}
		return v.Culprit
	case loc != "":
		return v.Culprit + " (" + loc + ")"
	}
	return v.Culprit
}

// Fact is a labeled value of a notification.
type Fact struct{ Label, Value string }

// Facts are the context lines of an issue notification, empty ones left
// out.
func Facts(v *IssueView) []Fact {
	var out []Fact
	for _, f := range []Fact{
		{"Proje", v.ProjectName}, {"Müşteri", v.Customer}, {"Kiracı", v.Tenant}, {"Ortam", v.Environment},
		{"Sürüm", v.Release}, {"Kullanıcı", v.User},
	} {
		if f.Value != "" {
			out = append(out, f)
		}
	}
	return out
}

// Stats counts what an issue affected: "12 olay, 3 kullanıcı, 1 müşteri".
func Stats(v *IssueView) string {
	s := fmt.Sprintf("%d olay, %d kullanıcı, %d müşteri", v.TimesSeen, v.Users, v.Customers)
	if v.Unhandled {
		s += ", işlenmemiş istisna"
	}
	return s
}

// Subject is the one-line title of a notification (e-mail subject).
func Subject(n *Notification) string {
	if n.Test() {
		return "[İzci] Test bildirimi"
	}
	return fmt.Sprintf("[İzci] %s: %s %s", Headline(n.Item.Kind, n.Item.Payload), n.Issue.ShortID, Truncate(Summary(n.Issue), 140))
}

// Truncate shortens s to n characters with an ellipsis.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// testText is the body of a test notification.
func testText(channelName, orgName string) string {
	return fmt.Sprintf("İzci, %s kanalına %s organizasyonunun bildirimlerini gönderebiliyor.", channelName, orgName)
}
