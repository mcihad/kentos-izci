package ingest

import (
	"crypto/sha256"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// GroupingVersion identifies the fingerprint algorithm. Changing the
// algorithm requires a new version so that existing issues keep their
// fingerprints.
const GroupingVersion = 1

// Grouping strategies, from most to least specific.
const (
	StrategyCustom     = "custom"
	StrategyStacktrace = "stacktrace"
	StrategyException  = "exception"
	StrategyMessage    = "message"
)

// Grouping is the fingerprint of an event and the human-readable inputs it
// was computed from (shown in the UI as "why is this event in this issue").
type Grouping struct {
	Strategy   string
	Components []string
	Hash       [16]byte
}

// maxGroupingFrames is how many crash-side frames take part in grouping.
const maxGroupingFrames = 30

// ComputeGrouping implements grouping v1 (documented in docs/gruplama.md):
//
//  1. A custom Event.fingerprint wins; "{{ default }}" expands to the
//     default components.
//  2. Stack traces: for the primary exception and up to two causes, the
//     exception type plus, per frame, the normalized module, function and
//     file name. Only in-app frames count when any exist; line numbers never
//     count, so events survive unrelated code shifts.
//  3. Exceptions without frames: type plus normalized message.
//  4. Messages: the template, or the normalized first line.
//
// Normalization replaces volatile tokens (numbers, UUIDs, hex, IPs, emails,
// URLs, dates, closure/lambda suffixes, bundle hashes) with placeholders.
func ComputeGrouping(ev *ingestv1.Event, platform string) Grouping {
	strategy, def := defaultComponents(ev, platform)
	if fp := ev.GetFingerprint(); len(fp) > 0 {
		var comps []string
		for _, f := range fp {
			if isDefaultMarker(f) {
				comps = append(comps, def...)
			} else {
				comps = append(comps, "custom:"+f)
			}
		}
		return finalize(StrategyCustom, comps)
	}
	return finalize(strategy, def)
}

func isDefaultMarker(s string) bool {
	return strings.ReplaceAll(s, " ", "") == "{{default}}"
}

func finalize(strategy string, comps []string) Grouping {
	h := sha256.New()
	h.Write([]byte("izci/grouping/v" + strconv.Itoa(GroupingVersion) + "\n"))
	for _, c := range comps {
		h.Write([]byte(c))
		h.Write([]byte{'\n'})
	}
	var g Grouping
	copy(g.Hash[:], h.Sum(nil))
	g.Strategy = strategy
	g.Components = comps
	return g
}

func defaultComponents(ev *ingestv1.Event, platform string) (string, []string) {
	excs := ev.GetExceptions()
	if len(excs) > 0 {
		start := max(0, len(excs)-3)
		chain := excs[start:]
		hasFrames := false
		for _, ex := range chain {
			if len(ex.GetStacktrace().GetFrames()) > 0 {
				hasFrames = true
				break
			}
		}
		var comps []string
		for _, ex := range chain {
			if typ := ex.GetType(); typ != "" && !ex.GetMechanism().GetSynthetic() {
				comps = append(comps, "type:"+normalizeType(typ))
			}
			frames := ex.GetStacktrace().GetFrames()
			if len(frames) == 0 {
				if !hasFrames {
					if v := normalizeMessage(firstLine(ex.GetValue())); v != "" {
						comps = append(comps, "value:"+v)
					}
				}
				continue
			}
			for _, f := range groupingFrames(frames) {
				if c := frameComponent(f, platform); c != "" {
					comps = append(comps, "frame:"+c)
				}
			}
		}
		if len(comps) > 0 {
			if hasFrames {
				return StrategyStacktrace, comps
			}
			return StrategyException, comps
		}
	}

	// A crash report without exceptions: group by the crashed thread.
	for _, th := range ev.GetThreads() {
		if th.GetCrashed() && len(th.GetStacktrace().GetFrames()) > 0 {
			var comps []string
			for _, f := range groupingFrames(th.GetStacktrace().GetFrames()) {
				if c := frameComponent(f, platform); c != "" {
					comps = append(comps, "frame:"+c)
				}
			}
			if len(comps) > 0 {
				return StrategyStacktrace, comps
			}
		}
	}

	msg := ev.GetMessage()
	text := msg.GetTemplate()
	if text == "" {
		text = normalizeMessage(firstLine(msg.GetFormatted()))
	}
	if text == "" {
		text = "<empty>"
	}
	return StrategyMessage, []string{"message:" + text}
}

// groupingFrames selects the crash-side frames that take part in grouping:
// in-app frames when any exist, otherwise all frames; at most
// maxGroupingFrames, newest last.
func groupingFrames(frames []*ingestv1.Frame) []*ingestv1.Frame {
	var inApp []*ingestv1.Frame
	for _, f := range frames {
		if f.GetInApp() {
			inApp = append(inApp, f)
		}
	}
	sel := frames
	if len(inApp) > 0 {
		sel = inApp
	}
	if len(sel) > maxGroupingFrames {
		sel = sel[len(sel)-maxGroupingFrames:]
	}
	return sel
}

func frameComponent(f *ingestv1.Frame, platform string) string {
	p := f.GetPlatform()
	if p == "" {
		p = platform
	}
	fn := normalizeFunction(f.GetFunction(), p)
	file := normalizeFilename(f.GetFilename(), f.GetAbsPath())
	mod := strings.TrimSpace(f.GetModule())
	if fn == "" {
		// Minified or anonymous code: the source line is the most stable
		// identity left; fall back to the line number as a last resort.
		if ctx := strings.Join(strings.Fields(f.GetContextLine()), " "); ctx != "" && utf8.RuneCountInString(ctx) <= 200 {
			fn = "ctx:" + ctx
		} else if f.GetLineno() > 0 {
			fn = "line:" + strconv.Itoa(int(f.GetLineno()))
		}
	}
	if fn == "" && file == "" && mod == "" {
		return ""
	}
	return mod + "|" + file + "|" + fn
}

var (
	reGoClosure     = regexp.MustCompile(`(\.func\d+|\.gowrap\d+|\.deferwrap\d+)(\.\d+)*$|-fm$`)
	reDotnetLambda  = regexp.MustCompile(`<([^>]*)>b__[0-9_]+`)
	reDotnetAsync   = regexp.MustCompile(`<([^>]*)>d__\d+(\.MoveNext)?`)
	reDotnetDisplay = regexp.MustCompile(`\.?<>c(__DisplayClass[0-9_]+)?`)
	reDotnetLMethod = regexp.MustCompile(`lambda_method\d+`)
	reJavaLambda    = regexp.MustCompile(`lambda\$([A-Za-z0-9_]+)\$\d+`)
	reJavaLambdaCls = regexp.MustCompile(`\$\$Lambda\$?[0-9]*(/0x[0-9a-fA-F]+)?`)
	reJavaAccess    = regexp.MustCompile(`access\$\d+`)
	reAddress       = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	reLongNumber    = regexp.MustCompile(`\d{3,}`)
)

// normalizeFunction removes compiler-generated, per-build volatile parts of
// function names.
func normalizeFunction(fn, platform string) string {
	fn = strings.TrimSpace(fn)
	if fn == "" || fn == "?" || fn == "<unknown>" {
		return ""
	}
	switch platform {
	case "go":
		fn = reGoClosure.ReplaceAllString(fn, "")
	case "dotnet":
		fn = reDotnetAsync.ReplaceAllString(fn, "$1")
		fn = reDotnetLambda.ReplaceAllString(fn, "$1.lambda")
		fn = reDotnetDisplay.ReplaceAllString(fn, "")
		fn = reDotnetLMethod.ReplaceAllString(fn, "lambda_method")
	case "java", "kotlin":
		fn = reJavaLambda.ReplaceAllString(fn, "lambda$$$1")
		fn = reJavaLambdaCls.ReplaceAllString(fn, "$$$$Lambda")
		fn = reJavaAccess.ReplaceAllString(fn, "access$$")
	case "javascript", "node":
		fn = strings.TrimPrefix(fn, "async ")
		fn = strings.ReplaceAll(fn, "Object.<anonymous>", "<anonymous>")
		if fn == "<anonymous>" || (len(fn) <= 2 && !strings.Contains(fn, ".")) {
			// Minified single-letter names change with every build.
			return ""
		}
	}
	fn = reAddress.ReplaceAllString(fn, "")
	fn = reLongNumber.ReplaceAllString(fn, "<num>")
	return fn
}

// normalizeFilename keeps relative paths, reduces absolute paths and URLs to
// their base name and strips bundler content hashes ("index-CeX-bC8z.js" →
// "index.js", "main.3f2a1c9d.chunk.js" → "main.chunk.js").
func normalizeFilename(filename, absPath string) string {
	f := filename
	if f == "" {
		f = absPath
	}
	if f == "" {
		return ""
	}
	if i := strings.IndexAny(f, "?#"); i >= 0 {
		f = f[:i]
	}
	f = strings.ReplaceAll(f, "\\", "/")
	if strings.Contains(f, "://") || strings.HasPrefix(f, "/") || (len(f) > 2 && f[1] == ':') {
		f = path.Base(f)
	}
	return stripBundleHash(f)
}

var (
	// webpack: "main.3f2a1c9d.chunk", "chunk-vendors.5f6a7b8c"
	reDotHash = regexp.MustCompile(`^(.+?)\.([0-9a-f]{8,20})(\.chunk)?$`)
	// Next.js and others: "_app-9f1c2b3a4d5e6f70"
	reDashHex = regexp.MustCompile(`^(.+)-([0-9a-f]{12,20})$`)
	// Vite/Rollup: "index-CeX-bC8z" (8-character base64url hash)
	reViteHash = regexp.MustCompile(`^(.+)-([A-Za-z0-9_-]{8})$`)
)

func stripBundleHash(name string) string {
	dir, base := path.Split(name)
	ext := path.Ext(base)
	switch ext {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".css", ".map":
	default:
		return name
	}
	stem := strings.TrimSuffix(base, ext)
	if m := reDotHash.FindStringSubmatch(stem); m != nil && hasDigit(m[2]) {
		stem = m[1] + m[3]
	} else if m := reDashHex.FindStringSubmatch(stem); m != nil && hasDigit(m[2]) {
		stem = m[1]
	} else if m := reViteHash.FindStringSubmatch(stem); m != nil && (hasDigit(m[2]) || hasUpper(m[2])) {
		// A lower-case word such as "-listview" is not a hash.
		stem = m[1]
	}
	return dir + stem + ext
}

func hasDigit(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
}

func hasUpper(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0
}

func normalizeType(t string) string {
	t = strings.TrimSpace(t)
	// Generic type arguments vary per instantiation: List`1[[System.Int32, …]].
	if i := strings.Index(t, "[["); i > 0 {
		t = t[:i]
	}
	return t
}

var messageRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<uuid>"},
	{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`), "<email>"},
	{regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`), "<url>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:[.,]\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?\b`), "<date>"},
	{regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`), "<ip>"},
	{regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`), "<hex>"},
	{regexp.MustCompile(`\b(?:[0-9a-fA-F]*[0-9][0-9a-fA-F]*[a-fA-F][0-9a-fA-F]*|[0-9a-fA-F]*[a-fA-F][0-9a-fA-F]*[0-9][0-9a-fA-F]*)\b`), "<hex>"},
	// Numbers anywhere, including those glued to units ("30000ms", "45s").
	{regexp.MustCompile(`\d+(?:[.,]\d+)*`), "<num>"},
}

// normalizeMessage replaces volatile tokens in a message so that messages
// differing only in ids, numbers or timestamps group together.
func normalizeMessage(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 512 {
		s = truncateUTF8(s, 512)
	}
	for _, r := range messageRules {
		s = r.re.ReplaceAllStringFunc(s, func(m string) string {
			// Hex rule: only tokens of at least 8 characters are ids.
			if r.repl == "<hex>" && !strings.HasPrefix(m, "0x") && len(m) < 8 {
				return m
			}
			return r.repl
		})
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
