package ingest

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/clientip"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// ErrEmptyEvent rejects events without a message or an exception.
var ErrEmptyEvent = errors.New("event has neither a message nor an exception")

// ProjectContext is what normalization needs to know about the project an
// event belongs to.
type ProjectContext struct {
	OrgID       uuid.UUID
	OrgSlug     string
	ProjectID   uuid.UUID
	ProjectSlug string
	Platform    string
	Settings    *apiv1.ProjectSettings
}

// NormalizeOptions carries request-level inputs.
type NormalizeOptions struct {
	Project ProjectContext
	// TransportIP is the resolved address of the client that sent the event.
	TransportIP netip.Addr
	// Retention is the maximum age of accepted timestamps.
	Retention time.Duration
	// Now overrides the clock (tests); zero means time.Now.
	Now time.Time
}

// Location is the top application frame of an event.
type Location struct {
	Filename string `json:"filename,omitempty"`
	Function string `json:"function,omitempty"`
	Module   string `json:"module,omitempty"`
	Lineno   uint32 `json:"lineno,omitempty"`
}

// Processed is a normalized event with every derived value the writer
// needs.
type Processed struct {
	Event        *ingestv1.Event
	ID           uuid.UUID // server-side UUIDv7; its timestamp is ReceivedAt
	EventID      uuid.UUID // client-side id
	OrgID        uuid.UUID
	OrgSlug      string
	ProjectID    uuid.UUID
	ProjectSlug  string
	ReceivedAt   time.Time
	OccurredAt   time.Time
	Level        ingestv1.Level
	Platform     string
	Environment  string
	Release      string
	CustomerKey  string
	CustomerName string
	CustomerAttr map[string]string
	TenantKey    string
	UserLabel    string
	UserHash     []byte
	ClientIP     netip.Addr
	ServerName   string
	Transaction  string
	URL          string
	UA           UserAgentInfo
	Browser      string
	OS           string
	Title        string
	Culprit      string
	Type         string
	Value        string
	Location     Location
	Grouping     Grouping
	Unhandled    bool
	Score        int16
	Tags         map[string]string
	Warnings     []string
	Payload      []byte
}

var payloadJSON = protojson.MarshalOptions{UseProtoNames: true}

// Normalize validates, trims, scrubs and enriches an event in place and
// computes everything derived from it. The event must not be used by the
// caller afterwards.
func Normalize(ev *ingestv1.Event, opt NormalizeOptions) (*Processed, error) {
	if ev == nil || (len(ev.GetExceptions()) == 0 && strings.TrimSpace(ev.GetMessage().GetFormatted()) == "" &&
		strings.TrimSpace(ev.GetMessage().GetTemplate()) == "" && !hasCrashedThread(ev)) {
		return nil, ErrEmptyEvent
	}
	settings := opt.Project.Settings
	privacy := settings.GetPrivacy()
	scrub := NewScrubber(privacy.GetScrubFields(), privacy.GetSafeFields())
	w := &warnings{}

	p := &Processed{
		Event: ev, OrgID: opt.Project.OrgID, OrgSlug: opt.Project.OrgSlug,
		ProjectID: opt.Project.ProjectID, ProjectSlug: opt.Project.ProjectSlug,
	}

	// Server id first: its embedded timestamp defines received_at, so that
	// the id alone identifies the daily partition of the row.
	p.ID = uuid.Must(uuid.NewV7())
	p.ReceivedAt = uuidV7Time(p.ID)
	if !opt.Now.IsZero() {
		p.ReceivedAt = opt.Now.UTC().Truncate(time.Millisecond)
		p.ID = uuidV7At(p.ReceivedAt)
	}

	if raw := strings.TrimSpace(ev.GetEventId()); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			w.addf("event_id: %q is not a UUID, replaced", truncateUTF8(raw, 64))
			id = uuid.New()
		}
		p.EventID = id
	} else {
		p.EventID = uuid.New()
	}
	ev.EventId = p.EventID.String()

	p.OccurredAt = p.ReceivedAt
	if ts := ev.GetTimestamp(); ts != nil && ts.IsValid() {
		t := ts.AsTime()
		switch {
		case t.After(p.ReceivedAt.Add(5 * time.Minute)):
			w.addf("timestamp: %s is in the future, replaced by receive time", t.Format(time.RFC3339))
		case opt.Retention > 0 && t.Before(p.ReceivedAt.Add(-opt.Retention)):
			w.addf("timestamp: %s is older than the retention window, replaced by receive time", t.Format(time.RFC3339))
		default:
			p.OccurredAt = t.UTC()
		}
	}
	ev.Timestamp = timestamppb.New(p.OccurredAt)

	p.Platform = strings.ToLower(w.clean("platform", ev.GetPlatform(), maxPlatform))
	if p.Platform == "" {
		p.Platform = strings.ToLower(opt.Project.Platform)
	}
	if p.Platform == "" {
		p.Platform = "other"
	}
	ev.Platform = p.Platform

	ev.Logger = w.clean("logger", ev.GetLogger(), maxLogger)
	ev.Transaction = w.clean("transaction", ev.GetTransaction(), maxTransaction)
	ev.ServerName = w.clean("server_name", ev.GetServerName(), maxServerName)
	ev.Release = w.clean("release", ev.GetRelease(), maxRelease)
	ev.Dist = w.clean("dist", ev.GetDist(), maxShortString)
	ev.Environment = strings.ToLower(w.clean("environment", ev.GetEnvironment(), maxShortString))
	if ev.Environment == "" {
		ev.Environment = "production"
	}
	p.Environment, p.Release, p.ServerName, p.Transaction = ev.Environment, ev.Release, ev.ServerName, ev.Transaction

	normalizeCustomer(ev, p, w)
	normalizeMessageField(ev, w, scrub)
	rules := InAppRules{Include: settings.GetInApp().GetInclude(), Exclude: settings.GetInApp().GetExclude()}
	normalizeExceptions(ev, p.Platform, rules, w, scrub)
	normalizeThreads(ev, p.Platform, rules, w, scrub)
	normalizeBreadcrumbs(ev, w, scrub)
	normalizeRequest(ev, w, scrub, privacy)
	normalizeUser(ev, w, opt.TransportIP)
	normalizeContexts(ev, w, scrub)
	normalizeTagsExtra(ev, w, scrub)

	p.UA = ParseUserAgent(UserAgentOf(ev))
	enrichFromUserAgent(ev, p)

	p.ClientIP = resolveClientIP(ev, p.Platform, opt.TransportIP)
	applyIPPrivacy(ev, p, privacy.GetIpStorage())

	// Level, unhandled flag.
	p.Unhandled = isUnhandled(ev)
	switch {
	case ev.GetLevel() == ingestv1.Level_LEVEL_UNSPECIFIED && (len(ev.GetExceptions()) > 0 || hasCrashedThread(ev)):
		ev.Level = ingestv1.Level_LEVEL_ERROR
	case ev.GetLevel() == ingestv1.Level_LEVEL_UNSPECIFIED:
		ev.Level = ingestv1.Level_LEVEL_INFO
	}
	if p.Unhandled && ev.GetLevel() < ingestv1.Level_LEVEL_ERROR {
		ev.Level = ingestv1.Level_LEVEL_ERROR
	}
	p.Level = ev.GetLevel()

	p.Grouping = ComputeGrouping(ev, p.Platform)
	describe(ev, p)
	p.UserLabel, p.UserHash = userIdentity(ev.GetUser())
	p.Browser = p.UA.BrowserLabel()
	if p.Browser == "" && ev.GetContexts().GetBrowser().GetName() != "" {
		b := ev.GetContexts().GetBrowser()
		major, _, _ := strings.Cut(b.GetVersion(), ".")
		p.Browser = strings.TrimSpace(b.GetName() + " " + major)
	}
	p.OS = p.UA.OSLabel()
	if p.OS == "" {
		o := ev.GetContexts().GetOs()
		p.OS = strings.TrimSpace(o.GetName() + " " + o.GetVersion())
	}
	if r := ev.GetRequest(); r != nil {
		p.URL = r.GetUrl()
	}
	p.Tags = facetTags(ev, p)
	p.Score = score(ev, p)

	payload, err := marshalBounded(ev, w)
	if err != nil {
		return nil, err
	}
	p.Payload = payload
	p.Warnings = w.list
	return p, nil
}

func hasCrashedThread(ev *ingestv1.Event) bool {
	for _, t := range ev.GetThreads() {
		if t.GetCrashed() && len(t.GetStacktrace().GetFrames()) > 0 {
			return true
		}
	}
	return false
}

// UserAgentOf returns the User-Agent of an event: the request header, else
// the browser context.
func UserAgentOf(ev *ingestv1.Event) string {
	if ua := headerValue(ev.GetRequest().GetHeaders(), "User-Agent"); ua != "" {
		return ua
	}
	return ev.GetContexts().GetBrowser().GetUserAgent()
}

// ReceivedAt returns the receive time embedded in a server-side event id,
// which identifies the daily partition of the event.
func ReceivedAt(id uuid.UUID) time.Time { return uuidV7Time(id) }

// uuidV7Time extracts the millisecond timestamp of a UUIDv7.
func uuidV7Time(id uuid.UUID) time.Time {
	ms := int64(binary.BigEndian.Uint64(append([]byte{0, 0}, id[:6]...)))
	return time.UnixMilli(ms).UTC()
}

// uuidV7At builds a random UUIDv7 with the given timestamp.
func uuidV7At(t time.Time) uuid.UUID {
	id := uuid.Must(uuid.NewV7())
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(t.UnixMilli()))
	copy(id[:6], b[2:])
	return id
}

func normalizeCustomer(ev *ingestv1.Event, p *Processed, w *warnings) {
	if c := ev.GetCustomer(); c != nil {
		c.Id = strings.ToLower(w.clean("customer.id", c.GetId(), maxShortString))
		c.Name = w.clean("customer.name", c.GetName(), maxName)
		c.Attributes = w.clampMap("customer.attributes", c.GetAttributes(), maxAttributes, maxShortString, maxName)
		if c.Id == "" && c.Name != "" {
			c.Id = strings.ToLower(truncateUTF8(c.Name, maxShortString))
		}
		p.CustomerKey, p.CustomerName, p.CustomerAttr = c.Id, c.Name, c.Attributes
	}
	if t := ev.GetTenant(); t != nil {
		t.Id = w.clean("tenant.id", t.GetId(), maxShortString)
		t.Name = w.clean("tenant.name", t.GetName(), maxName)
		p.TenantKey = t.Id
		if p.TenantKey == "" {
			p.TenantKey = t.Name
		}
	}
}

func normalizeMessageField(ev *ingestv1.Event, w *warnings, s *Scrubber) {
	m := ev.GetMessage()
	if m == nil {
		return
	}
	m.Formatted = s.Value(w.clamp("message.formatted", m.GetFormatted(), maxMessage))
	m.Template = w.clamp("message.template", m.GetTemplate(), maxTemplate)
	m.Params = w.clampList("message.params", m.GetParams(), maxParams, maxParamLen)
	for i, v := range m.Params {
		m.Params[i] = s.Value(v)
	}
	if m.Formatted == "" && m.Template != "" {
		m.Formatted = m.Template
	}
}

func normalizeExceptions(ev *ingestv1.Event, platform string, rules InAppRules, w *warnings, s *Scrubber) {
	excs := ev.GetExceptions()
	if len(excs) > maxExceptions {
		w.addf("exceptions: chain of %d trimmed to %d", len(excs), maxExceptions)
		excs = append(excs[:3:3], excs[len(excs)-(maxExceptions-3):]...)
		ev.Exceptions = excs
	}
	for i, ex := range excs {
		field := "exceptions[" + strconv.Itoa(i) + "]"
		ex.Type = w.clean(field+".type", ex.GetType(), maxTypeLen)
		ex.Value = s.Value(w.clamp(field+".value", ex.GetValue(), maxValueLen))
		ex.Module = w.clean(field+".module", ex.GetModule(), maxGenericField)
		if m := ex.GetMechanism(); m != nil {
			m.Type = w.clean(field+".mechanism.type", m.GetType(), maxShortString)
			m.Description = w.clamp(field+".mechanism.description", m.GetDescription(), maxParamLen)
			m.HelpLink = w.clamp(field+".mechanism.help_link", m.GetHelpLink(), maxParamLen)
			m.Data = w.clampMap(field+".mechanism.data", m.GetData(), maxDataEntries, maxShortString, maxParamLen)
		}
		if st := ex.GetStacktrace(); st != nil {
			normalizeStacktrace(st, field, platform, rules, w, s)
		}
	}
}

func normalizeThreads(ev *ingestv1.Event, platform string, rules InAppRules, w *warnings, s *Scrubber) {
	if len(ev.GetThreads()) > maxThreads {
		w.addf("threads: %d trimmed to %d", len(ev.Threads), maxThreads)
		ev.Threads = ev.Threads[:maxThreads]
	}
	for i, t := range ev.GetThreads() {
		field := "threads[" + strconv.Itoa(i) + "]"
		t.Id = w.clean(field+".id", t.GetId(), maxShortString)
		t.Name = w.clean(field+".name", t.GetName(), maxGenericField)
		t.State = w.clean(field+".state", t.GetState(), maxShortString)
		if st := t.GetStacktrace(); st != nil {
			normalizeStacktrace(st, field, platform, rules, w, s)
		}
	}
}

func normalizeStacktrace(st *ingestv1.Stacktrace, field, platform string, rules InAppRules, w *warnings, s *Scrubber) {
	frames := st.GetFrames()
	if len(frames) > maxFrames {
		w.addf("%s.stacktrace: %d frames trimmed to %d", field, len(frames), maxFrames)
		keepNew := maxFrames - keepOldFrames
		frames = append(frames[:keepOldFrames:keepOldFrames], frames[len(frames)-keepNew:]...)
		st.Frames = frames
	}
	for _, f := range frames {
		f.Function = w.clean(field+".frame.function", f.GetFunction(), maxFrameField)
		f.RawFunction = w.clean(field+".frame.raw_function", f.GetRawFunction(), maxFrameField)
		f.Module = w.clean(field+".frame.module", f.GetModule(), maxFrameField)
		f.Package = w.clean(field+".frame.package", f.GetPackage(), maxFrameField)
		f.Filename = w.clean(field+".frame.filename", f.GetFilename(), maxFilename)
		f.AbsPath = w.clean(field+".frame.abs_path", f.GetAbsPath(), maxAbsPath)
		f.ContextLine = w.clamp(field+".frame.context_line", strings.TrimRight(f.GetContextLine(), "\r\n"), maxContextLine)
		f.PreContext = w.clampList(field+".frame.pre_context", tail(f.GetPreContext(), maxContextLines), maxContextLines, maxContextLine)
		f.PostContext = w.clampList(field+".frame.post_context", f.GetPostContext(), maxContextLines, maxContextLine)
		f.InstructionAddr = w.clean(field+".frame.instruction_addr", f.GetInstructionAddr(), maxShortString)
		f.Symbol = w.clean(field+".frame.symbol", f.GetSymbol(), maxFrameField)
		f.Platform = strings.ToLower(w.clean(field+".frame.platform", f.GetPlatform(), maxPlatform))
		if vars := f.GetVars(); len(vars) > 0 {
			f.Vars = w.clampMap(field+".frame.vars", vars, maxFrameVars, maxShortString, maxVarLen)
			s.Map(f.Vars)
		}
	}
	resolveInApp(frames, platform, rules)
	if len(st.GetRegisters()) > 0 {
		st.Registers = w.clampMap(field+".stacktrace.registers", st.GetRegisters(), 64, maxShortString, maxShortString)
	}
}

func tail(l []string, n int) []string {
	if len(l) > n {
		return l[len(l)-n:]
	}
	return l
}

func normalizeBreadcrumbs(ev *ingestv1.Event, w *warnings, s *Scrubber) {
	crumbs := ev.GetBreadcrumbs()
	if len(crumbs) > maxBreadcrumbs {
		w.addf("breadcrumbs: %d trimmed to the newest %d", len(crumbs), maxBreadcrumbs)
		crumbs = crumbs[len(crumbs)-maxBreadcrumbs:]
		ev.Breadcrumbs = crumbs
	}
	for _, b := range crumbs {
		b.Type = strings.ToLower(w.clean("breadcrumbs.type", b.GetType(), maxShortString))
		b.Category = w.clean("breadcrumbs.category", b.GetCategory(), maxLogger)
		b.Message = s.Value(w.clamp("breadcrumbs.message", b.GetMessage(), maxMessage))
		if d := b.GetData(); d != nil {
			s.Struct(d)
			if proto.Size(d) > maxCrumbData {
				w.addf("breadcrumbs.data: dropped a %d-byte payload", proto.Size(d))
				b.Data = nil
			}
		}
	}
}

func normalizeRequest(ev *ingestv1.Event, w *warnings, s *Scrubber, privacy *apiv1.PrivacySettings) {
	r := ev.GetRequest()
	if r == nil {
		return
	}
	r.Method = strings.ToUpper(w.clean("request.method", r.GetMethod(), 16))
	u := w.clean("request.url", r.GetUrl(), maxURL)
	if base, q, ok := strings.Cut(u, "?"); ok {
		u = base
		if r.QueryString == "" {
			r.QueryString = q
		}
	}
	if base, frag, ok := strings.Cut(u, "#"); ok {
		u = base
		if r.Fragment == "" {
			r.Fragment = frag
		}
	}
	r.Url = s.Value(u)
	r.Route = w.clean("request.route", r.GetRoute(), maxRoute)
	r.QueryString = s.QueryString(w.clamp("request.query_string", strings.TrimPrefix(r.GetQueryString(), "?"), maxQuery))
	r.Fragment = w.clamp("request.fragment", r.GetFragment(), maxParamLen)
	r.Headers = scrubHeaders("request.headers", r.GetHeaders(), w, s)
	contentType := headerValue(r.Headers, "Content-Type")
	if len(r.GetCookies()) > 0 {
		r.Cookies = w.clampMap("request.cookies", r.GetCookies(), maxCookies, maxShortString, maxHeaderValue)
		if privacy.GetStoreCookieValues() {
			s.Map(r.Cookies)
		} else {
			for k := range r.Cookies {
				r.Cookies[k] = Filtered
			}
		}
	}
	if r.GetBody() != "" {
		if r.BodySize == 0 {
			r.BodySize = int64(len(r.Body))
		}
		if privacy == nil || privacy.GetStoreRequestBodies() {
			r.Body = s.Body(w.clamp("request.body", r.GetBody(), maxBody), contentType)
		} else {
			r.Body = ""
		}
	}
	r.Protocol = w.clean("request.protocol", r.GetProtocol(), maxShortString)
	r.RemoteAddr = w.clean("request.remote_addr", r.GetRemoteAddr(), maxShortString)
	if len(r.GetEnv()) > 0 {
		r.Env = w.clampMap("request.env", r.GetEnv(), maxEnvVars, maxShortString, maxParamLen)
		s.Map(r.Env)
	}
	if resp := ev.GetResponse(); resp != nil {
		resp.Headers = scrubHeaders("response.headers", resp.GetHeaders(), w, s)
	}
}

func scrubHeaders(field string, hs []*ingestv1.Header, w *warnings, s *Scrubber) []*ingestv1.Header {
	if len(hs) > maxHeaders {
		w.addf("%s: %d trimmed to %d", field, len(hs), maxHeaders)
		hs = hs[:maxHeaders]
	}
	out := hs[:0]
	for _, h := range hs {
		if h == nil || strings.TrimSpace(h.GetName()) == "" {
			continue
		}
		h.Name = w.clean(field+".name", h.GetName(), maxShortString*2)
		if s.SensitiveHeader(h.Name) {
			h.Value = Filtered
		} else {
			h.Value = s.Value(w.clamp(field+"."+h.Name, h.GetValue(), maxHeaderValue))
		}
		out = append(out, h)
	}
	return out
}

func headerValue(hs []*ingestv1.Header, name string) string {
	for _, h := range hs {
		if strings.EqualFold(h.GetName(), name) {
			return h.GetValue()
		}
	}
	return ""
}

func normalizeUser(ev *ingestv1.Event, w *warnings, transport netip.Addr) {
	u := ev.GetUser()
	if u == nil {
		return
	}
	u.Id = w.clean("user.id", u.GetId(), maxLogger)
	u.Username = w.clean("user.username", u.GetUsername(), maxLogger)
	u.Email = w.clean("user.email", u.GetEmail(), maxGenericField)
	u.Name = w.clean("user.name", u.GetName(), maxName)
	u.Segment = w.clean("user.segment", u.GetSegment(), maxShortString)
	u.Attributes = w.clampMap("user.attributes", u.GetAttributes(), maxAttributes, maxShortString, maxName)
	ip := strings.TrimSpace(u.GetIpAddress())
	switch {
	case ip == "{{auto}}":
		if transport.IsValid() {
			ip = transport.String()
		} else {
			ip = ""
		}
	case ip != "":
		if a, err := netip.ParseAddr(ip); err == nil {
			ip = a.Unmap().String()
		} else {
			w.addf("user.ip_address: %q is not an IP address, dropped", truncateUTF8(ip, 64))
			ip = ""
		}
	}
	u.IpAddress = ip
	if g := u.GetGeo(); g != nil {
		g.CountryCode = strings.ToUpper(w.clean("user.geo.country_code", g.GetCountryCode(), 2))
		g.Region = w.clean("user.geo.region", g.GetRegion(), maxName)
		g.City = w.clean("user.geo.city", g.GetCity(), maxName)
	}
}

func normalizeContexts(ev *ingestv1.Event, w *warnings, s *Scrubber) {
	c := ev.GetContexts()
	if c == nil {
		return
	}
	g := func(field, v string) string { return w.clean("contexts."+field, v, maxGenericField) }
	if d := c.GetDevice(); d != nil {
		d.Name, d.Family, d.Model, d.ModelId = g("device.name", d.Name), g("device.family", d.Family), g("device.model", d.Model), g("device.model_id", d.ModelId)
		d.Manufacturer, d.Brand, d.Arch = g("device.manufacturer", d.Manufacturer), g("device.brand", d.Brand), g("device.arch", d.Arch)
		d.DeviceType, d.Orientation = strings.ToLower(g("device.device_type", d.DeviceType)), g("device.orientation", d.Orientation)
		if cpu := d.GetCpu(); cpu != nil {
			cpu.Model = g("device.cpu.model", cpu.Model)
			if len(cpu.LoadAverage) > 3 {
				cpu.LoadAverage = cpu.LoadAverage[:3]
			}
		}
	}
	if o := c.GetOs(); o != nil {
		o.Name, o.Version, o.Build, o.KernelVersion = g("os.name", o.Name), g("os.version", o.Version), g("os.build", o.Build), g("os.kernel_version", o.KernelVersion)
		o.RawDescription, o.Distribution = g("os.raw_description", o.RawDescription), g("os.distribution", o.Distribution)
	}
	if r := c.GetRuntime(); r != nil {
		r.Name, r.Version, r.Build, r.RawDescription = g("runtime.name", r.Name), g("runtime.version", r.Version), g("runtime.build", r.Build), g("runtime.raw_description", r.RawDescription)
	}
	if b := c.GetBrowser(); b != nil {
		b.Name, b.Version = g("browser.name", b.Name), g("browser.version", b.Version)
		b.UserAgent = w.clamp("contexts.browser.user_agent", b.UserAgent, maxHeaderValue)
	}
	if a := c.GetApp(); a != nil {
		a.Id, a.Name, a.Version, a.Build = g("app.id", a.Id), g("app.name", a.Name), g("app.version", a.Version), g("app.build", a.Build)
	}
	if p := c.GetProcess(); p != nil {
		p.Name = g("process.name", p.Name)
		p.CommandLine = s.Value(w.clamp("contexts.process.command_line", p.CommandLine, maxCommandLine))
		p.WorkingDirectory = w.clamp("contexts.process.working_directory", p.WorkingDirectory, maxAbsPath)
	}
	if gpu := c.GetGpu(); gpu != nil {
		gpu.Name, gpu.VendorName, gpu.ApiType, gpu.DriverVersion = g("gpu.name", gpu.Name), g("gpu.vendor_name", gpu.VendorName), g("gpu.api_type", gpu.ApiType), g("gpu.driver_version", gpu.DriverVersion)
	}
	if cu := c.GetCulture(); cu != nil {
		cu.Locale, cu.Timezone, cu.Calendar = g("culture.locale", cu.Locale), g("culture.timezone", cu.Timezone), g("culture.calendar", cu.Calendar)
	}
	if t := c.GetTrace(); t != nil {
		t.TraceId, t.SpanId, t.ParentSpanId = strings.ToLower(g("trace.trace_id", t.TraceId)), strings.ToLower(g("trace.span_id", t.SpanId)), strings.ToLower(g("trace.parent_span_id", t.ParentSpanId))
		t.Op, t.Status = g("trace.op", t.Op), g("trace.status", t.Status)
	}
	if d := c.GetDatabase(); d != nil {
		d.System, d.Name, d.Operation, d.Host, d.ErrorCode = strings.ToLower(g("database.system", d.System)), g("database.name", d.Name), g("database.operation", d.Operation), g("database.host", d.Host), g("database.error_code", d.ErrorCode)
		d.Statement = s.Value(w.clamp("contexts.database.statement", d.Statement, maxDBStatement))
		d.Parameters = w.clampList("contexts.database.parameters", d.Parameters, maxDBParams, maxParamLen)
		for i, v := range d.Parameters {
			d.Parameters[i] = s.Value(v)
		}
	}
	if cl := c.GetCloud(); cl != nil {
		cl.Provider, cl.Region, cl.AvailabilityZone, cl.AccountId = g("cloud.provider", cl.Provider), g("cloud.region", cl.Region), g("cloud.availability_zone", cl.AvailabilityZone), g("cloud.account_id", cl.AccountId)
		cl.InstanceId, cl.Platform, cl.Cluster, cl.Namespace = g("cloud.instance_id", cl.InstanceId), g("cloud.platform", cl.Platform), g("cloud.cluster", cl.Cluster), g("cloud.namespace", cl.Namespace)
		cl.Pod, cl.Container, cl.Node = g("cloud.pod", cl.Pod), g("cloud.container", cl.Container), g("cloud.node", cl.Node)
	}
	if custom := c.GetCustom(); len(custom) > 0 {
		keys := sortedKeys(custom)
		for i, k := range keys {
			if i >= maxCustomBlocks {
				delete(custom, k)
				continue
			}
			st := custom[k]
			s.Struct(st)
			if proto.Size(st) > maxCustomBlock {
				w.addf("contexts.custom.%s: dropped a %d-byte block", k, proto.Size(st))
				delete(custom, k)
			}
		}
		if len(keys) > maxCustomBlocks {
			w.addf("contexts.custom: %d blocks dropped (max %d)", len(keys)-maxCustomBlocks, maxCustomBlocks)
		}
	}
}

var reTagKey = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

func normalizeTagsExtra(ev *ingestv1.Event, w *warnings, s *Scrubber) {
	if tags := ev.GetTags(); len(tags) > 0 {
		out := make(map[string]string, len(tags))
		dropped := 0
		for _, k := range sortedKeys(tags) {
			if !reTagKey.MatchString(k) || strings.HasPrefix(strings.ToLower(k), "izci.") {
				dropped++
				continue
			}
			if len(out) >= maxTags {
				dropped++
				continue
			}
			v := strings.TrimSpace(tags[k])
			if v == "" {
				continue
			}
			if s.SensitiveKey(k) {
				v = Filtered
			}
			out[k] = w.clamp("tags."+k, v, maxTagValue)
		}
		if dropped > 0 {
			w.addf("tags: %d invalid, reserved or excess tags dropped", dropped)
		}
		ev.Tags = out
	}
	if ex := ev.GetExtra(); ex != nil {
		s.Struct(ex)
		if proto.Size(ex) > maxExtra {
			w.addf("extra: dropped a %d-byte payload (max %d)", proto.Size(ex), maxExtra)
			ev.Extra = nil
		}
	}
	if len(ev.GetFingerprint()) > 0 {
		ev.Fingerprint = w.clampList("fingerprint", ev.GetFingerprint(), maxFingerprint, maxGenericField)
	}
	if sdk := ev.GetSdk(); sdk != nil {
		sdk.Name, sdk.Version = w.clean("sdk.name", sdk.Name, maxShortString*2), w.clean("sdk.version", sdk.Version, maxShortString)
		sdk.Integrations = w.clampList("sdk.integrations", sdk.Integrations, 50, maxShortString)
		if len(sdk.Packages) > 20 {
			sdk.Packages = sdk.Packages[:20]
		}
	}
	if len(ev.GetModules()) > 0 {
		ev.Modules = w.clampMap("modules", ev.GetModules(), maxModules, maxGenericField, maxShortString*2)
	}
}

// enrichFromUserAgent fills browser (and, for browser platforms, os and
// device) contexts from the parsed User-Agent. Server platforms keep their own
// os/device contexts: the header describes the end user's machine there.
func enrichFromUserAgent(ev *ingestv1.Event, p *Processed) {
	if p.UA.Browser == "" && p.UA.OS == "" {
		return
	}
	if ev.Contexts == nil {
		ev.Contexts = &ingestv1.Contexts{}
	}
	c := ev.Contexts
	if p.UA.Browser != "" && c.GetBrowser().GetName() == "" {
		if c.Browser == nil {
			c.Browser = &ingestv1.BrowserContext{}
		}
		c.Browser.Name, c.Browser.Version = p.UA.Browser, p.UA.BrowserVersion
	}
	if p.Platform != "javascript" {
		return
	}
	if p.UA.OS != "" && c.GetOs().GetName() == "" {
		c.Os = &ingestv1.OsContext{Name: p.UA.OS, Version: p.UA.OSVersion}
	}
	if c.Device == nil {
		c.Device = &ingestv1.DeviceContext{}
	}
	if c.Device.DeviceType == "" {
		c.Device.DeviceType = p.UA.DeviceType
	}
	if c.Device.Model == "" {
		c.Device.Model = p.UA.Device
	}
}

// resolveClientIP picks the end user's address: an explicit user IP, then
// the address the application saw, then — for browser platforms, where the
// sender is the end user — the transport address.
func resolveClientIP(ev *ingestv1.Event, platform string, transport netip.Addr) netip.Addr {
	if a, err := netip.ParseAddr(ev.GetUser().GetIpAddress()); err == nil {
		return a.Unmap()
	}
	r := ev.GetRequest()
	for _, v := range []string{r.GetRemoteAddr(), r.GetEnv()["REMOTE_ADDR"]} {
		if a := parseAddrLoose(v); a.IsValid() {
			return a
		}
	}
	if platform == "javascript" || platform == "dart" || platform == "cocoa" || platform == "android" {
		return transport
	}
	return netip.Addr{}
}

func parseAddrLoose(s string) netip.Addr {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return a.Unmap()
	}
	return netip.Addr{}
}

// applyIPPrivacy stores, anonymizes or removes IP addresses everywhere in
// the event according to the project setting.
func applyIPPrivacy(ev *ingestv1.Event, p *Processed, mode apiv1.IpStorage) {
	var transform func(netip.Addr) netip.Addr
	switch mode {
	case apiv1.IpStorage_IP_STORAGE_ANONYMIZED:
		transform = clientip.Anonymize
	case apiv1.IpStorage_IP_STORAGE_NONE:
		transform = func(netip.Addr) netip.Addr { return netip.Addr{} }
	default:
		return // full storage: keep addresses as they are
	}
	p.ClientIP = transform(p.ClientIP)
	str := func(v string) string {
		if a := parseAddrLoose(v); a.IsValid() {
			if t := transform(a); t.IsValid() {
				return t.String()
			}
			return ""
		}
		return v
	}
	if u := ev.GetUser(); u != nil {
		u.IpAddress = str(u.IpAddress)
	}
	if r := ev.GetRequest(); r != nil {
		r.RemoteAddr = str(r.RemoteAddr)
		if v, ok := r.Env["REMOTE_ADDR"]; ok {
			r.Env["REMOTE_ADDR"] = str(v)
		}
		for _, h := range r.Headers {
			switch strings.ToLower(h.Name) {
			case "x-forwarded-for", "x-real-ip", "cf-connecting-ip", "true-client-ip", "forwarded":
				h.Value = Filtered
			}
		}
	}
}

func isUnhandled(ev *ingestv1.Event) bool {
	for _, ex := range ev.GetExceptions() {
		if m := ex.GetMechanism(); m != nil && m.Handled != nil && !m.GetHandled() {
			return true
		}
	}
	return false
}

// describe derives title, culprit, type, value and location.
func describe(ev *ingestv1.Event, p *Processed) {
	excs := ev.GetExceptions()
	var frames []*ingestv1.Frame
	if len(excs) > 0 {
		primary := excs[len(excs)-1]
		p.Type = primary.GetType()
		p.Value = firstLine(primary.GetValue())
		switch {
		case p.Type != "" && p.Value != "":
			p.Title = p.Type + ": " + p.Value
		case p.Type != "":
			p.Title = p.Type
		default:
			p.Title = p.Value
		}
		frames = primary.GetStacktrace().GetFrames()
		if len(frames) == 0 {
			for i := len(excs) - 2; i >= 0 && len(frames) == 0; i-- {
				frames = excs[i].GetStacktrace().GetFrames()
			}
		}
	} else {
		p.Value = firstLine(ev.GetMessage().GetFormatted())
		p.Title = p.Value
	}
	if len(frames) == 0 {
		for _, t := range ev.GetThreads() {
			if t.GetCrashed() {
				frames = t.GetStacktrace().GetFrames()
				break
			}
		}
	}
	if p.Title == "" {
		p.Title = firstLine(ev.GetMessage().GetFormatted())
	}
	if p.Title == "" {
		p.Title = "<unlabeled event>"
	}
	p.Title = truncateUTF8(p.Title, 300)
	p.Value = truncateUTF8(p.Value, 1024)

	var top *ingestv1.Frame
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].GetInApp() {
			top = frames[i]
			break
		}
	}
	if top == nil && len(frames) > 0 {
		top = frames[len(frames)-1]
	}
	if top != nil {
		p.Location = Location{Filename: top.GetFilename(), Function: top.GetFunction(), Module: top.GetModule(), Lineno: top.GetLineno()}
		if p.Location.Filename == "" {
			p.Location.Filename = top.GetAbsPath()
		}
		file := normalizeDisplayFile(p.Location.Filename)
		fn := top.GetFunction()
		switch fn {
		case "?", "<anonymous>", "<unknown>", "Object.<anonymous>":
			fn = "" // top-level module code: the file says more
		}
		switch {
		case fn != "" && file != "":
			p.Culprit = fn + " in " + file
		case fn != "":
			p.Culprit = fn
		default:
			p.Culprit = file
		}
	}
	if p.Culprit == "" {
		p.Culprit = ev.GetTransaction()
	}
	if p.Culprit == "" {
		p.Culprit = ev.GetLogger()
	}
	p.Culprit = truncateUTF8(p.Culprit, 300)
}

func normalizeDisplayFile(f string) string {
	if f == "" {
		return ""
	}
	if u, err := url.Parse(f); err == nil && u.Scheme != "" && u.Host != "" {
		f = u.Path
	}
	f = strings.ReplaceAll(f, "\\", "/")
	if i := strings.LastIndexByte(f, '/'); i >= 0 {
		return f[i+1:]
	}
	return f
}

// userIdentity returns a display label and an 8-byte hash identifying the
// user for distinct counts.
func userIdentity(u *ingestv1.User) (string, []byte) {
	if u == nil {
		return "", nil
	}
	var label, key string
	switch {
	case u.GetId() != "":
		key = "id:" + u.GetId()
	case u.GetEmail() != "":
		key = "email:" + strings.ToLower(u.GetEmail())
	case u.GetUsername() != "":
		key = "username:" + u.GetUsername()
	case u.GetIpAddress() != "":
		key = "ip:" + u.GetIpAddress()
	default:
		return "", nil
	}
	for _, v := range []string{u.GetEmail(), u.GetUsername(), u.GetName(), u.GetId(), u.GetIpAddress()} {
		if v != "" {
			label = v
			break
		}
	}
	sum := sha256.Sum256([]byte(key))
	return truncateUTF8(label, maxName), sum[:8]
}

// facetTags builds the indexed tags of an event: built-in facets plus the
// custom tags (which cannot override built-in keys).
func facetTags(ev *ingestv1.Event, p *Processed) map[string]string {
	t := make(map[string]string, 16+len(ev.GetTags()))
	set := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			t[k] = truncateUTF8(v, maxTagValue)
		}
	}
	set("level", levelName(p.Level))
	set("environment", p.Environment)
	set("release", p.Release)
	set("customer", p.CustomerKey)
	set("tenant", p.TenantKey)
	set("browser", p.Browser)
	set("os", p.OS)
	device := ev.GetContexts().GetDevice().GetModel()
	if device == "" {
		device = ev.GetContexts().GetDevice().GetDeviceType()
	}
	if device == "" {
		device = p.UA.DeviceType
	}
	set("device", device)
	if r := ev.GetContexts().GetRuntime(); r.GetName() != "" {
		set("runtime", r.GetName()+" "+r.GetVersion())
	}
	set("server_name", p.ServerName)
	set("transaction", p.Transaction)
	set("user", p.UserLabel)
	set("logger", ev.GetLogger())
	for _, ex := range ev.GetExceptions() {
		if m := ex.GetMechanism(); m != nil && m.Handled != nil {
			if m.GetHandled() {
				set("handled", "yes")
			} else {
				set("handled", "no")
			}
		}
	}
	for k, v := range ev.GetTags() {
		if _, builtin := t[k]; !builtin {
			set(k, v)
		}
	}
	return t
}

func levelName(l ingestv1.Level) string {
	switch l {
	case ingestv1.Level_LEVEL_DEBUG:
		return "debug"
	case ingestv1.Level_LEVEL_INFO:
		return "info"
	case ingestv1.Level_LEVEL_WARNING:
		return "warning"
	case ingestv1.Level_LEVEL_ERROR:
		return "error"
	case ingestv1.Level_LEVEL_FATAL:
		return "fatal"
	}
	return ""
}

// score rates how informative an event is (used to pick an issue's
// recommended event).
func score(ev *ingestv1.Event, p *Processed) int16 {
	var s int16
	for _, ex := range ev.GetExceptions() {
		for _, f := range ex.GetStacktrace().GetFrames() {
			if f.GetInApp() {
				s += 4
				if f.GetContextLine() != "" {
					s++
				}
				goto frames
			}
		}
	}
frames:
	if ev.GetRequest() != nil {
		s += 2
	}
	if p.UserLabel != "" {
		s += 2
	}
	if len(ev.GetBreadcrumbs()) > 0 {
		s++
	}
	if ev.GetContexts() != nil {
		s++
	}
	return s
}

// marshalBounded encodes the event, shedding optional bulk (extra, threads,
// breadcrumbs, frame variables) while it exceeds the event size limit.
func marshalBounded(ev *ingestv1.Event, w *warnings) ([]byte, error) {
	out, err := payloadJSON.Marshal(ev)
	if err != nil {
		return nil, err
	}
	shed := []struct {
		name string
		drop func()
	}{
		{"extra", func() { ev.Extra = nil }},
		{"threads", func() { ev.Threads = nil }},
		{"breadcrumbs", func() { ev.Breadcrumbs = ev.Breadcrumbs[max(0, len(ev.Breadcrumbs)-20):] }},
		{"frame variables and source context", func() {
			for _, ex := range ev.Exceptions {
				for _, f := range ex.GetStacktrace().GetFrames() {
					f.Vars, f.PreContext, f.PostContext = nil, nil, nil
				}
			}
		}},
		{"request body", func() {
			if ev.Request != nil {
				ev.Request.Body = ""
			}
		}},
		{"contexts.custom", func() {
			if ev.Contexts != nil {
				ev.Contexts.Custom = nil
			}
		}},
	}
	for _, step := range shed {
		if len(out) <= maxEventBytes {
			break
		}
		step.drop()
		w.addf("event: %s dropped to stay under %d bytes", step.name, maxEventBytes)
		if out, err = payloadJSON.Marshal(ev); err != nil {
			return nil, err
		}
	}
	return out, nil
}
