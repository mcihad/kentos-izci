// Package slug turns display names into URL-safe identifiers.
package slug

import (
	"strings"
	"unicode"
)

// turkish maps Turkish letters (and a few common Latin ones) to ASCII so
// that "Sivas Belediyesi Şikâyet" becomes "sivas-belediyesi-sikayet".
var turkish = strings.NewReplacer(
	"ç", "c", "Ç", "c", "ğ", "g", "Ğ", "g", "ı", "i", "İ", "i",
	"ö", "o", "Ö", "o", "ş", "s", "Ş", "s", "ü", "u", "Ü", "u",
	"â", "a", "Â", "a", "î", "i", "Î", "i", "û", "u", "Û", "u",
)

// Transliterate replaces Turkish letters (and a few common Latin ones) with
// their ASCII counterparts, keeping everything else.
func Transliterate(s string) string { return turkish.Replace(s) }

// Make returns a slug of at most maxLen characters matching
// ^[a-z][a-z0-9-]*[a-z0-9]$, or "" when name has no usable characters.
// A leading digit is prefixed with "p-".
func Make(name string, maxLen int) string {
	s := strings.ToLower(turkish.Replace(name))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return ""
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "p-" + out
	}
	if len(out) > maxLen {
		out = strings.TrimRight(out[:maxLen], "-")
	}
	if len(out) < 2 {
		return ""
	}
	return out
}

// Valid reports whether s is a valid slug of 2–50 characters.
func Valid(s string) bool {
	if len(s) < 2 || len(s) > 50 || s[0] < 'a' || s[0] > 'z' || s[len(s)-1] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
