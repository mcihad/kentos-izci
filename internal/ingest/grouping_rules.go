package ingest

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// GroupingRules are a project's grouping rules (docs/gruplama.md):
// fingerprint rules choose the fingerprint of matching events, stack trace
// rules decide which frames are application code and which take part in
// grouping.
type GroupingRules struct {
	Fingerprint []*FingerprintRule
	Stacktrace  []*StacktraceRule
}

// FingerprintRule is "matchers -> parts", e.g.
// "error.type:TimeoutException transaction:/odeme* -> odeme, {{ default }}".
type FingerprintRule struct {
	Line     int
	Text     string
	matchers []matcher
	parts    []string
}

// StacktraceRule is "frame matchers actions", e.g.
// "stack.function:*Logger* -group".
type StacktraceRule struct {
	Line     int
	Text     string
	matchers []matcher
	// group is +1 (always counts for grouping), -1 (never) or 0.
	group int8
	// app is +1 (application code), -1 (not) or 0.
	app int8
}

// RuleError is an invalid line of grouping rules.
type RuleError struct {
	// Field is "fingerprint_rules" or "stacktrace_rules".
	Field string
	Line  int
	Kind  apiv1.GroupingRuleProblemKind
	// Value is the offending matcher, variable, action or token.
	Value string
	Msg   string
}

// ruleError is a problem found while parsing one line.
type ruleError struct {
	kind  apiv1.GroupingRuleProblemKind
	value string
	msg   string
}

func (e *ruleError) Error() string { return e.msg }

func problem(kind apiv1.GroupingRuleProblemKind, value, format string, args ...any) *ruleError {
	return &ruleError{kind: kind, value: value, msg: fmt.Sprintf(format, args...)}
}

func (e RuleError) Error() string { return fmt.Sprintf("%s line %d: %s", e.Field, e.Line, e.Msg) }

type matcher struct {
	key    string
	negate bool
	re     *regexp.Regexp
}

// Keys a fingerprint rule can match on, besides "tags.<key>".
var eventMatcherKeys = map[string]bool{
	"error.type": true, "error.value": true, "message": true, "logger": true, "level": true,
	"transaction": true, "environment": true, "release": true,
	"stack.module": true, "stack.function": true, "stack.filename": true, "stack.package": true,
}

// Keys a stack trace rule can match frames on.
var frameMatcherKeys = map[string]bool{
	"stack.module": true, "stack.function": true, "stack.filename": true, "stack.package": true,
}

// Variables fingerprint parts can use ({{ name }}), besides "tags.<key>".
var fingerprintVariables = map[string]bool{
	"default": true, "error.type": true, "error.value": true, "message": true, "transaction": true,
	"level": true, "logger": true, "environment": true, "release": true, "stack.function": true, "stack.module": true,
}

const (
	maxGroupingRules = 500
	maxRuleMatchers  = 10
	maxRuleParts     = 10
)

// ParseGroupingRules parses fingerprint and stack trace rules; invalid
// lines are reported and left out.
func ParseGroupingRules(fingerprint, stacktrace string) (*GroupingRules, []RuleError) {
	var rules GroupingRules
	var errs []RuleError
	for n, line := range ruleLines(fingerprint) {
		if line == "" {
			continue
		}
		fail := func(e *ruleError) {
			errs = append(errs, RuleError{Field: "fingerprint_rules", Line: n + 1, Kind: e.kind, Value: e.value, Msg: e.msg})
		}
		if len(rules.Fingerprint) >= maxGroupingRules {
			fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_TOO_MANY, "", "at most %d rules", maxGroupingRules))
			break
		}
		lhs, rhs, ok := strings.Cut(line, "->")
		if !ok {
			fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_NO_ARROW, "", `expected matchers, "->" and the fingerprint`))
			continue
		}
		matchers, perr := parseMatchers(lhs, eventMatcherKeys, true)
		if perr != nil {
			fail(perr)
			continue
		}
		var parts []string
		var bad *ruleError
		for _, p := range strings.Split(rhs, ",") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			if bad = checkVariables(p); bad != nil {
				break
			}
			parts = append(parts, p)
		}
		switch {
		case bad != nil:
			fail(bad)
			continue
		case len(parts) == 0:
			fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_EMPTY_FINGERPRINT, "", "the fingerprint is empty"))
			continue
		case len(parts) > maxRuleParts:
			fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_TOO_MANY, "", "at most %d fingerprint parts", maxRuleParts))
			continue
		}
		rules.Fingerprint = append(rules.Fingerprint, &FingerprintRule{Line: n + 1, Text: line, matchers: matchers, parts: parts})
	}
	for n, line := range ruleLines(stacktrace) {
		if line == "" {
			continue
		}
		fail := func(e *ruleError) {
			errs = append(errs, RuleError{Field: "stacktrace_rules", Line: n + 1, Kind: e.kind, Value: e.value, Msg: e.msg})
		}
		if len(rules.Stacktrace) >= maxGroupingRules {
			fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_TOO_MANY, "", "at most %d rules", maxGroupingRules))
			break
		}
		r := &StacktraceRule{Line: n + 1, Text: line}
		var matchText []string
		bad := false
		for _, tok := range splitRuleTokens(line) {
			switch tok {
			case "+group":
				r.group = 1
			case "-group":
				r.group = -1
			case "+app":
				r.app = 1
			case "-app":
				r.app = -1
			default:
				if strings.HasPrefix(tok, "+") || strings.HasPrefix(tok, "-") {
					fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_UNKNOWN_ACTION, tok,
						"unknown action %q (use +group, -group, +app or -app)", tok))
					bad = true
				}
				matchText = append(matchText, tok)
			}
		}
		if bad {
			continue
		}
		if r.group == 0 && r.app == 0 {
			fail(problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_NO_ACTION, "", "no action (+group, -group, +app or -app)"))
			continue
		}
		matchers, perr := parseMatchers(strings.Join(matchText, " "), frameMatcherKeys, false)
		if perr != nil {
			fail(perr)
			continue
		}
		r.matchers = matchers
		rules.Stacktrace = append(rules.Stacktrace, r)
	}
	return &rules, errs
}

// ruleLines splits rules text into trimmed lines; comments become empty.
func ruleLines(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") {
			l = ""
		}
		lines[i] = l
	}
	return lines
}

// splitRuleTokens splits on spaces outside double quotes.
func splitRuleTokens(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func parseMatchers(text string, keys map[string]bool, tags bool) ([]matcher, *ruleError) {
	tokens := splitRuleTokens(strings.TrimSpace(text))
	if len(tokens) == 0 {
		return nil, problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_NO_MATCHER, "", "no matcher")
	}
	if len(tokens) > maxRuleMatchers {
		return nil, problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_TOO_MANY, "", "at most %d matchers", maxRuleMatchers)
	}
	var out []matcher
	for _, tok := range tokens {
		m := matcher{}
		if strings.HasPrefix(tok, "!") {
			m.negate, tok = true, tok[1:]
		}
		key, value, ok := strings.Cut(tok, ":")
		key = strings.ToLower(key)
		if !ok || value == "" {
			return nil, problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_SYNTAX, tok, "expected key:value, got %q", tok)
		}
		if !keys[key] && !(tags && strings.HasPrefix(key, "tags.") && len(key) > len("tags.")) {
			return nil, problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_UNKNOWN_MATCHER, key, "unknown matcher %q", key)
		}
		if strings.HasPrefix(value, `"`) {
			if len(value) < 2 || !strings.HasSuffix(value, `"`) {
				return nil, problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_UNTERMINATED_QUOTE, tok,
					"unterminated quote in %q", tok)
			}
			value = value[1 : len(value)-1]
		}
		m.key, m.re = key, globPattern(value)
		out = append(out, m)
	}
	return out, nil
}

// globPattern compiles a case-insensitive glob: "*" matches anything, "?"
// one character.
func globPattern(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("(?is)^")
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

var reVariable = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)

func checkVariables(part string) *ruleError {
	for _, m := range reVariable.FindAllStringSubmatch(part, -1) {
		name := strings.ToLower(m[1])
		if !fingerprintVariables[name] && !(strings.HasPrefix(name, "tags.") && len(name) > len("tags.")) {
			return problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_UNKNOWN_VARIABLE, m[1], "unknown variable {{ %s }}", m[1])
		}
		if name == "default" && !isDefaultMarker(part) {
			return problem(apiv1.GroupingRuleProblemKind_GROUPING_RULE_PROBLEM_KIND_DEFAULT_NOT_ALONE, "",
				"{{ default }} must be a fingerprint part of its own")
		}
	}
	return nil
}

// rulesCache keeps compiled rules per rules text: projects share their
// rules across events, and compiling globs per event would be wasteful.
var rulesCache struct {
	sync.Mutex
	m map[string]*GroupingRules
}

// CompileGroupingRules returns the rules of project settings, compiled
// once; invalid lines are left out (UpdateProject rejects them).
func CompileGroupingRules(s *apiv1.GroupingSettings) *GroupingRules {
	fp, st := s.GetFingerprintRules(), s.GetStacktraceRules()
	if strings.TrimSpace(fp) == "" && strings.TrimSpace(st) == "" {
		return nil
	}
	key := fp + "\x00" + st
	rulesCache.Lock()
	defer rulesCache.Unlock()
	if r, ok := rulesCache.m[key]; ok {
		return r
	}
	if rulesCache.m == nil || len(rulesCache.m) >= 256 {
		rulesCache.m = map[string]*GroupingRules{}
	}
	r, _ := ParseGroupingRules(fp, st)
	rulesCache.m[key] = r
	return r
}

// frameMatches reports whether every matcher accepts the frame.
func frameMatches(ms []matcher, f *ingestv1.Frame) bool {
	for _, m := range ms {
		var ok bool
		switch m.key {
		case "stack.module":
			ok = m.re.MatchString(f.GetModule())
		case "stack.function":
			ok = m.re.MatchString(f.GetFunction())
		case "stack.filename":
			ok = m.re.MatchString(f.GetFilename()) || (f.GetAbsPath() != "" && m.re.MatchString(f.GetAbsPath()))
		case "stack.package":
			ok = m.re.MatchString(f.GetPackage())
		}
		if ok == m.negate {
			return false
		}
	}
	return true
}

// applyApp applies the +app/-app actions of stack trace rules; the last
// matching rule wins and overrides the SDK.
func (r *GroupingRules) applyApp(frames []*ingestv1.Frame) {
	if r == nil {
		return
	}
	for _, f := range frames {
		for _, rule := range r.Stacktrace {
			if rule.app != 0 && frameMatches(rule.matchers, f) {
				v := rule.app > 0
				f.InApp = &v
			}
		}
	}
}

// frameGroup is the +group/-group action of the last matching rule.
func (r *GroupingRules) frameGroup(f *ingestv1.Frame) int8 {
	if r == nil {
		return 0
	}
	var g int8
	for _, rule := range r.Stacktrace {
		if rule.group != 0 && frameMatches(rule.matchers, f) {
			g = rule.group
		}
	}
	return g
}

// eventFacts is what fingerprint rules match and fill in.
type eventFacts struct {
	ev       *ingestv1.Event
	platform string
}

func (e eventFacts) primary() *ingestv1.Exception {
	if ex := e.ev.GetExceptions(); len(ex) > 0 {
		return ex[len(ex)-1]
	}
	return nil
}

func (e eventFacts) message() string {
	m := e.ev.GetMessage()
	if m.GetFormatted() != "" {
		return m.GetFormatted()
	}
	return m.GetTemplate()
}

// topFrame is the newest application frame of the primary exception, or
// its newest frame.
func (e eventFacts) topFrame() *ingestv1.Frame {
	frames := e.primary().GetStacktrace().GetFrames()
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].GetInApp() {
			return frames[i]
		}
	}
	if len(frames) > 0 {
		return frames[len(frames)-1]
	}
	return nil
}

func (e eventFacts) matches(m matcher) bool {
	any := func(values ...string) bool {
		for _, v := range values {
			if m.re.MatchString(v) {
				return true
			}
		}
		return false
	}
	var ok bool
	switch m.key {
	case "error.type", "error.value":
		for _, ex := range e.ev.GetExceptions() {
			v := ex.GetType()
			if m.key == "error.value" {
				v = ex.GetValue()
			}
			if m.re.MatchString(v) {
				ok = true
				break
			}
		}
	case "message":
		ok = any(e.message(), e.ev.GetMessage().GetTemplate())
	case "logger":
		ok = m.re.MatchString(e.ev.GetLogger())
	case "level":
		ok = m.re.MatchString(levelName(e.ev.GetLevel()))
	case "transaction":
		ok = m.re.MatchString(e.ev.GetTransaction())
	case "environment":
		ok = m.re.MatchString(e.ev.GetEnvironment())
	case "release":
		ok = m.re.MatchString(e.ev.GetRelease())
	case "stack.module", "stack.function", "stack.filename", "stack.package":
		for _, ex := range e.ev.GetExceptions() {
			for _, f := range ex.GetStacktrace().GetFrames() {
				if frameMatches([]matcher{{key: m.key, re: m.re}}, f) {
					ok = true
					break
				}
			}
		}
	default:
		if tag, found := strings.CutPrefix(m.key, "tags."); found {
			if v, has := e.ev.GetTags()[tag]; has {
				ok = m.re.MatchString(v)
			}
		}
	}
	return ok != m.negate
}

// variable is the value of a fingerprint variable, normalized so that
// volatile parts do not split issues.
func (e eventFacts) variable(name string) string {
	switch name {
	case "error.type":
		return normalizeType(e.primary().GetType())
	case "error.value":
		return normalizeMessage(firstLine(e.primary().GetValue()))
	case "message":
		if t := e.ev.GetMessage().GetTemplate(); t != "" {
			return t
		}
		return normalizeMessage(firstLine(e.message()))
	case "transaction":
		return e.ev.GetTransaction()
	case "level":
		return levelName(e.ev.GetLevel())
	case "logger":
		return e.ev.GetLogger()
	case "environment":
		return e.ev.GetEnvironment()
	case "release":
		return e.ev.GetRelease()
	case "stack.function":
		if f := e.topFrame(); f != nil {
			p := f.GetPlatform()
			if p == "" {
				p = e.platform
			}
			return normalizeFunction(f.GetFunction(), p)
		}
	case "stack.module":
		return e.topFrame().GetModule()
	default:
		if tag, ok := strings.CutPrefix(name, "tags."); ok {
			return e.ev.GetTags()[tag]
		}
	}
	return ""
}

// match returns the first fingerprint rule that matches the event.
func (r *GroupingRules) match(e eventFacts) *FingerprintRule {
	if r == nil {
		return nil
	}
rules:
	for _, rule := range r.Fingerprint {
		for _, m := range rule.matchers {
			if !e.matches(m) {
				continue rules
			}
		}
		return rule
	}
	return nil
}

// expand turns a rule's parts into grouping components.
func (rule *FingerprintRule) expand(e eventFacts, def []string) []string {
	var comps []string
	for _, p := range rule.parts {
		if isDefaultMarker(p) {
			comps = append(comps, def...)
			continue
		}
		v := reVariable.ReplaceAllStringFunc(p, func(s string) string {
			name := strings.ToLower(reVariable.FindStringSubmatch(s)[1])
			if v := e.variable(name); v != "" {
				return v
			}
			return "<none>"
		})
		comps = append(comps, "rule:"+v)
	}
	return comps
}

// RegroupStored computes the grouping a stored (already normalized) event
// gets with rules: their +app and -app actions apply on top of the stored
// in-app flags.
func RegroupStored(ev *ingestv1.Event, platform string, rules *GroupingRules) Grouping {
	if rules != nil {
		for _, ex := range ev.GetExceptions() {
			rules.applyApp(ex.GetStacktrace().GetFrames())
		}
		for _, th := range ev.GetThreads() {
			rules.applyApp(th.GetStacktrace().GetFrames())
		}
	}
	return ComputeGroupingWith(ev, platform, rules)
}
