# Go SDK

`github.com/mcihad/kentos-izci/sdk/go` (paket adı `izci`), Go uygulamalarındaki hataları, panikleri ve mesajları İzci'ye gönderir. Her olayla birlikte şunları toplar: yığın izi ve hata zinciri, kaynak kod satırları, istek, kullanıcı, müşteri ve tenant, çalışma zamanı bilgisi (CPU, bellek, goroutine, süreç).

SDK ayrı bir Go modülüdür. Bağımlılıkları yalnızca Connect ve protobuf'tur.

```bash
go get github.com/mcihad/kentos-izci/sdk/go
```

## Başlatma

```go
import izci "github.com/mcihad/kentos-izci/sdk/go"

func main() {
	if err := izci.Init(izci.Options{
		Endpoint:     "https://izci.example.com",
		Key:          os.Getenv("IZCI_KEY"), // projenin ingest anahtarı (izk_…)
		Environment:  "production",
		Release:      "bildirim@2.4.0",
		CustomerID:   "sivas-belediyesi", // müşteriye özel kurulumlarda
		CustomerName: "Sivas Belediyesi",
	}); err != nil {
		log.Fatal(err)
	}
	defer izci.Close(3 * time.Second) // kuyruktaki olayları gönderir
	// …
}
```

`Options` alanları:

| Alan | Varsayılan | Açıklama |
|---|---|---|
| `Endpoint`, `Key` | — | Zorunlu. |
| `Environment` | `production` | |
| `Release`, `Dist`, `ServerName` | —, —, host adı | |
| `CustomerID`, `CustomerName`, `TenantID`, `TenantName` | — | Varsayılan müşteri ve tenant; scope'lar değiştirebilir. |
| `Tags` | — | Her olaya eklenen etiketler. |
| `SampleRate` | 1.0 | Gönderilecek olay oranı. |
| `MaxBreadcrumbs` | 100 | Scope başına breadcrumb. |
| `AttachStacktrace` | `false` | Mesaj olaylarına da yığın izi ekler. |
| `InAppInclude`, `InAppExclude` | ana modül | Uygulama kodu sayılan (sayılmayan) paket önekleri. |
| `DisableContextLines`, `DisableRuntimeContext`, `IncludeModules` | `false` | Kaynak satırları, çalışma zamanı bağlamı ve bağımlılık sürümleri. |
| `BeforeSend` | — | Olayı değiştirir ya da `nil` döndürerek düşürür. |
| `Protocol` | `ProtocolConnect` | `ProtocolGRPC` native gRPC kullanır (HTTP/2; `http://` için h2c). |
| `QueueSize`, `BatchSize`, `FlushInterval` | 1000, 50, 1s | Gönderim kuyruğu. Kuyruk doluysa yeni olaylar düşürülür. |

## Yakalama

```go
izci.CaptureError(err)                                   // hata ve zinciri
izci.CaptureErrorContext(ctx, err, izci.WithTag("adim", "tahsilat"))
izci.CaptureMessage("Günlük mutabakat eksik kapandı", izci.WithLevel(izci.LevelWarning))
defer izci.Recover()                                     // paniği bildirir, yutar
defer izci.RecoverRepanic()                              // paniği bildirir, yeniden fırlatır
```

Her yakalama fonksiyonu olay kimliğini döndürür. Seçenekler: `WithLevel`, `WithTag`, `WithFingerprint`, `WithCustomer`.

Hata zinciri `errors.Unwrap`, `Unwrap() []error` (`errors.Join`) ve pkg/errors tarzı `StackTrace()` üzerinden kurulur. Zincirdeki her hata ayrı bir istisna olarak gönderilir.

## Scope: istek boyunca bağlam

Scope; kullanıcı, müşteri, tenant, etiket, breadcrumb ve parmak izini tutar. İstek başına bir scope, `context.Context` içinde taşınır:

```go
ctx = izci.WithScope(ctx, func(s *izci.Scope) {
	s.SetUser(izci.User{ID: kullanici.ID, Email: kullanici.Eposta})
	s.SetCustomer(belediye.Kod, belediye.Ad)
	s.SetTenant(birim.Kod, birim.Ad)
	s.SetTag("modul", "e-imza")
})

izci.AddBreadcrumb(ctx, izci.Breadcrumb{Type: "http", Category: "http.client", Message: "POST kimlik/token → 200"})
izci.CaptureErrorContext(ctx, err)
```

Tüm uygulama için: `izci.ConfigureScope(func(s *izci.Scope) { … })`.

## HTTP ara katmanı: `izcihttp`

```go
import "github.com/mcihad/kentos-izci/sdk/go/izcihttp"

handler := izcihttp.New(izcihttp.Options{CaptureServerErrors: true}).Handle(mux)
```

- Her isteğe ayrı bir scope açar; `izci.FromContext(r.Context())` ile ulaşılır.
- Panikleri istek bilgisiyle bildirir ve 500 döner (`Repanic: true` ile paniği dış katmana bırakır).
- `CaptureServerErrors` panik olmadan dönen 5xx yanıtları da bildirir.
- `Authorization` ve `Cookie` gibi hassas başlıklar hiç gönderilmez.

## slog: `izcislog`

```go
import "github.com/mcihad/kentos-izci/sdk/go/izcislog"

logger := slog.New(izcislog.New(slog.NewJSONHandler(os.Stderr, nil), izcislog.Options{}))
logger.ErrorContext(ctx, "fatura oluşturulamadı", "err", err, "fatura_id", id)
```

`Error` ve üstü kayıtlar olay olur (`err` özniteliği hata zinciriyle birlikte). `Info` ve üstü kayıtlar breadcrumb olarak saklanır. Eşikler `EventLevel` ve `BreadcrumbLevel` ile değiştirilir. Kayıtlar ayrıca sarmalanan handler'a da yazılır.

## pgx: `izcipgx`

Ayrı modüldür, böylece SDK pgx'e bağımlı olmaz:

```go
import "github.com/mcihad/kentos-izci/sdk/go/izcipgx"

cfg, _ := pgxpool.ParseConfig(url)
cfg.ConnConfig.Tracer = &izcipgx.Tracer{}
```

Sorgular breadcrumb olur. Hatalı sorgu, PostgreSQL hata koduyla birlikte scope'a eklenir; ardından yakalanan hatanın olayında "Veritabanı" kartında görünür.

## Zamanlanmış görevler

`WithMonitor` bir görevi [izleyicinin](cron.md) bir çalışması olarak çalıştırır: başlarken `in_progress`, görev dönünce süresiyle birlikte `ok` ya da (görev hata döndürdüyse) `error` gönderir. Görevin hatası olduğu gibi döner; check-in gönderilemezse görev yine çalışır.

```go
err := izci.WithMonitor(ctx, "gunluk-tahsilat", &izci.MonitorConfig{
	Crontab:       "0 3 * * *",
	Timezone:      "Europe/Istanbul",
	CheckinMargin: 5 * time.Minute,
	MaxRuntime:    30 * time.Minute,
}, runBilling)
```

`MonitorConfig` izleyiciyi oluşturur ya da günceller; izleyici arayüzden oluşturulduysa `nil` verilebilir. Aralıklı işler için `Crontab` yerine `Interval: 2, Unit: izci.Hour` yazılır.

Tek tek göndermek için `CaptureCheckIn` vardır. Olay kuyruğunu beklemeden hemen gönderir ve çalışmanın kimliğini döner:

```go
id, err := izci.CaptureCheckIn(ctx, izci.CheckIn{MonitorSlug: "gece-aktarim", Status: izci.CheckInInProgress}, nil)
// ... iş ...
_, err = izci.CaptureCheckIn(ctx, izci.CheckIn{ID: id, MonitorSlug: "gece-aktarim", Status: izci.CheckInOK}, nil)
```

Ortam ve sürüm verilmezse istemcinin `Environment` ve `Release` ayarları kullanılır.

## Çalışma zamanı bağlamı

`DisableRuntimeContext` kapalıyken her olay şunları taşır: `runtime.MemStats` (heap, GC), goroutine sayısı, `GOMAXPROCS`, süreç CPU süresi ve RSS, makinenin belleği ve yük ortalaması (Linux'ta `/proc`, macOS'ta `sysctl`). Bu bilgiler olay sayfasında gösterge kartları olarak görünür.

## Gönderim

Olaylar sınırlı bir kuyrukta birikir ve `SendEvents` ile toplu gönderilir. Geçici hatalarda artan beklemeyle yeniden denenir; sunucu `RESOURCE_EXHAUSTED` ile birlikte `RetryInfo` gönderirse o süre beklenir. Uygulama kapanırken `izci.Close` (ya da `izci.Flush`) çağrılmazsa kuyruktaki olaylar kaybolabilir. `izci.CurrentClient().Dropped()` düşürülen olay sayısını verir.

## Örnek

[`examples/go-basic`](../examples/go-basic/main.go) bir HTTP servisini `izcihttp` ve `izcislog` ile izler:

```bash
IZCI_ENDPOINT=http://localhost:5300 IZCI_KEY=izk_… go run ./examples/go-basic -demo
```
