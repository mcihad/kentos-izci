package ingest

import (
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// FilterReason is why an inbound filter dropped an event
// (izci.api.v1.FilterReason).
type FilterReason int16

// Filter reasons.
const (
	FilterNone             FilterReason = 0
	FilterEnvironment      FilterReason = 1
	FilterBrowserExtension FilterReason = 2
	FilterWebCrawler       FilterReason = 3
	FilterLegacyBrowser    FilterReason = 4
	FilterLocalhost        FilterReason = 5
	FilterErrorMessage     FilterReason = 6
	FilterIPAddress        FilterReason = 7
	FilterRelease          FilterReason = 8
)

// Filters are a project's inbound filters, compiled.
type Filters struct {
	environments []string
	extensions   bool
	crawlers     bool
	legacy       bool
	localhost    bool
	messages     []*regexp.Regexp
	ips          []netip.Prefix
	releases     []*regexp.Regexp
}

// filterCache keeps compiled filters per settings value; key resolution
// hands out a new settings pointer whenever the key cache refreshes, so
// entries expire quickly.
var filterCache = expirable.NewLRU[*apiv1.ProjectSettings, *Filters](10000, nil, 10*time.Minute)

// FiltersOf returns the compiled filters of project settings.
func FiltersOf(settings *apiv1.ProjectSettings) *Filters {
	if settings == nil {
		return &Filters{}
	}
	if f, ok := filterCache.Get(settings); ok {
		return f
	}
	f := CompileFilters(settings)
	filterCache.Add(settings, f)
	return f
}

// CompileFilters compiles project settings; invalid patterns are skipped
// (the API validates them on save).
func CompileFilters(settings *apiv1.ProjectSettings) *Filters {
	in := settings.GetFilters()
	f := &Filters{
		environments: settings.GetIgnoredEnvironments(),
		extensions:   in.GetBrowserExtensions(),
		crawlers:     in.GetWebCrawlers(),
		legacy:       in.GetLegacyBrowsers(),
		localhost:    in.GetLocalhost(),
	}
	for _, p := range in.GetErrorMessages() {
		if re, err := Glob(p); err == nil {
			f.messages = append(f.messages, re)
		}
	}
	for _, p := range in.GetReleases() {
		if re, err := Glob(p); err == nil {
			f.releases = append(f.releases, re)
		}
	}
	for _, s := range in.GetIpAddresses() {
		if p, err := ParseIPFilter(s); err == nil {
			f.ips = append(f.ips, p)
		}
	}
	return f
}

// Glob compiles a case-insensitive glob pattern matching a whole string:
// "*" matches anything (also line breaks), "?" one character.
func Glob(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString(`(?is)^`)
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(`$`)
	return regexp.Compile(b.String())
}

// ParseIPFilter parses an address ("10.0.0.5") or a CIDR range
// ("10.0.0.0/8").
func ParseIPFilter(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// Match returns why the filters drop an event, or FilterNone. It runs on
// the event as received, before normalization: transport is the address
// the event came from and platform the project's default platform.
func (f *Filters) Match(ev *ingestv1.Event, transport netip.Addr, platform string) FilterReason {
	if len(f.environments) > 0 {
		env := strings.ToLower(strings.TrimSpace(ev.GetEnvironment()))
		if env == "" {
			env = "production"
		}
		if slices.Contains(f.environments, env) {
			return FilterEnvironment
		}
	}
	if p := strings.ToLower(ev.GetPlatform()); p != "" {
		platform = p
	}
	if f.extensions && fromBrowserExtension(ev, platform) {
		return FilterBrowserExtension
	}
	if f.crawlers || f.legacy {
		if ua := UserAgentOf(ev); ua != "" {
			if f.crawlers && isCrawler(ua) {
				return FilterWebCrawler
			}
			if f.legacy && isLegacyBrowser(ParseUserAgent(ua)) {
				return FilterLegacyBrowser
			}
		}
	}
	var client netip.Addr
	if f.localhost || len(f.ips) > 0 {
		client = resolveClientIP(ev, platform, transport)
	}
	if f.localhost && fromLocalhost(ev, client) {
		return FilterLocalhost
	}
	// Addresses match the event's client (the end user) or the host that
	// sent it (a server, for server-side SDKs).
	if len(f.ips) > 0 {
		for _, a := range []netip.Addr{client, transport} {
			if !a.IsValid() {
				continue
			}
			for _, p := range f.ips {
				if p.Contains(a.Unmap()) {
					return FilterIPAddress
				}
			}
		}
	}
	if len(f.messages) > 0 && matchesAny(f.messages, messagesOf(ev)) {
		return FilterErrorMessage
	}
	if len(f.releases) > 0 && ev.GetRelease() != "" && matchesAny(f.releases, []string{strings.TrimSpace(ev.GetRelease())}) {
		return FilterRelease
	}
	return FilterNone
}

func matchesAny(res []*regexp.Regexp, values []string) bool {
	for _, v := range values {
		for _, re := range res {
			if re.MatchString(v) {
				return true
			}
		}
	}
	return false
}

// messagesOf are the texts error message patterns are matched against.
func messagesOf(ev *ingestv1.Event) []string {
	var out []string
	for _, ex := range ev.GetExceptions() {
		t, v := strings.TrimSpace(ex.GetType()), strings.TrimSpace(ex.GetValue())
		switch {
		case t != "" && v != "":
			out = append(out, t+": "+v, v, t)
		case v != "":
			out = append(out, v)
		case t != "":
			out = append(out, t)
		}
	}
	if m := ev.GetMessage(); m != nil {
		for _, s := range []string{m.GetFormatted(), m.GetTemplate()} {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// extensionSchemes are the script URLs of browser extensions.
var extensionSchemes = []string{"chrome-extension://", "moz-extension://", "safari-extension://", "safari-web-extension://",
	"ms-browser-extension://", "edge-extension://", "opera-extension://", "webkit-masked-url://"}

// extensionMessages are errors browser extensions and injected scripts are
// known for (Sentry's browser extension filter), and the useless
// "Script error." of cross-origin scripts.
var extensionMessages = regexp.MustCompile(`(?i)top\.GLOBALS|originalCreateNotification|canvas\.contentDocument|` +
	`MyApp_RemoveAllHighlights|http://tt\.epicplay\.com|Can't find variable: ZiteReader|jigsaw is not defined|` +
	`ComboSearch is not defined|http://loading\.retry\.widdit\.com/|atomicFindClose|fb_xd_fragment|bmi_SafeAddOnload|` +
	`EBCallBackMessageReceived|^conduitPage$|^Script error\.?$|^Javascript error: Script error\.? on line 0$`)

func fromBrowserExtension(ev *ingestv1.Event, platform string) bool {
	check := func(st *ingestv1.Stacktrace) bool {
		for _, f := range st.GetFrames() {
			for _, u := range []string{f.GetAbsPath(), f.GetFilename()} {
				u = strings.ToLower(u)
				for _, s := range extensionSchemes {
					if strings.HasPrefix(u, s) {
						return true
					}
				}
			}
		}
		return false
	}
	for _, ex := range ev.GetExceptions() {
		if check(ex.GetStacktrace()) {
			return true
		}
	}
	for _, th := range ev.GetThreads() {
		if check(th.GetStacktrace()) {
			return true
		}
	}
	if platform != "javascript" {
		return false
	}
	for _, m := range messagesOf(ev) {
		if extensionMessages.MatchString(m) {
			return true
		}
	}
	return false
}

// crawlers matches the User-Agents of search engines, link previews and
// uptime checkers.
var crawlers = regexp.MustCompile(`(?i)googlebot|mediapartners-google|adsbot-google|feedfetcher-google|storebot-google|` +
	`google-inspectiontool|bingbot|bingpreview|slurp|duckduckbot|baiduspider|yandex(bot|images|metrika)|sogou|exabot|` +
	`facebot|facebookexternalhit|ia_archiver|applebot|twitterbot|linkedinbot|petalbot|ahrefsbot|semrushbot|mj12bot|` +
	`dotbot|seznambot|bytespider|gptbot|claudebot|ccbot|chrome-lighthouse|pingdom|uptimerobot|statuscake|site24x7|` +
	`newrelicpinger|gtmetrix|headlesschrome|phantomjs|bots?[/\s);]|spider[/\s);]|crawler`)

func isCrawler(ua string) bool { return crawlers.MatchString(ua) }

// isLegacyBrowser reports browsers izci treats as unsupported.
func isLegacyBrowser(u UserAgentInfo) bool {
	major, _ := strconv.Atoi(strings.SplitN(u.BrowserVersion, ".", 2)[0])
	switch u.Browser {
	case "IE", "Opera Mini", "Android Browser":
		return true
	case "Edge":
		return major > 0 && major < 79
	case "Safari":
		return major > 0 && major < 12
	case "Chrome":
		return major > 0 && major < 80
	case "Firefox":
		return major > 0 && major < 78
	case "Opera":
		return major > 0 && major < 67
	}
	return false
}

// fromLocalhost reports events of local development.
func fromLocalhost(ev *ingestv1.Event, client netip.Addr) bool {
	if client.IsValid() && client.Unmap().IsLoopback() {
		return true
	}
	raw := ev.GetRequest().GetUrl()
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if a, err := netip.ParseAddr(host); err == nil && a.Unmap().IsLoopback() {
		return true
	}
	return false
}

// Name is the reason as used in metrics labels.
func (r FilterReason) Name() string {
	switch r {
	case FilterEnvironment:
		return "environment"
	case FilterBrowserExtension:
		return "browser_extension"
	case FilterWebCrawler:
		return "web_crawler"
	case FilterLegacyBrowser:
		return "legacy_browser"
	case FilterLocalhost:
		return "localhost"
	case FilterErrorMessage:
		return "error_message"
	case FilterIPAddress:
		return "ip_address"
	case FilterRelease:
		return "release"
	}
	return "none"
}
