// Package ownership decides who owns an issue: rules in Sentry's syntax
// ("path:src/billing/* ayse@kentos.com.tr"), an imported CODEOWNERS file,
// and the suspect commit (the commit that last changed the failing lines,
// found with git blame through GitHub). New issues can be assigned to
// their owner automatically.
package ownership

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Kind is what a rule looks at.
type Kind int8

// Rule kinds.
const (
	KindPath       Kind = iota + 1 // file paths of stack frames
	KindModule                     // modules (namespaces, packages) of stack frames
	KindURL                        // request URL
	KindTag                        // an event tag, "tags.<key>"
	KindCodeowners                 // a CODEOWNERS pattern on repository paths
)

// Source says where a rule comes from.
type Source int8

// Rule sources.
const (
	SourceRules      Source = 1 // the project's ownership rules
	SourceCodeowners Source = 2 // the imported CODEOWNERS file
)

// Rule is one ownership rule.
type Rule struct {
	Kind    Kind
	Tag     string // tag key of KindTag
	Pattern string
	// Owners as written: e-mail addresses and aliases ("@login",
	// "@org/team", "#team"). CODEOWNERS rules may have none, which makes
	// the matching paths unowned.
	Owners []string
	Source Source
	Line   int
	re     *regexp.Regexp
}

// Text is the rule's matcher as written, e.g. "path:src/billing/*".
func (r *Rule) Text() string {
	switch r.Kind {
	case KindPath:
		return "path:" + r.Pattern
	case KindModule:
		return "module:" + r.Pattern
	case KindURL:
		return "url:" + r.Pattern
	case KindTag:
		return "tags." + r.Tag + ":" + r.Pattern
	}
	return "codeowners:" + r.Pattern
}

// Problem is what is wrong with a line (izci.api.v1.RuleProblemKind).
type Problem int8

// Problems.
const (
	ProblemSyntax         Problem = 1 // not "type:pattern owners"
	ProblemUnknownType    Problem = 2 // Value: the type
	ProblemNoOwners       Problem = 3
	ProblemInvalidOwner   Problem = 4 // Value: the owner
	ProblemInvalidPattern Problem = 5 // Value: the pattern
	ProblemTooMany        Problem = 6 // too many rules, or owners on a line
)

// LineError is a problem on one line of rules or CODEOWNERS.
type LineError struct {
	Line    int
	Problem Problem
	Value   string
	Msg     string
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Size limits of rules and CODEOWNERS text.
const (
	MaxTextBytes = 256 << 10
	maxRules     = 5000
	maxOwners    = 20
)

// ParseRules parses ownership rules: one rule per line, "type:pattern"
// followed by owners, "#" starting a comment line. Invalid lines are
// reported and skipped.
func ParseRules(text string) ([]*Rule, []*LineError) {
	var (
		rules []*Rule
		errs  []*LineError
	)
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		typ, pattern, ok := strings.Cut(fields[0], ":")
		if !ok || pattern == "" {
			errs = append(errs, &LineError{n, ProblemSyntax, fields[0], "expected type:pattern followed by owners"})
			continue
		}
		r := &Rule{Pattern: pattern, Owners: fields[1:], Source: SourceRules, Line: n}
		switch {
		case typ == "path":
			r.Kind = KindPath
		case typ == "module":
			r.Kind = KindModule
		case typ == "url":
			r.Kind = KindURL
		case typ == "codeowners":
			r.Kind = KindCodeowners
		case strings.HasPrefix(typ, "tags.") && len(typ) > len("tags."):
			r.Kind, r.Tag = KindTag, strings.TrimPrefix(typ, "tags.")
		default:
			errs = append(errs, &LineError{n, ProblemUnknownType, typ,
				fmt.Sprintf("unknown rule type %q (path, module, url, tags.<key>, codeowners)", typ)})
			continue
		}
		if len(r.Owners) == 0 {
			errs = append(errs, &LineError{n, ProblemNoOwners, "", "rule has no owners"})
			continue
		}
		if e := checkOwners(n, r.Owners); e != nil {
			errs = append(errs, e)
			continue
		}
		if err := r.compile(); err != nil {
			errs = append(errs, &LineError{n, ProblemInvalidPattern, r.Pattern, "invalid pattern: " + err.Error()})
			continue
		}
		if len(rules) == maxRules {
			errs = append(errs, &LineError{n, ProblemTooMany, "", fmt.Sprintf("more than %d rules", maxRules)})
			break
		}
		rules = append(rules, r)
	}
	return rules, errs
}

// ParseCodeowners parses a CODEOWNERS file (GitHub's format; GitLab section
// headers are skipped). Invalid lines are reported and skipped.
func ParseCodeowners(text string) ([]*Rule, []*LineError) {
	var (
		rules []*Rule
		errs  []*LineError
	)
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "^[") {
			continue
		}
		// Comments after an entry.
		if j := strings.Index(line, " #"); j >= 0 {
			line = strings.TrimSpace(line[:j])
		}
		fields := strings.Fields(line)
		pattern := strings.TrimPrefix(fields[0], `\`)
		r := &Rule{Kind: KindCodeowners, Pattern: pattern, Owners: fields[1:], Source: SourceCodeowners, Line: n}
		if e := checkOwners(n, r.Owners); e != nil {
			errs = append(errs, e)
			continue
		}
		if err := r.compile(); err != nil {
			errs = append(errs, &LineError{n, ProblemInvalidPattern, r.Pattern, "invalid pattern: " + err.Error()})
			continue
		}
		if len(rules) == maxRules {
			errs = append(errs, &LineError{n, ProblemTooMany, "", fmt.Sprintf("more than %d rules", maxRules)})
			break
		}
		rules = append(rules, r)
	}
	return rules, errs
}

func checkOwners(line int, owners []string) *LineError {
	if len(owners) > maxOwners {
		return &LineError{line, ProblemTooMany, "", fmt.Sprintf("more than %d owners", maxOwners)}
	}
	for _, o := range owners {
		if !validOwner(o) {
			return &LineError{line, ProblemInvalidOwner, o, fmt.Sprintf("owner %q is neither an e-mail address nor an @name or #team", o)}
		}
	}
	return nil
}

func validOwner(o string) bool {
	if len(o) > 254 {
		return false
	}
	if strings.HasPrefix(o, "@") || strings.HasPrefix(o, "#") {
		return len(o) > 1 && !strings.ContainsAny(o[1:], "@#")
	}
	local, domain, ok := strings.Cut(o, "@")
	return ok && local != "" && strings.Contains(domain, ".") && !strings.ContainsAny(domain, "@/")
}

// IsAlias reports whether an owner is an alias ("@login", "#team") rather
// than an e-mail address.
func IsAlias(owner string) bool {
	return strings.HasPrefix(owner, "@") || strings.HasPrefix(owner, "#")
}

// NormalizeOwner is the lookup form of an owner: aliases and e-mail
// addresses are case-insensitive.
func NormalizeOwner(owner string) string { return strings.ToLower(strings.TrimSpace(owner)) }

func (r *Rule) compile() error {
	var err error
	switch r.Kind {
	case KindPath:
		p := strings.ReplaceAll(r.Pattern, `\`, "/")
		// Relative patterns match at any directory: "src/billing/*" matches
		// "/app/src/billing/Invoice.cs".
		r.re, err = glob(p, !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "*"))
	case KindModule, KindURL, KindTag:
		r.re, err = glob(r.Pattern, false)
	case KindCodeowners:
		r.re, err = codeownersRegexp(r.Pattern)
	}
	return err
}

// glob compiles a case-insensitive glob matching a whole value: "*"
// matches anything (also "/"), "?" one character.
func glob(pattern string, anyDir bool) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString(`(?is)^`)
	if anyDir {
		b.WriteString(`(?:.*/)?`)
	}
	for _, c := range pattern {
		switch c {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`$`)
	return regexp.Compile(b.String())
}

// codeownersRegexp compiles a CODEOWNERS pattern (gitignore rules) that
// matches repository paths without a leading slash:
//
//   - a leading or inner "/" anchors the pattern at the repository root,
//     otherwise it matches at any depth ("*.js", "apps/");
//   - a trailing "/" matches everything below a directory;
//   - "*" and "?" stay within one path segment, "**" crosses segments;
//   - a last segment without wildcards also matches everything below it
//     ("/build/logs"), one with wildcards only direct entries ("docs/*").
func codeownersRegexp(pattern string) (*regexp.Regexp, error) {
	p := pattern
	dirOnly := strings.HasSuffix(p, "/")
	p = strings.TrimRight(p, "/")
	anchored := strings.HasPrefix(p, "/") || strings.Contains(strings.TrimPrefix(p, "/"), "/")
	p = strings.TrimPrefix(p, "/")
	if strings.HasPrefix(p, "**/") {
		anchored, p = false, strings.TrimPrefix(p, "**/")
	}
	if p == "" || p == "**" {
		return regexp.Compile(`^.*$`)
	}
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString(`(?:.*/)?`)
	}
	for i := 0; i < len(p); i++ {
		switch rest := p[i:]; {
		case strings.HasPrefix(rest, "**/"):
			b.WriteString(`(?:.*/)?`)
			i += 2
		case strings.HasPrefix(rest, "/**") && i+3 == len(p):
			b.WriteString(`/.*`)
			i += 2
		case strings.HasPrefix(rest, "**"):
			b.WriteString(`.*`)
			i++
		case p[i] == '*':
			b.WriteString(`[^/]*`)
		case p[i] == '?':
			b.WriteString(`[^/]`)
		case p[i] == '\\' && i+1 < len(p):
			i++
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
		default:
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
		}
	}
	last := p[strings.LastIndexByte(p, '/')+1:]
	switch {
	case dirOnly:
		b.WriteString(`/.+$`)
	case strings.ContainsAny(last, "*?"):
		b.WriteString(`$`)
	default:
		b.WriteString(`(?:/.*)?$`)
	}
	return regexp.Compile(b.String())
}

// Frame is a stack frame as rules see it.
type Frame struct {
	// Paths are the frame's file paths (absolute path and file name) as
	// sent and normalized.
	Paths  []string
	Module string
	InApp  bool
}

// Subject is what rules match in an event.
type Subject struct {
	// Frames of every exception and thread, innermost first.
	Frames []Frame
	URL    string
	Tags   map[string]string
}

const maxFrames = 200

// SubjectOf extracts what rules match from an event.
func SubjectOf(ev *ingestv1.Event) *Subject {
	s := &Subject{URL: ev.GetRequest().GetUrl(), Tags: map[string]string{}}
	add := func(st *ingestv1.Stacktrace) {
		frames := st.GetFrames()
		for i := len(frames) - 1; i >= 0 && len(s.Frames) < maxFrames; i-- {
			f := frames[i]
			var paths []string
			for _, p := range []string{f.GetAbsPath(), f.GetFilename()} {
				for _, v := range []string{strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"), NormalizePath(p)} {
					if v != "" && !slices.Contains(paths, v) {
						paths = append(paths, v)
					}
				}
			}
			if len(paths) == 0 && f.GetModule() == "" {
				continue
			}
			s.Frames = append(s.Frames, Frame{Paths: paths, Module: f.GetModule(), InApp: f.GetInApp()})
		}
	}
	exceptions := ev.GetExceptions()
	for i := len(exceptions) - 1; i >= 0; i-- {
		add(exceptions[i].GetStacktrace())
	}
	for _, th := range ev.GetThreads() {
		add(th.GetStacktrace())
	}
	for k, v := range ev.GetTags() {
		s.Tags[k] = v
	}
	set := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			s.Tags[k] = v
		}
	}
	if l := ev.GetLevel(); l != ingestv1.Level_LEVEL_UNSPECIFIED {
		set("level", strings.ToLower(strings.TrimPrefix(l.String(), "LEVEL_")))
	}
	set("environment", ev.GetEnvironment())
	set("release", ev.GetRelease())
	set("transaction", ev.GetTransaction())
	set("server_name", ev.GetServerName())
	set("logger", ev.GetLogger())
	set("customer", ev.GetCustomer().GetId())
	set("tenant", ev.GetTenant().GetId())
	return s
}

// NormalizePath turns a frame path into a plain path: backslashes become
// slashes, and URL schemes ("webpack:///", "app:///",
// "https://host") and query strings are dropped.
func NormalizePath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if i := strings.Index(p, "://"); i >= 0 {
		scheme, rest := strings.ToLower(p[:i]), p[i+3:]
		if scheme == "http" || scheme == "https" {
			if j := strings.IndexByte(rest, '/'); j >= 0 {
				rest = rest[j:]
			} else {
				rest = ""
			}
		}
		p = rest
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	return strings.TrimPrefix(p, "~")
}

// Rewrite maps a frame path prefix to a repository path prefix (the
// project repository's path mappings).
type Rewrite struct {
	From string `json:"from_prefix"`
	To   string `json:"to_prefix"`
}

// RepoPaths are the repository paths a frame path may stand for: the
// rewritten path when a mapping applies, otherwise the path and its
// shorter suffixes ("app/src/a.cs", "src/a.cs", "a.cs").
func RepoPaths(p string, rewrites []Rewrite) []string {
	var out []string
	for _, r := range rewrites {
		if r.From != "" && strings.HasPrefix(p, r.From) {
			if v := strings.TrimLeft(r.To+strings.TrimPrefix(p, r.From), "/"); v != "" && !strings.Contains(v, "..") {
				out = append(out, v)
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	rel := strings.TrimLeft(p, "/")
	for n := 0; rel != "" && n < 8; n++ {
		out = append(out, rel)
		i := strings.IndexByte(rel, '/')
		if i < 0 {
			break
		}
		rel = rel[i+1:]
	}
	return out
}

// Ruleset is a project's compiled ownership configuration: CODEOWNERS
// rules first, then the project's own rules, so that the latter win.
type Ruleset struct {
	Rules    []*Rule
	Rewrites []Rewrite
}

// Match returns the rules that match a subject, in order.
func (s *Ruleset) Match(sub *Subject) []*Rule {
	if s == nil || len(s.Rules) == 0 {
		return nil
	}
	// CODEOWNERS describes the repository: look at the application's own
	// frames when there are any.
	var app []Frame
	for _, f := range sub.Frames {
		if f.InApp {
			app = append(app, f)
		}
	}
	if len(app) == 0 {
		app = sub.Frames
	}
	var repoPaths [][]string
	var out []*Rule
	for _, r := range s.Rules {
		matched := false
		switch r.Kind {
		case KindPath:
			matched = anyFrame(sub.Frames, func(f Frame) bool { return slices.ContainsFunc(f.Paths, r.re.MatchString) })
		case KindModule:
			matched = anyFrame(sub.Frames, func(f Frame) bool { return f.Module != "" && r.re.MatchString(f.Module) })
		case KindURL:
			matched = sub.URL != "" && r.re.MatchString(sub.URL)
		case KindTag:
			v, ok := sub.Tags[r.Tag]
			matched = ok && r.re.MatchString(v)
		case KindCodeowners:
			if repoPaths == nil {
				repoPaths = make([][]string, 0, len(app))
				for _, f := range app {
					var ps []string
					for _, p := range f.Paths {
						ps = append(ps, RepoPaths(NormalizePath(p), s.Rewrites)...)
					}
					repoPaths = append(repoPaths, ps)
				}
			}
			for _, ps := range repoPaths {
				if slices.ContainsFunc(ps, r.re.MatchString) {
					matched = true
					break
				}
			}
		}
		if matched {
			out = append(out, r)
		}
	}
	return out
}

func anyFrame(frames []Frame, f func(Frame) bool) bool {
	return slices.ContainsFunc(frames, f)
}

// Winner is the rule that decides automatic assignment: the last match,
// as in CODEOWNERS.
func Winner(matches []*Rule) *Rule {
	if len(matches) == 0 {
		return nil
	}
	return matches[len(matches)-1]
}
