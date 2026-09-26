package ingest

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// truncateUTF8 cuts s to at most n bytes without splitting a UTF-8
// sequence.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// clamp trims s to n bytes (UTF-8 safe), recording a warning for field when
// something was cut. Invalid UTF-8 is repaired.
func (w *warnings) clamp(field, s string, n int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if len(s) > n {
		w.addf("%s: truncated from %d to %d bytes", field, len(s), n)
		return truncateUTF8(s, n)
	}
	return s
}

// clean trims whitespace and clamps.
func (w *warnings) clean(field, s string, n int) string {
	return w.clamp(field, strings.TrimSpace(s), n)
}

// warnings collects non-fatal adjustments made to an event.
type warnings struct {
	list []string
}

const maxWarnings = 50

func (w *warnings) addf(format string, args ...any) {
	if len(w.list) >= maxWarnings {
		return
	}
	w.list = append(w.list, sprintf(format, args...))
}

// clampMap limits a string map to maxEntries entries with bounded keys and
// values (keys are visited in sorted order for determinism).
func (w *warnings) clampMap(field string, m map[string]string, maxEntries, maxKey, maxValue int) map[string]string {
	if len(m) == 0 {
		return m
	}
	keys := sortedKeys(m)
	out := make(map[string]string, min(len(m), maxEntries))
	for _, k := range keys {
		if len(out) >= maxEntries {
			w.addf("%s: %d entries dropped (max %d)", field, len(m)-maxEntries, maxEntries)
			break
		}
		ck := truncateUTF8(strings.ToValidUTF8(k, "�"), maxKey)
		out[ck] = w.clamp(field+"."+ck, m[k], maxValue)
	}
	return out
}

// clampList limits a string list's length and element size.
func (w *warnings) clampList(field string, l []string, maxItems, maxLen int) []string {
	if len(l) > maxItems {
		w.addf("%s: %d items dropped (max %d)", field, len(l)-maxItems, maxItems)
		l = l[:maxItems]
	}
	for i, s := range l {
		l[i] = w.clamp(field, s, maxLen)
	}
	return l
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
