package telegram

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/mcihad/kentos-izci/internal/store"
)

// IssueView is everything a notification shows about an issue.
type IssueView struct {
	ID          string
	ShortID     string
	OrgSlug     string
	ProjectName string
	Title       string
	Type        string
	Value       string
	Culprit     string
	Filename    string
	Lineno      uint32
	Level       int16
	Status      int16
	Unhandled   bool
	TimesSeen   int64
	Users       int32
	Customers   int32
	// From the event that triggered the notification.
	Customer    string
	Tenant      string
	Environment string
	Release     string
	User        string
	// StatusBy names who resolved or ignored the issue.
	StatusBy string
}

var levelMarks = map[int16]string{1: "⚪", 2: "🔵", 3: "🟠", 4: "🔴", 5: "🟣"}

// headline is the first line of a notification per outbox kind.
func headline(kind int16, payload map[string]any) string {
	switch kind {
	case store.NotifyRegression:
		return "Çözülmüş hata geri döndü"
	case store.NotifyFrequency:
		n, _ := payload["threshold"].(float64)
		w, _ := payload["window_seconds"].(float64)
		return fmt.Sprintf("Hata sıklaştı: %s içinde %d olay", window(int(w)), int(n))
	case store.NotifyTest:
		return "Test bildirimi"
	default:
		return "Yeni hata"
	}
}

func window(seconds int) string {
	switch {
	case seconds%86400 == 0:
		return fmt.Sprintf("%d gün", seconds/86400)
	case seconds%3600 == 0:
		return fmt.Sprintf("%d saat", seconds/3600)
	default:
		return fmt.Sprintf("%d dakika", max(1, seconds/60))
	}
}

// RenderIssue builds the HTML text of an issue notification.
func RenderIssue(kind int16, payload map[string]any, v IssueView, issueURL string) string {
	e := html.EscapeString
	var b strings.Builder
	mark := levelMarks[v.Level]
	if mark == "" {
		mark = "🔴"
	}
	fmt.Fprintf(&b, "%s <b>%s</b>  <a href=\"%s\">%s</a>\n", mark, e(headline(kind, payload)), e(issueURL), e(v.ShortID))
	switch v.Status {
	case store.IssueResolved:
		b.WriteString("✅ <b>Çözüldü</b>")
	case store.IssueIgnored:
		b.WriteString("🔕 <b>Yoksayıldı</b>")
	}
	if v.Status != store.IssueUnresolved {
		if v.StatusBy != "" {
			b.WriteString(" (" + e(v.StatusBy) + ")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if v.Type != "" {
		fmt.Fprintf(&b, "<b>%s</b>\n", e(v.Type))
		if v.Value != "" {
			fmt.Fprintf(&b, "%s\n", e(truncate(v.Value, 600)))
		}
	} else {
		fmt.Fprintf(&b, "<b>%s</b>\n", e(truncate(v.Title, 600)))
	}
	if v.Culprit != "" || v.Filename != "" {
		b.WriteString("\n")
		if v.Culprit != "" {
			fmt.Fprintf(&b, "📍 <code>%s</code>\n", e(truncate(v.Culprit, 200)))
		}
		if v.Filename != "" {
			loc := v.Filename
			if v.Lineno > 0 {
				loc = fmt.Sprintf("%s:%d", loc, v.Lineno)
			}
			fmt.Fprintf(&b, "      <code>%s</code>\n", e(truncate(loc, 200)))
		}
	}
	b.WriteString("\n")
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, e(value))
		}
	}
	line("Proje", v.ProjectName)
	line("Müşteri", v.Customer)
	line("Kiracı", v.Tenant)
	line("Ortam", v.Environment)
	line("Sürüm", v.Release)
	line("Kullanıcı", v.User)
	fmt.Fprintf(&b, "\n%d olay, %d kullanıcı, %d müşteri", v.TimesSeen, v.Users, v.Customers)
	if v.Unhandled {
		b.WriteString(", işlenmemiş istisna")
	}
	return b.String()
}

// RenderTest is the text of a test notification.
func RenderTest(channelName, orgName string) string {
	return fmt.Sprintf("✅ <b>Test bildirimi</b>\n\nİzci, <b>%s</b> sohbetine %s organizasyonunun bildirimlerini gönderebiliyor.",
		html.EscapeString(channelName), html.EscapeString(orgName))
}

// Keyboard returns the buttons under an issue notification. Link buttons
// are only added for public addresses, which Telegram accepts.
func Keyboard(v IssueView, issueURL string, sign func(action, issueID string) string) *InlineKeyboard {
	var row []Button
	if public(issueURL) {
		row = append(row, Button{Text: "Aç", URL: issueURL})
	}
	if v.Status == store.IssueUnresolved {
		row = append(row,
			Button{Text: "✅ Çöz", CallbackData: sign(actionResolve, v.ID)},
			Button{Text: "🔕 Yoksay", CallbackData: sign(actionIgnore, v.ID)})
	} else {
		row = append(row, Button{Text: "↩️ Yeniden aç", CallbackData: sign(actionReopen, v.ID)})
	}
	return &InlineKeyboard{Rows: [][]Button{row}}
}

func public(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	host := u.Hostname()
	return host != "localhost" && host != "127.0.0.1" && host != "::1" && !strings.HasSuffix(host, ".localhost") && strings.Contains(host, ".")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
