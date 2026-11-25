package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/version"
)

// ErrPrivateAddress refuses connections to addresses inside the server's
// network unless NOTIFY_ALLOW_PRIVATE_NETWORKS is set: organization admins
// must not be able to make izci call internal services.
var ErrPrivateAddress = errors.New("the destination resolves to a private, loopback or link-local address; " +
	"set NOTIFY_ALLOW_PRIVATE_NETWORKS=true to allow it")

// NewHTTPClient returns the client outgoing notifications use. Without
// allowPrivate, connections to loopback, private, link-local, multicast and
// unspecified addresses are refused when dialing, after DNS resolution.
func NewHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !PublicAddr(ip) {
				return ErrPrivateAddress
			}
			return nil
		}
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// PublicAddr reports whether ip is a public unicast address.
func PublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified() && !ip.IsInterfaceLocalMulticast() &&
		!netip.MustParsePrefix("100.64.0.0/10").Contains(ip) // carrier-grade NAT
}

// ValidateURL checks a destination URL given by a user: absolute, http(s)
// (https only when httpsOnly), without credentials.
func ValidateURL(raw string, httpsOnly bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, errors.New("not an absolute URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && !httpsOnly:
	default:
		if httpsOnly {
			return nil, errors.New("the URL must start with https://")
		}
		return nil, errors.New("the URL must start with http:// or https://")
	}
	if u.User != nil {
		return nil, errors.New("the URL must not contain a user name or password")
	}
	return u, nil
}

// URLHint shows where a secret URL points without revealing it:
// "hooks.slack.com/…/x7Qa".
func URLHint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	tail := strings.TrimRight(u.Path, "/")
	if i := strings.LastIndexByte(tail, '/'); i >= 0 {
		tail = tail[i+1:]
	}
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	if tail == "" {
		return u.Host
	}
	return u.Host + "/…/" + tail
}

// sealAD binds sealed channel secrets to their channel.
func sealAD(channelID uuid.UUID) []byte { return []byte("izci/channel/" + channelID.String()) }

// Seal encrypts a channel secret for storage in its settings.
func Seal(box *secret.Box, channelID uuid.UUID, plaintext string) string {
	return base64.StdEncoding.EncodeToString(box.Seal([]byte(plaintext), sealAD(channelID)))
}

// Open decrypts a channel secret sealed with Seal.
func Open(box *secret.Box, channelID uuid.UUID, sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", secret.ErrDecrypt
	}
	b, err := box.Open(raw, sealAD(channelID))
	return string(b), err
}

// Reseal makes a channel secret one of the current master key; it reports
// whether it had to be sealed again (izci secrets rotate).
func Reseal(box *secret.Box, channelID uuid.UUID, sealed string) (string, bool, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", false, secret.ErrDecrypt
	}
	out, changed, err := box.Reseal(raw, sealAD(channelID))
	if err != nil || !changed {
		return sealed, false, err
	}
	return base64.StdEncoding.EncodeToString(out), true, nil
}

// userAgent identifies izci's outgoing requests.
func userAgent() string { return "izci/" + version.Get().Version }

// post sends a JSON body and classifies the answer: 2xx succeeds, 429 and
// 5xx are retried (honoring Retry-After), other statuses give up.
func post(ctx context.Context, hc *http.Client, rawURL string, body []byte, header http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, Permanent("invalid destination: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())
	for k, v := range header {
		req.Header[k] = v
	}
	return do(hc, req)
}

// do runs a request and classifies the answer like post.
func do(hc *http.Client, req *http.Request) ([]byte, error) {
	res, err := hc.Do(req)
	if err != nil {
		if errors.Is(err, ErrPrivateAddress) {
			return nil, &PermanentError{Reason: ErrPrivateAddress.Error()}
		}
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Host, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return body, nil
	}
	reason := fmt.Sprintf("%s answered %d", req.URL.Host, res.StatusCode)
	if detail := strings.TrimSpace(string(body)); detail != "" && len(detail) < 300 && !strings.HasPrefix(detail, "<") {
		reason += ": " + detail
	}
	switch {
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 || res.StatusCode == http.StatusRequestTimeout:
		return nil, &RetryError{After: retryAfter(res.Header.Get("Retry-After")), Reason: reason}
	default:
		return nil, &PermanentError{Reason: reason}
	}
}

// retryAfter reads a Retry-After header (seconds or an HTTP date); one
// minute when absent.
func retryAfter(h string) time.Duration {
	if h == "" {
		return time.Minute
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n >= 0 {
		return min(time.Duration(n)*time.Second, time.Hour)
	}
	if t, err := http.ParseTime(h); err == nil {
		return min(max(time.Until(t), 0), time.Hour)
	}
	return time.Minute
}
