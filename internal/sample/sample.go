// Package sample generates realistic events for demos, load tests and the
// "send test event" button: .NET, Go, Node.js and browser errors with stack
// traces, requests, users, customers, breadcrumbs and CPU/memory contexts.
package sample

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Customers are fictional municipalities used in demo data.
var Customers = []struct{ ID, Name string }{
	{"sivas-belediyesi", "Sivas Belediyesi"},
	{"tokat-belediyesi", "Tokat Belediyesi"},
	{"yozgat-belediyesi", "Yozgat Belediyesi"},
	{"amasya-belediyesi", "Amasya Belediyesi"},
	{"kayseri-belediyesi", "Kayseri Büyükşehir Belediyesi"},
	{"erzincan-belediyesi", "Erzincan Belediyesi"},
}

var users = []struct{ ID, Email, Name string }{
	{"1042", "ayse.yilmaz@example.com", "Ayşe Yılmaz"},
	{"2231", "mehmet.demir@example.com", "Mehmet Demir"},
	{"3310", "zeynep.kaya@example.com", "Zeynep Kaya"},
	{"4518", "ali.celik@example.com", "Ali Çelik"},
	{"5120", "elif.sahin@example.com", "Elif Şahin"},
	{"6007", "can.ozturk@example.com", "Can Öztürk"},
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.6668.58 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:130.0) Gecko/20100101 Firefox/130.0",
	"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.6668.70 Mobile Safari/537.36",
}

type template struct {
	platform, typ, module, logger, route, method string
	value                                        func(r *rand.Rand) string
	frames                                       []*ingestv1.Frame
	level                                        ingestv1.Level
	handled                                      bool
	db                                           *ingestv1.DatabaseContext
}

func f(module, fn, file string, line uint32, inApp bool, ctx string) *ingestv1.Frame {
	v := inApp
	fr := &ingestv1.Frame{Module: module, Function: fn, Filename: file, Lineno: line, InApp: &v}
	if ctx != "" {
		fr.ContextLine = ctx
		fr.PreContext = []string{"    {", "        var order = await _orders.FindAsync(id);"}
		fr.PostContext = []string{"        return Ok(result);", "    }"}
	}
	return fr
}

var templates = []template{
	{
		platform: "dotnet", typ: "System.NullReferenceException", module: "System", logger: "KentOS.Kalem.Services.OrderService",
		route: "POST /api/orders/{id}/approve", method: "POST", level: ingestv1.Level_LEVEL_ERROR, handled: false,
		value: func(*rand.Rand) string { return "Object reference not set to an instance of an object." },
		frames: []*ingestv1.Frame{
			f("Microsoft.AspNetCore.Mvc.Infrastructure", "ControllerActionInvoker.InvokeActionMethodAsync", "", 0, false, ""),
			f("KentOS.Kalem.Web.Controllers", "OrdersController.Approve", "src/KentOS.Kalem.Web/Controllers/OrdersController.cs", 88, true, "        var result = await _service.ApproveAsync(id, User.GetId());"),
			f("KentOS.Kalem.Application.Services", "OrderService.ApproveAsync", "src/KentOS.Kalem.Application/Services/OrderService.cs", 142, true, "        var owner = order.Owner.Department.Name;"),
		},
	},
	{
		platform: "dotnet", typ: "Npgsql.PostgresException", module: "Npgsql", logger: "KentOS.Kalem.Data",
		route: "POST /api/citizens", method: "POST", level: ingestv1.Level_LEVEL_ERROR, handled: true,
		value: func(r *rand.Rand) string {
			return fmt.Sprintf("23505: duplicate key value violates unique constraint \"citizens_tckn_key\" (row %d)", r.IntN(9000)+1000)
		},
		frames: []*ingestv1.Frame{
			f("Npgsql", "NpgsqlConnector.ReadMessageLong", "", 0, false, ""),
			f("KentOS.Kalem.Data.Repositories", "CitizenRepository.InsertAsync", "src/KentOS.Kalem.Data/Repositories/CitizenRepository.cs", 57, true, "        await conn.ExecuteAsync(InsertSql, citizen);"),
			f("KentOS.Kalem.Application.Services", "CitizenService.RegisterAsync", "src/KentOS.Kalem.Application/Services/CitizenService.cs", 31, true, "        await _citizens.InsertAsync(entity);"),
		},
		db: &ingestv1.DatabaseContext{System: "postgresql", Name: "kalem", Operation: "INSERT", ErrorCode: "23505",
			Statement: "INSERT INTO citizens (tckn, name, district_id) VALUES ($1, $2, $3)", DurationMs: 4.2},
	},
	{
		platform: "go", typ: "*net.OpError", module: "net", logger: "bildirim.sms",
		route: "POST /v1/sms/send", method: "POST", level: ingestv1.Level_LEVEL_ERROR, handled: true,
		value: func(r *rand.Rand) string {
			return fmt.Sprintf("dial tcp 10.0.%d.%d:443: i/o timeout", r.IntN(4), r.IntN(250))
		},
		frames: []*ingestv1.Frame{
			f("net/http", "(*Client).Do", "net/http/client.go", 590, false, ""),
			f("github.com/kentos/bildirim/internal/sms", "(*Provider).Send", "internal/sms/provider.go", 118, true, "\tresp, err := p.client.Do(req)"),
			f("github.com/kentos/bildirim/internal/sms", "(*Dispatcher).dispatch.func1", "internal/sms/dispatcher.go", 64, true, "\t\tif err := p.Send(ctx, msg); err != nil {"),
		},
	},
	{
		platform: "node", typ: "TypeError", module: "", logger: "kentos-platform",
		route: "GET /api/raporlar/:id", method: "GET", level: ingestv1.Level_LEVEL_ERROR, handled: false,
		value: func(*rand.Rand) string { return "Cannot read properties of undefined (reading 'columns')" },
		frames: []*ingestv1.Frame{
			f("", "processTicksAndRejections", "node:internal/process/task_queues", 95, false, ""),
			f("", "ReportService.render", "backend/src/modules/rapor/report.service.ts", 211, true, "    const cols = definition.layout.columns.map(toColumn);"),
			f("", "ReportController.get", "backend/src/modules/rapor/report.controller.ts", 48, true, "    return this.reports.render(id, query);"),
		},
	},
	{
		platform: "javascript", typ: "ChunkLoadError", module: "", logger: "browser",
		route: "/basvurular/yeni", method: "GET", level: ingestv1.Level_LEVEL_WARNING, handled: true,
		value: func(r *rand.Rand) string {
			return fmt.Sprintf("Loading chunk %d failed. (timeout: https://kalem.example.com/assets/form-%x.js)", r.IntN(90)+10, r.Uint32())
		},
		frames: []*ingestv1.Frame{
			f("", "requireEnsure", "https://kalem.example.com/assets/index-CeX-bC8z.js", 1, false, ""),
			f("", "BasvuruForm", "https://kalem.example.com/assets/form-Dk39aLq1.js", 1, true, ""),
		},
	},
	{
		platform: "dotnet", typ: "System.TimeoutException", module: "System", logger: "KentOS.Bildirim.Firebase",
		route: "worker:push-dispatch", method: "", level: ingestv1.Level_LEVEL_FATAL, handled: false,
		value: func(r *rand.Rand) string {
			return fmt.Sprintf("The operation has timed out after %d ms.", 30000+r.IntN(5)*1000)
		},
		frames: []*ingestv1.Frame{
			f("Polly", "AsyncRetryEngine.ImplementationAsync", "", 0, false, ""),
			f("KentOS.Bildirim.Firebase", "PushSender.SendAsync", "src/KentOS.Bildirim.Firebase/PushSender.cs", 73, true, "        var response = await _messaging.SendEachAsync(batch, ct);"),
		},
	},
}

// Options control Event.
type Options struct {
	// Template picks a specific error (index into the catalog); -1 = random.
	Template int
	// Release to report; empty picks one of a few demo releases.
	Release string
	// Timestamp overrides the event time.
	Timestamp time.Time
}

// Count is the number of distinct error templates.
func Count() int { return len(templates) }

// Event builds one random, realistic event.
func Event(r *rand.Rand, opt Options) *ingestv1.Event {
	t := templates[r.IntN(len(templates))]
	if opt.Template >= 0 && opt.Template < len(templates) {
		t = templates[opt.Template]
	}
	cust := Customers[r.IntN(len(Customers))]
	user := users[r.IntN(len(users))]
	release := opt.Release
	if release == "" {
		release = []string{"1.4.0", "1.4.1", "1.4.2"}[r.IntN(3)]
	}
	ts := opt.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	handled := t.handled
	frames := make([]*ingestv1.Frame, len(t.frames))
	for i, fr := range t.frames {
		frames[i] = proto.CloneOf(fr)
	}

	ev := &ingestv1.Event{
		EventId:     uuid.NewString(),
		Timestamp:   timestamppb.New(ts),
		Level:       t.level,
		Platform:    t.platform,
		Logger:      t.logger,
		Transaction: t.route,
		ServerName:  fmt.Sprintf("%s-%d", t.platform, r.IntN(3)+1),
		Release:     release,
		Environment: []string{"production", "production", "production", "staging"}[r.IntN(4)],
		Customer:    &ingestv1.Customer{Id: cust.ID, Name: cust.Name},
		User:        &ingestv1.User{Id: user.ID, Email: user.Email, Name: user.Name, IpAddress: fmt.Sprintf("85.%d.%d.%d", 96+r.IntN(20), r.IntN(250), r.IntN(250))},
		Exceptions: []*ingestv1.Exception{{
			Type: t.typ, Value: t.value(r), Module: t.module,
			Mechanism:  &ingestv1.Mechanism{Type: "generic", Handled: &handled},
			Stacktrace: &ingestv1.Stacktrace{Frames: frames},
		}},
		Tags: map[string]string{"module": moduleOf(t.logger), "region": []string{"ic-anadolu", "karadeniz"}[r.IntN(2)]},
		Sdk:  &ingestv1.Sdk{Name: "izci.sample", Version: "1.0.0"},
		Contexts: &ingestv1.Contexts{
			Runtime: runtimeOf(t.platform),
			Os:      &ingestv1.OsContext{Name: "Linux", Version: "6.8.0", Distribution: "ubuntu"},
			Device: &ingestv1.DeviceContext{Arch: "amd64", DeviceType: "server",
				Cpu:    &ingestv1.CpuInfo{Count: 8, Model: "Intel(R) Xeon(R) Gold 6338", UsagePercent: ptr(20 + r.Float64()*70), LoadAverage: []float64{1.2 + r.Float64(), 0.9, 0.7}},
				Memory: &ingestv1.MemoryInfo{TotalBytes: 16 << 30, AvailableBytes: uint64(2+r.IntN(10)) << 30, UsedPercent: ptr(40 + r.Float64()*50)}},
			Process: &ingestv1.ProcessContext{Pid: uint32(1000 + r.IntN(30000)), RssBytes: uint64(200+r.IntN(900)) << 20, CpuPercent: ptr(5 + r.Float64()*80),
				Threads: uint32(20 + r.IntN(60)), Uptime: durationpb.New(time.Duration(r.IntN(72)) * time.Hour)},
			Culture: &ingestv1.CultureContext{Locale: "tr-TR", Timezone: "Europe/Istanbul"},
			Trace:   &ingestv1.TraceContext{TraceId: fmt.Sprintf("%032x", r.Uint64()), SpanId: fmt.Sprintf("%016x", r.Uint64())},
		},
		Breadcrumbs: []*ingestv1.Breadcrumb{
			{Timestamp: timestamppb.New(ts.Add(-4 * time.Second)), Type: "navigation", Category: "navigation", Message: "/basvurular → " + t.route},
			{Timestamp: timestamppb.New(ts.Add(-2 * time.Second)), Type: "http", Category: "http.client", Message: "GET https://kimlik.example.com/connect/userinfo → 200"},
			{Timestamp: timestamppb.New(ts.Add(-800 * time.Millisecond)), Type: "query", Category: "db.sql", Message: "SELECT * FROM orders WHERE id = $1"},
		},
	}
	if t.db != nil {
		ev.Contexts.Database = t.db
	}
	if t.method != "" {
		ev.Request = &ingestv1.Request{
			Method: t.method, Url: "https://kalem.example.com" + routePath(t.route, r), Route: t.route,
			QueryString: "page=1&sort=desc",
			Headers: []*ingestv1.Header{
				{Name: "User-Agent", Value: userAgents[r.IntN(len(userAgents))]},
				{Name: "Accept-Language", Value: "tr-TR,tr;q=0.9,en;q=0.8"},
				{Name: "Authorization", Value: "Bearer eyJhbGciOiJSUzI1NiJ9.demo.signature"},
			},
			RemoteAddr: ev.User.IpAddress,
		}
		if t.method == "POST" {
			ev.Request.Body = `{"id": 42, "note": "acil", "password": "hunter2"}`
			ev.Request.Headers = append(ev.Request.Headers, &ingestv1.Header{Name: "Content-Type", Value: "application/json"})
		}
	}
	if t.platform == "javascript" {
		ev.Contexts.Runtime, ev.Contexts.Os, ev.Contexts.Device.DeviceType = nil, nil, ""
		ev.User.IpAddress = "{{auto}}"
	}
	extra, _ := structpb.NewStruct(map[string]any{"order_id": r.IntN(90000) + 10000, "retry": r.IntN(3)})
	ev.Extra = extra
	return ev
}

func ptr[T any](v T) *T { return &v }

func moduleOf(logger string) string {
	for i := len(logger) - 1; i >= 0; i-- {
		if logger[i] == '.' {
			return logger[i+1:]
		}
	}
	return logger
}

func routePath(route string, r *rand.Rand) string {
	out := []byte{}
	for i := 0; i < len(route); i++ {
		switch {
		case route[i] == '{' || route[i] == ':':
			out = fmt.Appendf(out, "%d", r.IntN(9000)+100)
			for i < len(route) && route[i] != '}' && route[i] != '/' {
				i++
			}
			if i < len(route) && route[i] == '/' {
				out = append(out, '/')
			}
		case route[i] == ' ':
			out = out[:0]
		default:
			out = append(out, route[i])
		}
	}
	return string(out)
}

func runtimeOf(platform string) *ingestv1.RuntimeContext {
	switch platform {
	case "dotnet":
		return &ingestv1.RuntimeContext{Name: ".NET", Version: "10.0.0"}
	case "go":
		return &ingestv1.RuntimeContext{Name: "go", Version: "1.26.1"}
	case "node":
		return &ingestv1.RuntimeContext{Name: "node", Version: "24.14.0"}
	}
	return nil
}
