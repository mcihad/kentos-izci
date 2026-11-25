// Package accountmail writes the e-mails of account flows: sign-up links,
// invitations, password resets and security notices (a sign-in from a new
// device, a changed password, two-factor authentication turned off, a new
// access token). Messages carry nothing an outsider typed beyond what the
// recipient needs to recognize, so they cannot be used to send arbitrary
// text to other people's addresses.
package accountmail

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/netip"
	"strings"
	"time"
)

// Mailer sends e-mail (notify.Email).
type Mailer interface {
	SendMail(ctx context.Context, to, subject, text, htmlBody string) error
}

// Sender writes and sends account e-mails. A nil Sender, or one without a
// mailer, sends nothing.
type Sender struct {
	mailer    Mailer
	publicURL string
	log       *slog.Logger
	loc       *time.Location
}

// New creates the sender; mailer is nil when the server has no SMTP.
func New(mailer Mailer, publicURL string, log *slog.Logger) *Sender {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		loc = time.UTC
	}
	return &Sender{mailer: mailer, publicURL: strings.TrimRight(publicURL, "/"), log: log, loc: loc}
}

// Available reports whether the server sends e-mail.
func (s *Sender) Available() bool { return s != nil && s.mailer != nil }

// message is an e-mail: paragraphs, an optional button and a footer.
type message struct {
	Subject    string
	Paragraphs []string
	Button     string
	Link       string
	Footer     string
}

func (s *Sender) send(ctx context.Context, to string, m message) error {
	if !s.Available() {
		return nil
	}
	var t strings.Builder
	t.WriteString("Merhaba,\r\n\r\n")
	for _, p := range m.Paragraphs {
		t.WriteString(p + "\r\n\r\n")
	}
	if m.Link != "" {
		t.WriteString(m.Button + ": " + m.Link + "\r\n\r\n")
	}
	if m.Footer != "" {
		t.WriteString(m.Footer + "\r\n")
	}
	var h bytes.Buffer
	if err := layout.Execute(&h, m); err != nil {
		return err
	}
	return s.mailer.SendMail(ctx, to, m.Subject, t.String(), h.String())
}

// notice sends a security notice without making the caller wait; failures
// are logged.
func (s *Sender) notice(ctx context.Context, to string, m message) {
	if !s.Available() {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	go func() {
		defer cancel()
		if err := s.send(ctx, to, m); err != nil {
			s.log.Warn("security notice not sent", slog.String("subject", m.Subject), slog.Any("err", err))
		}
	}()
}

func (s *Sender) when(t time.Time) string { return t.In(s.loc).Format("02.01.2006 15:04") }

// account links to a page of the account (sessions, tokens) in whichever
// organization the member opens.
func (s *Sender) account(tab string) string { return s.publicURL + "/account?tab=" + tab }

// SignupLink sends the link that confirms a self sign-up.
func (s *Sender) SignupLink(ctx context.Context, to, link string) error {
	return s.send(ctx, to, message{
		Subject: "İzci hesabınızı etkinleştirin",
		Paragraphs: []string{to + " adresiyle İzci'de hesap açmak istediniz. Hesabınızı ve organizasyonunuzu oluşturmak için " +
			"bağlantıyı açın."},
		Button: "Hesabımı etkinleştir", Link: link,
		Footer: "Bağlantı 24 saat geçerlidir ve bir kez kullanılır. Bu isteği siz yapmadıysanız e-postayı yok sayın; hesap açılmaz.",
	})
}

// Invitation sends an invitation link.
func (s *Sender) Invitation(ctx context.Context, to, inviter, org, role, link string, expires time.Time) error {
	return s.send(ctx, to, message{
		Subject:    fmt.Sprintf("İzci: %s organizasyonuna davet edildiniz", org),
		Paragraphs: []string{fmt.Sprintf("%s, %s organizasyonunun hatalarını İzci'de izlemek için sizi %s olarak davet etti.", inviter, org, role)},
		Button:     "Daveti kabul et", Link: link,
		Footer: fmt.Sprintf("Bağlantı %s tarihine kadar geçerlidir. Bu daveti beklemiyorsanız e-postayı yok sayın.", s.when(expires)),
	})
}

// PasswordReset sends the link that sets a new password.
func (s *Sender) PasswordReset(ctx context.Context, to, link string) error {
	return s.send(ctx, to, message{
		Subject:    "İzci parolanızı sıfırlayın",
		Paragraphs: []string{"İzci hesabınız için yeni parola belirleme istendi."},
		Button:     "Yeni parola belirle", Link: link,
		Footer: "Bağlantı 1 saat geçerlidir ve bir kez kullanılır. Bu isteği siz yapmadıysanız e-postayı yok sayın; parolanız değişmez.",
	})
}

// NewSignIn tells a member about a sign-in from a device they had not used.
func (s *Sender) NewSignIn(ctx context.Context, to, device string, ip netip.Addr, at time.Time) {
	where := ""
	if ip.IsValid() {
		where = " (IP " + ip.String() + ")"
	}
	s.notice(ctx, to, message{
		Subject: "İzci hesabınıza yeni bir cihazdan giriş yapıldı",
		Paragraphs: []string{
			fmt.Sprintf("Hesabınıza %s saatinde %s ile giriş yapıldı%s.", s.when(at), device, where),
			"Bu siz değilseniz parolanızı değiştirin ve tanımadığınız oturumları kapatın.",
		},
		Button: "Oturumları gör", Link: s.account("sessions"),
	})
}

// PasswordChanged tells a member their password changed.
func (s *Sender) PasswordChanged(ctx context.Context, to string, at time.Time) {
	s.notice(ctx, to, message{
		Subject: "İzci parolanız değiştirildi",
		Paragraphs: []string{
			fmt.Sprintf("Hesabınızın parolası %s saatinde değiştirildi; diğer cihazlardaki oturumlar kapatıldı.", s.when(at)),
			"Bu değişikliği siz yapmadıysanız hemen sunucu yöneticinize başvurun.",
		},
	})
}

// TwoFactorDisabled tells a member their two-factor authentication was
// turned off, by themselves or by an administrator.
func (s *Sender) TwoFactorDisabled(ctx context.Context, to string, byAdmin bool, at time.Time) {
	who := "Hesabınızda"
	if byAdmin {
		who = "Bir sunucu yöneticisi hesabınızda"
	}
	s.notice(ctx, to, message{
		Subject: "İzci hesabınızda iki adımlı doğrulama kapatıldı",
		Paragraphs: []string{
			fmt.Sprintf("%s iki adımlı doğrulama %s saatinde kapatıldı. Hesabınız artık yalnız parolayla açılabilir.", who, s.when(at)),
			"Bu değişikliği siz istemediyseniz parolanızı değiştirin ve iki adımlı doğrulamayı yeniden açın.",
		},
		Button: "İki adımlı doğrulama", Link: s.account("two-factor"),
	})
}

// TokenCreated tells a member about a new personal access token.
func (s *Sender) TokenCreated(ctx context.Context, to, name, scopes string, expires *time.Time) {
	until := "süresiz"
	if expires != nil {
		until = s.when(*expires) + " tarihine kadar geçerli"
	}
	s.notice(ctx, to, message{
		Subject: "İzci hesabınızda yeni bir erişim anahtarı oluşturuldu",
		Paragraphs: []string{
			fmt.Sprintf("Hesabınızda “%s” adlı yeni bir erişim anahtarı oluşturuldu: %s yetkili, %s.", name, scopes, until),
			"Anahtarı siz oluşturmadıysanız iptal edin ve parolanızı değiştirin.",
		},
		Button: "Erişim anahtarları", Link: s.account("tokens"),
	})
}

var layout = template.Must(template.New("account").Parse(`<!doctype html>
<html lang="tr"><body style="margin:0;padding:24px;background:#f6f7f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#1f2328">
<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e3e6ea;border-radius:8px;padding:28px">
<p style="margin:0 0 16px;font-size:15px;line-height:1.5">Merhaba,</p>
{{range .Paragraphs}}<p style="margin:0 0 16px;font-size:15px;line-height:1.5">{{.}}</p>
{{end}}{{if .Link}}<p style="margin:24px 0"><a href="{{.Link}}" style="display:inline-block;background:#1d4ed8;color:#ffffff;text-decoration:none;padding:10px 18px;border-radius:6px;font-weight:600;font-size:15px">{{.Button}}</a></p>
<p style="margin:0 0 8px;font-size:12px;line-height:1.5;color:#59636e;word-break:break-all">{{.Link}}</p>
{{end}}{{if .Footer}}<p style="margin:8px 0 0;font-size:13px;line-height:1.5;color:#59636e">{{.Footer}}</p>
{{end}}</div></body></html>
`))
