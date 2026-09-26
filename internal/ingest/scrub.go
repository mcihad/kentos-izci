package ingest

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
)

// Filtered replaces scrubbed values.
const Filtered = "[Filtered]"

// defaultSensitiveKeys are case-insensitive substrings that mark a key
// (header, query parameter, JSON field, variable, cookie) as sensitive.
var defaultSensitiveKeys = []string{
	"password", "passwd", "parola", "sifre", "şifre", "secret", "token", "apikey", "api_key", "api-key",
	"authorization", "credential", "private_key", "privatekey", "cookie", "session",
	"csrf", "xsrf", "card_number", "cardnumber", "kart_no", "cvv", "cvc", "iban",
	"tckn", "tc_kimlik", "kimlik_no", "ssn", "otp", "pin_code",
}

// sensitiveHeaders are always scrubbed, regardless of project settings.
var sensitiveHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true, "set-cookie": true,
	"x-api-key": true, "x-izci-key": true, "x-sentry-auth": true, "x-auth-token": true,
	"x-csrf-token": true, "x-xsrf-token": true, "x-amz-security-token": true,
}

// Scrubber removes sensitive data from events. The zero value is not
// usable; build one with NewScrubber.
type Scrubber struct {
	keys []string
	safe map[string]bool
}

// NewScrubber combines the built-in key list with project-specific extra
// keys; safe keys (exact, case-insensitive) are never scrubbed.
func NewScrubber(extra, safe []string) *Scrubber {
	s := &Scrubber{keys: slices.Clone(defaultSensitiveKeys), safe: map[string]bool{}}
	for _, k := range extra {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			s.keys = append(s.keys, k)
		}
	}
	for _, k := range safe {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			s.safe[k] = true
		}
	}
	return s
}

// SensitiveKey reports whether values stored under key must be scrubbed.
func (s *Scrubber) SensitiveKey(key string) bool {
	k := strings.ToLower(key)
	if s.safe[k] {
		return false
	}
	for _, pat := range s.keys {
		if strings.Contains(k, pat) {
			return true
		}
	}
	return false
}

// SensitiveHeader reports whether a header value must be scrubbed.
func (s *Scrubber) SensitiveHeader(name string) bool {
	n := strings.ToLower(name)
	return sensitiveHeaders[n] || s.SensitiveKey(n)
}

var (
	reBearer   = regexp.MustCompile(`(?i)\b(bearer|basic|token)\s+[A-Za-z0-9\-._~+/]{8,}=*`)
	reURLCreds = regexp.MustCompile(`(://[^:/@\s]+):[^@/\s]+@`)
	reKVSecret = regexp.MustCompile(`(?i)\b(password|passwd|pwd|parola|secret|token|api[_-]?key|access[_-]?token)(["']?\s*[:=]\s*["']?)([^&\s"',;]+)`)
	reIBAN     = regexp.MustCompile(`(?i)\bTR\d{2}(?:\s?\d{4}){5}\s?\d{2}\b`)
	reDigits   = regexp.MustCompile(`\d(?:[ -]?\d){10,18}`)
)

// Value masks secrets embedded in free text: authorization schemes, URL
// credentials, key=value secrets, IBANs, payment card numbers (Luhn) and
// Turkish identity numbers (TCKN checksum).
func (s *Scrubber) Value(v string) string {
	if len(v) < 8 {
		return v
	}
	lower := strings.ToLower(v)
	if strings.Contains(lower, "bearer") || strings.Contains(lower, "basic") || strings.Contains(lower, "token") {
		v = reBearer.ReplaceAllString(v, "$1 "+Filtered)
	}
	if strings.Contains(v, "://") && strings.Contains(v, "@") {
		v = reURLCreds.ReplaceAllString(v, "$1:"+Filtered+"@")
	}
	if strings.ContainsAny(v, "=:") {
		v = reKVSecret.ReplaceAllString(v, "${1}${2}"+Filtered)
	}
	if hasDigitRun(v, 11) {
		if strings.Contains(lower, "tr") {
			v = reIBAN.ReplaceAllString(v, Filtered)
		}
		v = reDigits.ReplaceAllStringFunc(v, func(m string) string {
			digits := stripSeparators(m)
			if (len(digits) >= 13 && luhn(digits)) || (len(digits) == 11 && validTCKN(digits)) {
				return Filtered
			}
			return m
		})
	}
	return v
}

// hasDigitRun reports whether s contains at least n digits in a row
// (ignoring single spaces or dashes between them).
func hasDigitRun(s string, n int) bool {
	run := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			run++
			if run >= n {
				return true
			}
		case (c == ' ' || c == '-') && run > 0:
		default:
			run = 0
		}
	}
	return false
}

func stripSeparators(s string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(s)
}

func luhn(digits string) bool {
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// validTCKN checks the Turkish national identity number algorithm.
func validTCKN(d string) bool {
	if len(d) != 11 || d[0] == '0' {
		return false
	}
	n := make([]int, 11)
	for i := range d {
		n[i] = int(d[i] - '0')
	}
	odd := n[0] + n[2] + n[4] + n[6] + n[8]
	even := n[1] + n[3] + n[5] + n[7]
	d10 := ((odd*7-even)%10 + 10) % 10
	if d10 != n[9] {
		return false
	}
	sum := 0
	for i := 0; i < 10; i++ {
		sum += n[i]
	}
	return sum%10 == n[10]
}

// Map scrubs a string map in place: sensitive keys lose their value, other
// values are pattern-scrubbed.
func (s *Scrubber) Map(m map[string]string) {
	for k, v := range m {
		if s.SensitiveKey(k) {
			m[k] = Filtered
		} else {
			m[k] = s.Value(v)
		}
	}
}

// Struct scrubs a protobuf Struct recursively.
func (s *Scrubber) Struct(st *structpb.Struct) {
	if st == nil {
		return
	}
	for k, v := range st.Fields {
		if s.SensitiveKey(k) {
			st.Fields[k] = structpb.NewStringValue(Filtered)
			continue
		}
		s.structValue(v)
	}
}

func (s *Scrubber) structValue(v *structpb.Value) {
	switch x := v.GetKind().(type) {
	case *structpb.Value_StringValue:
		x.StringValue = s.Value(x.StringValue)
	case *structpb.Value_StructValue:
		s.Struct(x.StructValue)
	case *structpb.Value_ListValue:
		for _, item := range x.ListValue.GetValues() {
			s.structValue(item)
		}
	}
}

// QueryString scrubs a raw query string, keeping parameter order.
func (s *Scrubber) QueryString(q string) string {
	if q == "" {
		return q
	}
	parts := strings.Split(q, "&")
	for i, p := range parts {
		k, v, hasValue := strings.Cut(p, "=")
		name, err := url.QueryUnescape(k)
		if err != nil {
			name = k
		}
		if hasValue && s.SensitiveKey(name) {
			parts[i] = k + "=" + url.QueryEscape(Filtered)
		} else if hasValue {
			parts[i] = k + "=" + s.Value(v)
		}
	}
	return strings.Join(parts, "&")
}

// Body scrubs a request body: JSON and form bodies field by field, anything
// else by value patterns.
func (s *Scrubber) Body(body, contentType string) string {
	trimmed := strings.TrimSpace(body)
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "json") || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
		var v any
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&v); err == nil {
			s.jsonValue(&v)
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(v); err == nil {
				return strings.TrimRight(buf.String(), "\n")
			}
		}
	case strings.Contains(ct, "x-www-form-urlencoded"):
		return s.QueryString(trimmed)
	}
	return s.Value(body)
}

func (s *Scrubber) jsonValue(v *any) {
	switch x := (*v).(type) {
	case map[string]any:
		for k, child := range x {
			if s.SensitiveKey(k) {
				x[k] = Filtered
				continue
			}
			s.jsonValue(&child)
			x[k] = child
		}
	case []any:
		for i := range x {
			s.jsonValue(&x[i])
		}
	case string:
		*v = s.Value(x)
	}
}
