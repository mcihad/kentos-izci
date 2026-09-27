package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/store"
)

// EmailConfig is what e-mail channels store.
type EmailConfig struct {
	Recipients []string `json:"recipients"`
}

// MaxEmailRecipients bounds the recipients of one channel.
const MaxEmailRecipients = 20

// Email sends notifications through an SMTP server.
type Email struct {
	cfg    config.SMTPConfig
	from   *mail.Address
	domain string
	helo   string
}

// NewEmail creates the e-mail sender.
func NewEmail(cfg config.SMTPConfig) (*Email, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("SMTP_FROM: %w", err)
	}
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	helo, _ := os.Hostname()
	if helo == "" || !strings.Contains(helo, ".") {
		helo = domain
	}
	return &Email{cfg: cfg, from: from, domain: domain, helo: helo}, nil
}

// From is the sender address shown in the UI.
func (e *Email) From() string { return e.from.Address }

// Send delivers a notification by e-mail. Later notifications of an issue
// reply to the first one, so mail clients show them as one conversation.
func (e *Email) Send(ctx context.Context, n *Notification) (*Receipt, error) {
	var cfg EmailConfig
	if err := json.Unmarshal(n.Channel.Config, &cfg); err != nil || len(cfg.Recipients) == 0 {
		return nil, Permanent("the channel has no recipients")
	}
	msgID := "<" + n.Item.ID.String() + "@" + e.domain + ">"
	root := ""
	if n.Thread != nil {
		root, _ = n.Thread.Payload["message_id"].(string)
	}
	msg, err := e.Message(n, cfg.Recipients, msgID, root, time.Now())
	if err != nil {
		return nil, Permanent("cannot build the message: %v", err)
	}
	if err := e.deliver(ctx, cfg.Recipients, msg); err != nil {
		return nil, classifySMTP(err)
	}
	if n.Issue == nil {
		return nil, nil
	}
	if root == "" {
		root = msgID
	}
	return &Receipt{Thread: &store.Thread{
		ChannelID: n.Channel.ID, IssueID: uuid.MustParse(n.Issue.ID), LastSentAt: time.Now(), LastCount: n.Issue.TimesSeen,
		Kind: n.Item.Kind, Payload: map[string]any{"message_id": root},
	}}, nil
}

// classifySMTP: 4xx replies are temporary, 5xx permanent (a rejected
// recipient or wrong credentials); network errors are retried.
func classifySMTP(err error) error {
	var te *textproto.Error
	if errors.As(err, &te) {
		reason := fmt.Sprintf("SMTP server answered %d %s", te.Code, te.Msg)
		if te.Code >= 500 {
			return &PermanentError{Reason: reason}
		}
		return &RetryError{After: 5 * time.Minute, Reason: reason}
	}
	var pe *PermanentError
	if errors.As(err, &pe) {
		return err
	}
	return fmt.Errorf("SMTP: %w", err)
}

func (e *Email) deliver(ctx context.Context, to []string, msg []byte) error {
	addr := net.JoinHostPort(e.cfg.Host, strconv.Itoa(e.cfg.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: e.cfg.Host, MinVersion: tls.VersionTLS12}
	if e.cfg.TLS == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Minute)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, e.cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello(e.helo); err != nil {
		return err
	}
	if e.cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return Permanent("the SMTP server does not offer STARTTLS; use SMTP_TLS=tls or, for a local relay, SMTP_TLS=none")
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if e.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(smtp.PlainAuth("", e.cfg.Username, e.cfg.Password.Value(), e.cfg.Host)); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(e.from.Address); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Message renders a notification as a MIME message with a plain text and
// an HTML part.
func (e *Email) Message(n *Notification, to []string, msgID, inReplyTo string, now time.Time) ([]byte, error) {
	text, htmlBody, err := renderEmail(n)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	from := mail.Address{Name: e.from.Name, Address: e.from.Address}
	if from.Name == "" {
		from.Name = "İzci"
	}
	header("From", from.String())
	header("To", strings.Join(to, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", Subject(n)))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", msgID)
	if inReplyTo != "" {
		header("In-Reply-To", inReplyTo)
		header("References", inReplyTo)
	}
	header("MIME-Version", "1.0")
	header("Auto-Submitted", "auto-generated")
	if n.Issue != nil {
		header("X-Izci-Issue", n.Issue.ShortID)
	}
	mw := multipart.NewWriter(&buf)
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	buf.WriteString("\r\n")
	for _, part := range []struct{ typ, body string }{{"text/plain", text}, {"text/html", htmlBody}} {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderEmail(n *Notification) (string, string, error) {
	if n.Test() {
		text := testText(n.Channel.Name, n.OrgName)
		var h bytes.Buffer
		err := emailTest.Execute(&h, map[string]string{"Text": text})
		return text + "\r\n", h.String(), err
	}
	v := n.Issue
	var t strings.Builder
	fmt.Fprintf(&t, "%s: %s\r\n", Headline(n.Item.Kind, n.Item.Payload), v.ShortID)
	if s := StatusText(v); s != "" {
		fmt.Fprintf(&t, "%s\r\n", s)
	}
	fmt.Fprintf(&t, "\r\n%s\r\n", Truncate(Summary(v), 2000))
	if loc := Location(v); loc != "" {
		fmt.Fprintf(&t, "Konum: %s\r\n", loc)
	}
	t.WriteString("\r\n")
	for _, f := range Facts(v) {
		fmt.Fprintf(&t, "%s: %s\r\n", f.Label, f.Value)
	}
	fmt.Fprintf(&t, "%s\r\n\r\nHatayı aç: %s\r\n", Stats(v), n.Link)
	var h bytes.Buffer
	err := emailIssue.Execute(&h, map[string]any{
		"Headline": Headline(n.Item.Kind, n.Item.Payload), "ShortID": v.ShortID, "Status": StatusText(v),
		"Title": Truncate(firstNonEmpty(v.Type, v.Title), 300), "Value": Truncate(valueOf(v), 2000), "Location": Location(v),
		"Facts": Facts(v), "Stats": Stats(v), "Link": n.Link, "Color": levelColor(v.Level),
	})
	return t.String(), h.String(), err
}

func valueOf(v *IssueView) string {
	if v.Type != "" {
		return v.Value
	}
	return ""
}

func levelColor(level int16) string {
	switch level {
	case 1, 2:
		return "#2563eb"
	case 3:
		return "#d97706"
	case 5:
		return "#9333ea"
	}
	return "#dc2626"
}

var emailIssue = template.Must(template.New("issue").Parse(`<!doctype html>
<html lang="tr"><body style="margin:0;padding:24px;background:#f4f5f7;font-family:-apple-system,'Segoe UI',Roboto,Arial,sans-serif;color:#1f2328">
<table role="presentation" width="100%" style="max-width:640px;margin:0 auto;background:#ffffff;border:1px solid #d8dde3;border-radius:8px;border-collapse:separate">
<tr><td style="padding:20px 24px;border-top:4px solid {{.Color}};border-radius:8px 8px 0 0">
<p style="margin:0;font-size:13px;color:#57606a">{{.Headline}}: <a href="{{.Link}}" style="color:#0969da">{{.ShortID}}</a>{{if .Status}}, {{.Status}}{{end}}</p>
<h1 style="margin:8px 0 0;font-size:18px;line-height:1.35">{{.Title}}</h1>
{{if .Value}}<p style="margin:8px 0 0;font-size:15px;line-height:1.5;white-space:pre-wrap">{{.Value}}</p>{{end}}
{{if .Location}}<p style="margin:12px 0 0;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px;color:#57606a">{{.Location}}</p>{{end}}
</td></tr>
{{if .Facts}}<tr><td style="padding:0 24px"><table role="presentation" style="border-collapse:collapse;font-size:14px">
{{range .Facts}}<tr><td style="padding:3px 16px 3px 0;color:#57606a">{{.Label}}</td><td style="padding:3px 0">{{.Value}}</td></tr>{{end}}
</table></td></tr>{{end}}
<tr><td style="padding:16px 24px 20px">
<p style="margin:0 0 16px;font-size:13px;color:#57606a">{{.Stats}}</p>
<a href="{{.Link}}" style="display:inline-block;padding:9px 16px;background:#1f6feb;color:#ffffff;text-decoration:none;border-radius:6px;font-size:14px;font-weight:600">Hatayı aç</a>
</td></tr></table>
<p style="max-width:640px;margin:12px auto 0;font-size:12px;color:#8c959f">Bu e-postayı İzci'deki bir uyarı kuralı gönderdi. Bildirimleri İzci'de Uyarılar sayfasından yönetebilirsiniz.</p>
</body></html>`))

var emailTest = template.Must(template.New("test").Parse(`<!doctype html>
<html lang="tr"><body style="margin:0;padding:24px;background:#f4f5f7;font-family:-apple-system,'Segoe UI',Roboto,Arial,sans-serif;color:#1f2328">
<table role="presentation" width="100%" style="max-width:640px;margin:0 auto;background:#ffffff;border:1px solid #d8dde3;border-radius:8px">
<tr><td style="padding:20px 24px"><h1 style="margin:0;font-size:18px">Test bildirimi</h1>
<p style="margin:8px 0 0;font-size:15px;line-height:1.5">{{.Text}}</p></td></tr></table>
</body></html>`))
