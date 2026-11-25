package inbox

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"strings"

	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

// line is one notification as people read it.
type line struct {
	Sentence string
	Title    string
	Quote    string
	Link     string
}

func (s *Service) link(e *store.InboxEntry) string {
	return s.publicURL + "/o/" + e.OrgSlug + "/issues/" + ingest.ShortID(e.ProjectSlug, e.IssueSeq)
}

// describe says what happened, in Turkish.
func describe(e *store.InboxEntry) line {
	short := ingest.ShortID(e.ProjectSlug, e.IssueSeq)
	actor := e.ActorName
	if actor == "" {
		actor = e.ActorEmail
	}
	l := line{Title: e.IssueTitle}
	switch e.Kind {
	case store.NotifyAssigned:
		switch {
		case actor != "":
			l.Sentence = fmt.Sprintf("%s size %s hatasını atadı.", actor, short)
		case e.Data["auto"] == "suspect_commit":
			l.Sentence = fmt.Sprintf("%s hatası, hataya yol açmış olabilecek commit'iniz nedeniyle size atandı.", short)
		default:
			l.Sentence = fmt.Sprintf("%s hatası sahiplik kuralıyla size atandı.", short)
			if rule := e.Data["rule"]; rule != "" {
				l.Sentence = fmt.Sprintf("%s hatası sahiplik kuralıyla (%s) size atandı.", short, rule)
			}
		}
	case store.NotifyMentioned:
		l.Sentence = fmt.Sprintf("%s sizi %s hatasının yorumunda andı.", actor, short)
		l.Quote = e.Data["text"]
	case store.NotifyRegressed:
		l.Sentence = fmt.Sprintf("Takip ettiğiniz %s hatası çözüldükten sonra yeniden görüldü.", short)
	case store.NotifyCommented:
		l.Sentence = fmt.Sprintf("%s, takip ettiğiniz %s hatasına yorum yazdı.", actor, short)
		l.Quote = e.Data["text"]
	default:
		l.Sentence = short
	}
	return l
}

type message struct{ subject, text, html string }

// render builds one e-mail for a member's notifications.
func (s *Service) render(list []*store.InboxEntry) message {
	lines := make([]line, len(list))
	for i, e := range list {
		lines[i] = describe(e)
		lines[i].Link = s.link(e)
	}
	subject := fmt.Sprintf("[İzci] %d yeni bildirim", len(list))
	if len(list) == 1 {
		subject = "[İzci] " + strings.TrimSuffix(lines[0].Sentence, ".")
	}
	settings := s.publicURL + "/o/" + list[0].OrgSlug + "/account?tab=notifications"
	var t strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&t, "%s\r\n%s\r\n", l.Sentence, l.Title)
		if l.Quote != "" {
			fmt.Fprintf(&t, "“%s”\r\n", l.Quote)
		}
		fmt.Fprintf(&t, "%s\r\n\r\n", l.Link)
	}
	fmt.Fprintf(&t, "Bildirim tercihleriniz: %s\r\n", settings)
	var h bytes.Buffer
	_ = mailTemplate.Execute(&h, map[string]any{"Lines": lines, "Settings": settings})
	return message{subject: subject, text: t.String(), html: h.String()}
}

// renderTelegram builds one Telegram message (HTML) for a member's
// notifications.
func (s *Service) renderTelegram(list []*store.InboxEntry) string {
	var b strings.Builder
	for i, e := range list {
		if i > 0 {
			b.WriteString("\n\n")
		}
		l := describe(e)
		fmt.Fprintf(&b, "%s\n<b>%s</b>", html.EscapeString(l.Sentence), html.EscapeString(l.Title))
		if l.Quote != "" {
			fmt.Fprintf(&b, "\n<i>%s</i>", html.EscapeString(l.Quote))
		}
		fmt.Fprintf(&b, "\n<a href=\"%s\">Hatayı aç</a>", html.EscapeString(s.link(e)))
	}
	return b.String()
}

var mailTemplate = template.Must(template.New("inbox").Parse(`<!doctype html>
<html lang="tr"><body style="margin:0;padding:24px;background:#f6f7f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#1f2328">
<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e3e6ea;border-radius:8px;padding:24px">
{{range .Lines}}<div style="margin:0 0 20px">
<p style="margin:0 0 4px;font-size:14px;line-height:1.5">{{.Sentence}}</p>
<p style="margin:0 0 6px;font-size:15px;font-weight:600;line-height:1.4">{{.Title}}</p>
{{if .Quote}}<p style="margin:0 0 6px;padding:6px 10px;border-left:3px solid #d0d7de;color:#59636e;font-size:13px;line-height:1.5">{{.Quote}}</p>{{end}}
<a href="{{.Link}}" style="font-size:13px;color:#1d4ed8">Hatayı aç</a>
</div>{{end}}
<p style="margin:16px 0 0;font-size:12px;color:#59636e">Hangi bildirimlerin e-postayla geleceğini <a href="{{.Settings}}" style="color:#59636e">bildirim tercihlerinizden</a> seçebilirsiniz.</p>
</div></body></html>
`))
