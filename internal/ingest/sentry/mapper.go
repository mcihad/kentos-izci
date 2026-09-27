// Package sentry accepts events from unmodified Sentry SDKs.
//
// Applications point their DSN at izci ("https://izk_…@izci.example.com/42")
// and keep using the Sentry SDK of their platform. The envelope and legacy
// store endpoints decode the Sentry event format, map it onto
// izci.ingest.v1.Event and hand it to the regular ingest service, so grouping,
// alerts and AI analysis are identical for both transports. Item types izci
// does not store (transactions, sessions, replays, attachments, …) are
// accepted and dropped.
package sentry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// sentryEvent is the subset of the Sentry event payload izci understands.
// Polymorphic fields are kept raw and decoded by helpers.
type sentryEvent struct {
	EventID     string                     `json:"event_id"`
	Timestamp   json.RawMessage            `json:"timestamp"`
	Platform    string                     `json:"platform"`
	Level       string                     `json:"level"`
	Logger      string                     `json:"logger"`
	Transaction string                     `json:"transaction"`
	ServerName  string                     `json:"server_name"`
	Release     string                     `json:"release"`
	Dist        string                     `json:"dist"`
	Environment string                     `json:"environment"`
	Message     json.RawMessage            `json:"message"`
	LogEntry    *logEntry                  `json:"logentry"`
	Exception   json.RawMessage            `json:"exception"`
	Threads     json.RawMessage            `json:"threads"`
	Breadcrumbs json.RawMessage            `json:"breadcrumbs"`
	Request     *request                   `json:"request"`
	User        map[string]any             `json:"user"`
	Contexts    map[string]json.RawMessage `json:"contexts"`
	Tags        json.RawMessage            `json:"tags"`
	Extra       map[string]any             `json:"extra"`
	Fingerprint []string                   `json:"fingerprint"`
	SDK         *sdkInfo                   `json:"sdk"`
	Modules     map[string]any             `json:"modules"`
	DebugMeta   *debugMeta                 `json:"debug_meta"`
}

type debugMeta struct {
	Images []debugImage `json:"images"`
}

// debugImage is an entry of debug_meta.images: a script with a debug id
// ("sourcemap") or a native image.
type debugImage struct {
	Type      string          `json:"type"`
	DebugID   string          `json:"debug_id"`
	UUID      string          `json:"uuid"` // older Apple SDKs
	CodeID    string          `json:"code_id"`
	CodeFile  string          `json:"code_file"`
	ImageAddr json.RawMessage `json:"image_addr"`
	ImageSize json.Number     `json:"image_size"`
	Arch      string          `json:"arch"`
}

type logEntry struct {
	Formatted string `json:"formatted"`
	Message   string `json:"message"`
	Params    []any  `json:"params"`
}

type exception struct {
	Type       string      `json:"type"`
	Value      string      `json:"value"`
	Module     string      `json:"module"`
	ThreadID   any         `json:"thread_id"`
	Mechanism  *mechanism  `json:"mechanism"`
	Stacktrace *stacktrace `json:"stacktrace"`
}

type mechanism struct {
	Type             string         `json:"type"`
	Handled          *bool          `json:"handled"`
	Synthetic        bool           `json:"synthetic"`
	Description      string         `json:"description"`
	HelpLink         string         `json:"help_link"`
	Data             map[string]any `json:"data"`
	ExceptionID      *int32         `json:"exception_id"`
	ParentID         *int32         `json:"parent_id"`
	IsExceptionGroup bool           `json:"is_exception_group"`
	Source           string         `json:"source"`
}

type stacktrace struct {
	Frames    []frame        `json:"frames"`
	Registers map[string]any `json:"registers"`
}

type frame struct {
	Function        string         `json:"function"`
	RawFunction     string         `json:"raw_function"`
	Module          string         `json:"module"`
	Package         string         `json:"package"`
	Filename        string         `json:"filename"`
	AbsPath         string         `json:"abs_path"`
	Lineno          json.Number    `json:"lineno"`
	Colno           json.Number    `json:"colno"`
	ContextLine     string         `json:"context_line"`
	PreContext      []string       `json:"pre_context"`
	PostContext     []string       `json:"post_context"`
	InApp           *bool          `json:"in_app"`
	Vars            map[string]any `json:"vars"`
	InstructionAddr string         `json:"instruction_addr"`
	Symbol          string         `json:"symbol"`
	Platform        string         `json:"platform"`
}

type thread struct {
	ID         any         `json:"id"`
	Name       string      `json:"name"`
	Crashed    bool        `json:"crashed"`
	Current    bool        `json:"current"`
	Main       bool        `json:"main"`
	State      string      `json:"state"`
	Stacktrace *stacktrace `json:"stacktrace"`
}

type breadcrumb struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Type      string          `json:"type"`
	Category  string          `json:"category"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Data      map[string]any  `json:"data"`
}

type request struct {
	Method      string          `json:"method"`
	URL         string          `json:"url"`
	QueryString json.RawMessage `json:"query_string"`
	Data        json.RawMessage `json:"data"`
	Cookies     json.RawMessage `json:"cookies"`
	Headers     json.RawMessage `json:"headers"`
	Env         map[string]any  `json:"env"`
	Fragment    string          `json:"fragment"`
}

type sdkInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Integrations []string `json:"integrations"`
	Packages     []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"packages"`
}

// Decode parses a Sentry event JSON document into an izci event.
func Decode(data []byte) (*ingestv1.Event, error) {
	var se sentryEvent
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&se); err != nil {
		return nil, fmt.Errorf("invalid Sentry event JSON: %w", err)
	}
	return convert(&se), nil
}

func convert(se *sentryEvent) *ingestv1.Event {
	ev := &ingestv1.Event{
		EventId:     se.EventID,
		Platform:    platform(se.Platform, se.SDK),
		Level:       level(se.Level),
		Logger:      se.Logger,
		Transaction: se.Transaction,
		ServerName:  se.ServerName,
		Release:     se.Release,
		Dist:        se.Dist,
		Environment: se.Environment,
		Fingerprint: se.Fingerprint,
	}
	if t, ok := parseTime(se.Timestamp); ok {
		ev.Timestamp = timestamppb.New(t)
	}

	// Message: "message" may be a string or an object; "logentry" carries the
	// template.
	if m := message(se.Message, se.LogEntry); m != nil {
		ev.Message = m
	}

	for _, ex := range listOf[exception](se.Exception) {
		ev.Exceptions = append(ev.Exceptions, convertException(ex))
	}
	for _, th := range listOf[thread](se.Threads) {
		t := &ingestv1.Thread{Id: anyString(th.ID), Name: th.Name, Crashed: th.Crashed, Current: th.Current, Main: th.Main, State: th.State}
		if th.Stacktrace != nil {
			t.Stacktrace = convertStacktrace(th.Stacktrace)
		}
		ev.Threads = append(ev.Threads, t)
	}
	for _, b := range listOf[breadcrumb](se.Breadcrumbs) {
		bc := &ingestv1.Breadcrumb{Type: b.Type, Category: b.Category, Level: level(b.Level), Message: b.Message}
		if t, ok := parseTime(b.Timestamp); ok {
			bc.Timestamp = timestamppb.New(t)
		}
		if len(b.Data) > 0 {
			bc.Data, _ = structpb.NewStruct(jsonSafe(b.Data))
		}
		ev.Breadcrumbs = append(ev.Breadcrumbs, bc)
	}
	if se.Request != nil {
		if req := convertRequest(se.Request); !proto.Equal(req, &ingestv1.Request{}) {
			ev.Request = req
		}
	}
	if len(se.User) > 0 {
		ev.User = convertUser(se.User)
	}

	tags := pairs(se.Tags)
	ev.Contexts = convertContexts(se.Contexts, ev)
	applyIzciMapping(ev, tags, se.Contexts)
	if len(tags) > 0 {
		ev.Tags = tags
	}
	if len(se.Extra) > 0 {
		ev.Extra, _ = structpb.NewStruct(jsonSafe(se.Extra))
	}
	if se.SDK != nil {
		ev.Sdk = &ingestv1.Sdk{Name: se.SDK.Name, Version: se.SDK.Version, Integrations: se.SDK.Integrations}
		for _, p := range se.SDK.Packages {
			ev.Sdk.Packages = append(ev.Sdk.Packages, &ingestv1.SdkPackage{Name: p.Name, Version: p.Version})
		}
	}
	if len(se.Modules) > 0 {
		ev.Modules = map[string]string{}
		for k, v := range se.Modules {
			ev.Modules[k] = anyString(v)
		}
	}
	if se.DebugMeta != nil {
		for _, im := range se.DebugMeta.Images {
			img := &ingestv1.DebugImage{Type: im.Type, DebugId: im.DebugID, CodeId: im.CodeID, CodeFile: im.CodeFile,
				ImageAddr: address(im.ImageAddr), Arch: im.Arch}
			if img.DebugId == "" {
				img.DebugId = im.UUID
			}
			if n := number(im.ImageSize); n > 0 {
				img.ImageSize = uint64(n)
			}
			ev.DebugImages = append(ev.DebugImages, img)
		}
	}
	return ev
}

// address reads an address given as a hex string or as a number.
func address(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		return rawString(raw)
	}
	n, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return ""
	}
	return "0x" + strconv.FormatUint(n, 16)
}

// applyIzciMapping reads customer and tenant from well-known tags
// ("customer", "customer_name", "tenant", "tenant_name", with or without an
// "izci." prefix) or from a context named "izci", and removes those tags.
func applyIzciMapping(ev *ingestv1.Event, tags map[string]string, contexts map[string]json.RawMessage) {
	take := func(keys ...string) string {
		for _, k := range keys {
			for _, cand := range []string{k, "izci." + k} {
				if v, ok := tags[cand]; ok {
					delete(tags, cand)
					return v
				}
			}
		}
		return ""
	}
	cust := take("customer", "customer_id")
	custName := take("customer_name")
	tenant := take("tenant", "tenant_id")
	tenantName := take("tenant_name")
	if raw, ok := contexts["izci"]; ok {
		var c map[string]any
		if json.Unmarshal(raw, &c) == nil {
			if cust == "" {
				cust = anyString(c["customer"])
			}
			if custName == "" {
				custName = anyString(c["customer_name"])
			}
			if tenant == "" {
				tenant = anyString(c["tenant"])
			}
			if tenantName == "" {
				tenantName = anyString(c["tenant_name"])
			}
		}
	}
	if cust != "" || custName != "" {
		ev.Customer = &ingestv1.Customer{Id: cust, Name: custName}
	}
	if tenant != "" || tenantName != "" {
		ev.Tenant = &ingestv1.Tenant{Id: tenant, Name: tenantName}
	}
}

func convertException(ex exception) *ingestv1.Exception {
	out := &ingestv1.Exception{Type: ex.Type, Value: ex.Value, Module: ex.Module, ThreadId: anyString(ex.ThreadID)}
	if m := ex.Mechanism; m != nil {
		out.Mechanism = &ingestv1.Mechanism{
			Type: m.Type, Handled: m.Handled, Synthetic: m.Synthetic, Description: m.Description,
			HelpLink: m.HelpLink, IsExceptionGroup: m.IsExceptionGroup, Source: m.Source,
			ParentId: m.ParentID,
		}
		if m.ExceptionID != nil {
			out.Mechanism.ExceptionId = *m.ExceptionID
		}
		if len(m.Data) > 0 {
			out.Mechanism.Data = stringMap(m.Data)
		}
	}
	if ex.Stacktrace != nil {
		out.Stacktrace = convertStacktrace(ex.Stacktrace)
	}
	return out
}

func convertStacktrace(st *stacktrace) *ingestv1.Stacktrace {
	out := &ingestv1.Stacktrace{}
	for _, f := range st.Frames {
		fr := &ingestv1.Frame{
			Function: f.Function, RawFunction: f.RawFunction, Module: f.Module, Package: f.Package,
			Filename: f.Filename, AbsPath: f.AbsPath, ContextLine: f.ContextLine,
			PreContext: f.PreContext, PostContext: f.PostContext, InApp: f.InApp,
			InstructionAddr: f.InstructionAddr, Symbol: f.Symbol, Platform: f.Platform,
			Lineno: uint32(number(f.Lineno)), Colno: uint32(number(f.Colno)),
		}
		if len(f.Vars) > 0 {
			fr.Vars = stringMap(f.Vars)
		}
		out.Frames = append(out.Frames, fr)
	}
	if len(st.Registers) > 0 {
		out.Registers = stringMap(st.Registers)
	}
	return out
}

func convertRequest(r *request) *ingestv1.Request {
	out := &ingestv1.Request{Method: r.Method, Url: r.URL, Fragment: r.Fragment}
	switch q := rawString(r.QueryString); {
	case q != "":
		out.QueryString = strings.TrimPrefix(q, "?")
	default:
		var parts []string
		for k, v := range pairsOrdered(r.QueryString) {
			parts = append(parts, k+"="+v)
		}
		out.QueryString = strings.Join(parts, "&")
	}
	for name, value := range pairsOrdered(r.Headers) {
		out.Headers = append(out.Headers, &ingestv1.Header{Name: name, Value: value})
	}
	if c := rawString(r.Cookies); c != "" {
		out.Cookies = map[string]string{}
		for _, part := range strings.Split(c, ";") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
			if k != "" {
				out.Cookies[k] = v
			}
		}
	} else if m := pairs(r.Cookies); len(m) > 0 {
		out.Cookies = m
	}
	if len(r.Data) > 0 && string(r.Data) != "null" {
		if s := rawString(r.Data); s != "" {
			out.Body = s
		} else {
			out.Body = string(r.Data)
		}
		out.BodySize = int64(len(out.Body))
	}
	if len(r.Env) > 0 {
		out.Env = stringMap(r.Env)
		out.RemoteAddr = out.Env["REMOTE_ADDR"]
	}
	return out
}

func convertUser(u map[string]any) *ingestv1.User {
	out := &ingestv1.User{
		Id: anyString(u["id"]), Username: anyString(u["username"]), Email: anyString(u["email"]),
		Name: anyString(u["name"]), IpAddress: anyString(u["ip_address"]), Segment: anyString(u["segment"]),
	}
	if g, ok := u["geo"].(map[string]any); ok {
		out.Geo = &ingestv1.Geo{CountryCode: anyString(g["country_code"]), Region: anyString(g["region"]), City: anyString(g["city"])}
	}
	attrs := map[string]string{}
	for k, v := range u {
		switch k {
		case "id", "username", "email", "name", "ip_address", "segment", "geo":
		default:
			attrs[k] = anyString(v)
		}
	}
	if data, ok := u["data"].(map[string]any); ok {
		delete(attrs, "data")
		for k, v := range data {
			attrs[k] = anyString(v)
		}
	}
	if len(attrs) > 0 {
		out.Attributes = attrs
	}
	return out
}

// knownContextFields lists the fields izci models for each well-known
// context type; anything else an SDK sends is kept under Contexts.custom with
// the context's name, so that no reported detail is lost.
var knownContextFields = map[string][]string{
	"device": {"name", "family", "model", "model_id", "manufacturer", "brand", "arch", "device_type", "orientation",
		"simulator", "charging", "online", "battery_level", "processor_count", "cpu_description", "processor_frequency",
		"memory_size", "free_memory", "usable_memory", "low_memory", "storage_size", "free_storage", "screen_resolution",
		"screen_width_pixels", "screen_height_pixels", "screen_density", "screen_dpi", "boot_time"},
	"os":             {"name", "version", "build", "kernel_version", "raw_description", "rooted"},
	"client_os":      {"name", "version", "build", "kernel_version", "raw_description", "rooted"},
	"runtime":        {"name", "version", "build", "raw_description"},
	"browser":        {"name", "version"},
	"app":            {"app_identifier", "app_name", "app_version", "app_build", "app_memory", "in_foreground", "app_start_time"},
	"gpu":            {"name", "vendor_name", "memory_size", "api_type", "driver_version"},
	"culture":        {"locale", "timezone", "calendar", "is_24_hour_format"},
	"trace":          {"trace_id", "span_id", "parent_span_id", "op", "status"},
	"response":       {"status_code", "body_size"},
	"cloud_resource": {"cloud.provider", "cloud.region", "cloud.availability_zone", "cloud.account.id", "host.id", "cloud.platform"},
	// .NET reports CultureInfo.CurrentCulture as "Current Culture".
	"current culture": {"name", "calendar"},
}

// leftover returns the fields of m that the context type does not model,
// without empty values.
func leftover(typ string, m map[string]any) map[string]any {
	known := knownContextFields[typ]
	out := map[string]any{}
	for k, v := range m {
		if k == "type" || slices.Contains(known, k) || v == nil || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func convertContexts(ctxs map[string]json.RawMessage, ev *ingestv1.Event) *ingestv1.Contexts {
	if len(ctxs) == 0 {
		return nil
	}
	out := &ingestv1.Contexts{}
	custom := map[string]*structpb.Struct{}
	for name, raw := range ctxs {
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&m) != nil {
			continue
		}
		typ := strings.ToLower(anyString(m["type"]))
		if typ == "" {
			typ = strings.ToLower(name)
		}
		if len(leftover("", m)) == 0 {
			continue // {} or only a type
		}
		if _, known := knownContextFields[typ]; known {
			if rest := leftover(typ, m); len(rest) > 0 {
				if st, err := structpb.NewStruct(jsonSafe(rest)); err == nil {
					custom[name] = st
				}
			}
		}
		switch typ {
		case "device":
			out.Device = convertDevice(m)
		case "os", "client_os":
			if typ == "os" || out.Os == nil {
				out.Os = &ingestv1.OsContext{Name: str(m, "name"), Version: str(m, "version"), Build: str(m, "build"),
					KernelVersion: str(m, "kernel_version"), RawDescription: str(m, "raw_description"), Rooted: boolPtr(m, "rooted")}
			}
		case "runtime":
			out.Runtime = &ingestv1.RuntimeContext{Name: str(m, "name"), Version: str(m, "version"), Build: str(m, "build"), RawDescription: str(m, "raw_description")}
		case "browser":
			out.Browser = &ingestv1.BrowserContext{Name: str(m, "name"), Version: str(m, "version")}
		case "app":
			a := &ingestv1.AppContext{Id: str(m, "app_identifier"), Name: str(m, "app_name"), Version: str(m, "app_version"),
				Build: str(m, "app_build"), MemoryBytes: uint64(num(m, "app_memory")), InForeground: boolPtr(m, "in_foreground")}
			if t, ok := parseTime(rawOf(m["app_start_time"])); ok {
				a.StartTime = timestamppb.New(t)
			}
			out.App = a
		case "gpu":
			out.Gpu = &ingestv1.GpuContext{Name: str(m, "name"), VendorName: str(m, "vendor_name"),
				MemoryBytes: uint64(num(m, "memory_size")) << 20, ApiType: str(m, "api_type"), DriverVersion: str(m, "driver_version")}
		case "culture":
			c := &ingestv1.CultureContext{Locale: str(m, "locale"), Timezone: str(m, "timezone"), Calendar: str(m, "calendar"),
				Is_24HourFormat: boolPtr(m, "is_24_hour_format")}
			if out.Culture != nil && c.Timezone == "" {
				c.Timezone = out.Culture.Timezone
			}
			out.Culture = c
		case "current culture":
			if out.Culture == nil {
				out.Culture = &ingestv1.CultureContext{}
			}
			if out.Culture.Locale == "" {
				out.Culture.Locale = str(m, "name")
			}
			if out.Culture.Calendar == "" {
				out.Culture.Calendar = str(m, "calendar")
			}
		case "trace":
			out.Trace = &ingestv1.TraceContext{TraceId: str(m, "trace_id"), SpanId: str(m, "span_id"),
				ParentSpanId: str(m, "parent_span_id"), Op: str(m, "op"), Status: str(m, "status")}
		case "response":
			if ev.Response == nil {
				ev.Response = &ingestv1.Response{}
			}
			ev.Response.StatusCode = uint32(num(m, "status_code"))
			ev.Response.BodySize = int64(num(m, "body_size"))
		case "cloud_resource":
			out.Cloud = &ingestv1.CloudContext{Provider: str(m, "cloud.provider"), Region: str(m, "cloud.region"),
				AvailabilityZone: str(m, "cloud.availability_zone"), AccountId: str(m, "cloud.account.id"),
				InstanceId: str(m, "host.id"), Platform: str(m, "cloud.platform")}
		case "izci":
			// Consumed by applyIzciMapping.
		default:
			delete(m, "type")
			if st, err := structpb.NewStruct(jsonSafe(m)); err == nil {
				custom[name] = st
			}
		}
	}
	if len(custom) > 0 {
		out.Custom = custom
	}
	if proto.Equal(out, &ingestv1.Contexts{}) {
		return nil
	}
	return out
}

func convertDevice(m map[string]any) *ingestv1.DeviceContext {
	d := &ingestv1.DeviceContext{
		Name: str(m, "name"), Family: str(m, "family"), Model: str(m, "model"), ModelId: str(m, "model_id"),
		Manufacturer: str(m, "manufacturer"), Brand: str(m, "brand"), Arch: str(m, "arch"),
		DeviceType: strings.ToLower(str(m, "device_type")), Orientation: str(m, "orientation"),
		Simulator: boolOf(m, "simulator"), Charging: boolPtr(m, "charging"), Online: boolPtr(m, "online"),
	}
	if v, ok := m["battery_level"]; ok {
		f := float32(toFloat(v))
		d.BatteryLevel = &f
	}
	if c := num(m, "processor_count"); c > 0 || str(m, "cpu_description") != "" {
		d.Cpu = &ingestv1.CpuInfo{Count: uint32(c), Model: str(m, "cpu_description"), FrequencyMhz: toFloat(m["processor_frequency"])}
	}
	if total := num(m, "memory_size"); total > 0 || num(m, "free_memory") > 0 {
		d.Memory = &ingestv1.MemoryInfo{TotalBytes: uint64(total), FreeBytes: uint64(num(m, "free_memory")),
			AvailableBytes: uint64(num(m, "usable_memory")), LowMemory: boolOf(m, "low_memory")}
		if total > 0 && d.Memory.AvailableBytes > 0 {
			used := 100 * (1 - float64(d.Memory.AvailableBytes)/float64(total))
			d.Memory.UsedPercent = &used
		}
	}
	if total := num(m, "storage_size"); total > 0 {
		d.Storage = &ingestv1.StorageInfo{TotalBytes: uint64(total), FreeBytes: uint64(num(m, "free_storage"))}
	}
	if res := str(m, "screen_resolution"); res != "" || num(m, "screen_width_pixels") > 0 {
		s := &ingestv1.ScreenInfo{Density: toFloat(m["screen_density"]), Dpi: uint32(num(m, "screen_dpi")),
			WidthPixels: uint32(num(m, "screen_width_pixels")), HeightPixels: uint32(num(m, "screen_height_pixels"))}
		if w, h, ok := strings.Cut(res, "x"); ok {
			wi, _ := strconv.Atoi(strings.TrimSpace(w))
			hi, _ := strconv.Atoi(strings.TrimSpace(h))
			s.WidthPixels, s.HeightPixels = uint32(wi), uint32(hi)
		}
		d.Screen = s
	}
	if t, ok := parseTime(rawOf(m["boot_time"])); ok {
		d.BootTime = timestamppb.New(t)
	}
	return d
}

// ── helpers ──────────────────────────────────────────────────────────────

// listOf decodes {"values":[…]} or […].
func listOf[T any](raw json.RawMessage) []T {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var wrapped struct {
		Values []T `json:"values"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if raw[0] == '{' && dec.Decode(&wrapped) == nil {
		return wrapped.Values
	}
	var list []T
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&list) == nil {
		return list
	}
	return nil
}

func message(raw json.RawMessage, le *logEntry) *ingestv1.Message {
	m := &ingestv1.Message{}
	if s := rawString(raw); s != "" {
		m.Formatted = s
	} else if len(raw) > 0 && raw[0] == '{' {
		var obj logEntry
		if json.Unmarshal(raw, &obj) == nil {
			le = mergeLogEntry(le, &obj)
		}
	}
	if le != nil {
		if m.Formatted == "" {
			m.Formatted = le.Formatted
		}
		if le.Message != "" {
			m.Template = le.Message
		}
		for _, p := range le.Params {
			m.Params = append(m.Params, anyString(p))
		}
		if m.Formatted == "" {
			m.Formatted = le.Message
		}
	}
	if m.Formatted == "" && m.Template == "" {
		return nil
	}
	return m
}

func mergeLogEntry(a, b *logEntry) *logEntry {
	if a == nil {
		return b
	}
	if a.Formatted == "" {
		a.Formatted = b.Formatted
	}
	if a.Message == "" {
		a.Message = b.Message
	}
	if len(a.Params) == 0 {
		a.Params = b.Params
	}
	return a
}

// pairs decodes {"k":"v"} or [["k","v"], …] into a map.
func pairs(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	for k, v := range pairsOrdered(raw) {
		out[k] = v
	}
	return out
}

// pairsOrdered yields key/value pairs in document order.
func pairsOrdered(raw json.RawMessage) func(yield func(string, string) bool) {
	return func(yield func(string, string) bool) {
		if len(raw) == 0 || string(raw) == "null" {
			return
		}
		switch raw[0] {
		case '[':
			var list [][]any
			if json.Unmarshal(raw, &list) != nil {
				return
			}
			for _, p := range list {
				if len(p) == 2 && !yield(anyString(p[0]), anyString(p[1])) {
					return
				}
			}
		case '{':
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if _, err := dec.Token(); err != nil {
				return
			}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return
				}
				var v any
				if dec.Decode(&v) != nil {
					return
				}
				if !yield(anyString(kt), anyString(v)) {
					return
				}
			}
		}
	}
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || raw[0] != '"' {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func rawOf(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// parseTime accepts RFC 3339 strings (with or without zone) and Unix
// seconds as numbers.
func parseTime(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false
	}
	if raw[0] == '"' {
		s := rawString(raw)
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC(), true
			}
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return unixFloat(f), true
		}
		return time.Time{}, false
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || f <= 0 {
		return time.Time{}, false
	}
	return unixFloat(f), true
}

func unixFloat(f float64) time.Time {
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC()
}

func level(s string) ingestv1.Level {
	switch strings.ToLower(s) {
	case "fatal", "critical":
		return ingestv1.Level_LEVEL_FATAL
	case "error":
		return ingestv1.Level_LEVEL_ERROR
	case "warning", "warn":
		return ingestv1.Level_LEVEL_WARNING
	case "info", "log":
		return ingestv1.Level_LEVEL_INFO
	case "debug":
		return ingestv1.Level_LEVEL_DEBUG
	}
	return ingestv1.Level_LEVEL_UNSPECIFIED
}

// sdkPlatforms maps Sentry SDK name prefixes to platforms, for SDKs that
// report a generic platform ("other", "native").
var sdkPlatforms = []struct{ prefix, platform string }{
	{"sentry.dart", "dart"},
	{"sentry.rust", "rust"},
	{"sentry.dotnet", "dotnet"},
	{"sentry.unity", "dotnet"},
	{"sentry.java", "java"},
	{"sentry.kotlin", "kotlin"},
	{"sentry.cocoa", "cocoa"},
	{"sentry.python", "python"},
	{"sentry.go", "go"},
	{"sentry.php", "php"},
	{"sentry.ruby", "ruby"},
	{"sentry.elixir", "elixir"},
}

// platform maps Sentry platform names onto izci's well-known values.
func platform(p string, sdk *sdkInfo) string {
	p = strings.ToLower(p)
	if (p == "" || p == "other" || p == "native") && sdk != nil {
		for _, sp := range sdkPlatforms {
			if strings.HasPrefix(strings.ToLower(sdk.Name), sp.prefix) {
				return sp.platform
			}
		}
	}
	switch p {
	case "csharp", "dotnet":
		return "dotnet"
	case "javascript":
		if sdk != nil && (strings.Contains(sdk.Name, "node") || strings.Contains(sdk.Name, "nestjs") || strings.Contains(sdk.Name, "bun")) {
			return "node"
		}
		return "javascript"
	case "node":
		return "node"
	case "go", "python", "java", "php", "ruby", "elixir", "rust", "native", "dart", "cocoa", "objc", "kotlin":
		return p
	case "flutter":
		return "dart"
	}
	if p == "" {
		return ""
	}
	return p
}

func str(m map[string]any, k string) string { return anyString(m[k]) }

func num(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		f, _ := v.Float64()
		return int64(f)
	case float64:
		return int64(v)
	case string:
		i, _ := strconv.ParseInt(v, 10, 64)
		return i
	}
	return 0
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func number(n json.Number) int64 {
	if n == "" {
		return 0
	}
	if i, err := n.Int64(); err == nil && i > 0 {
		return i
	}
	return 0
}

func boolOf(m map[string]any, k string) bool {
	b, _ := m[k].(bool)
	return b
}

func boolPtr(m map[string]any, k string) *bool {
	if b, ok := m[k].(bool); ok {
		return &b
	}
	return nil
}

// anyString renders a JSON value as text (objects and arrays as JSON).
func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = anyString(v)
	}
	return out
}

// jsonSafe converts json.Number values so structpb accepts the map.
func jsonSafe(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = jsonSafeValue(v)
	}
	return out
}

func jsonSafeValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case map[string]any:
		return jsonSafe(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = jsonSafeValue(item)
		}
		return out
	}
	return v
}
