# Sentry SDK'larıyla kullanım

İzci, Sentry'nin olay gönderme protokolünü anlar. Sentry SDK'sı kullanan bir uygulamayı İzci'ye bağlamak için **yalnız DSN'i değiştirmek** yeter; kod değişmez. Sentry olayları İzci olay modeline çevrilir ve gRPC ile gelen olaylarla aynı yoldan geçer: aynı gruplama, uyarılar, müşteri analizi ve yapay zekâ.

## DSN

```
https://izk_…@izci.example.com/<proje-numarası>
```

- Kullanıcı adı yerine projenin ingest anahtarı (`izk_…`) yazılır.
- Yol, projenin sayısal numarasıdır (Sentry SDK'ları sayı bekler).

Hazır DSN, **Projeler › proje › Kurulum** sayfasında gösterilir.

## Platformlara göre

**.NET (ASP.NET Core)**

```csharp
builder.WebHost.UseSentry(o =>
{
    o.Dsn = "https://izk_…@izci.example.com/1";
    o.Environment = "production";
    o.Release = "kalem@1.4.2";
    o.TracesSampleRate = 0; // İzci işlem (transaction) kaydı tutmaz
});

// İstek boyunca müşteri ve tenant:
SentrySdk.ConfigureScope(s =>
{
    s.SetTag("customer", "sivas-belediyesi");
    s.SetTag("customer_name", "Sivas Belediyesi");
    s.SetTag("tenant", "sivas");
});
```

**Tarayıcı (JavaScript, React)**

```ts
import * as Sentry from "@sentry/browser"; // ya da @sentry/react

Sentry.init({
  dsn: "https://izk_…@izci.example.com/4",
  environment: "production",
  release: "kalem-web@2.3.0",
});
Sentry.setTag("customer", musteri.kod);
```

Tarayıcıdan gönderen anahtara izinli kökeni (ör. `https://kalem.example.com`) ekleyin. Anahtar sayfada göründüğü için yalnız olay göndermeye yetkilidir.

**Node.js (NestJS, Express)**

```js
const Sentry = require("@sentry/node");
Sentry.init({ dsn: process.env.IZCI_DSN, environment: "production", release: "rapor@2.1.0", tracesSampleRate: 0 });
Sentry.setTag("customer", "tokat-belediyesi");
```

**Python**

```python
import sentry_sdk
sentry_sdk.init(dsn=os.environ["IZCI_DSN"], environment="production", release="tahsilat@3.0.1", traces_sample_rate=0)
sentry_sdk.set_tag("customer", "sivas-belediyesi")
```

**Flutter**

```dart
await SentryFlutter.init((o) {
  o.dsn = 'https://izk_…@izci.example.com/6';
  o.environment = 'production';
}, appRunner: () => runApp(const App()));
Sentry.configureScope((s) => s.setTag('customer', 'yozgat-belediyesi'));
```

**Go:** `sentry-go` da çalışır, ancak İzci'nin kendi [Go SDK'sı](go-sdk.md) CPU, bellek ve müşteri bilgisini ek ayar gerektirmeden gönderir.

Çalıştırılabilir örnekler: [`examples/sentry/python_sentry.py`](../examples/sentry/python_sentry.py) ve [`examples/sentry/node_sentry.cjs`](../examples/sentry/node_sentry.cjs).

## Müşteri ve tenant

İzci'de müşteri ve tenant ayrı alanlardır; Sentry'de karşılıkları yoktur. İzci bunları şu kaynaklardan okur, bulduğu etiketleri de etiket listesinden çıkarır:

| Sentry'de | İzci'de |
|---|---|
| `customer` ya da `customer_id` etiketi | `customer.id` |
| `customer_name` etiketi | `customer.name` |
| `tenant` ya da `tenant_id` etiketi | `tenant.id` |
| `tenant_name` etiketi | `tenant.name` |
| `izci.` önekli aynı etiketler | aynı alanlar |
| `izci` adlı context (`{"customer": …, "tenant": …}`) | aynı alanlar (etiket yoksa) |

## Eşleme

| Sentry | İzci |
|---|---|
| `exception.values[]` | `exceptions[]` (sıra aynı: kök neden ilk) |
| `stacktrace.frames[]` | `frames[]` (sıra aynı: en eski çağrı ilk) |
| `threads` | `threads` |
| `breadcrumbs` (dizi ya da `values`) | `breadcrumbs` |
| `message`, `logentry` | `message.formatted`, `message.template` |
| `request` | `request` (başlıklar sunucuda da maskelenir) |
| `user` | `user` |
| `contexts.device`, `os`, `runtime`, `browser`, `app`, `gpu`, `culture`, `trace`, `cloud_resource` | aynı adlı İzci bağlamları |
| `contexts.response` | `response.status_code` |
| diğer `contexts.*` | `contexts.custom` |
| `tags`, `extra`, `fingerprint`, `sdk`, `modules` | aynı alanlar |
| `level`: `fatal`/`critical`, `error`, `warning`, `info`/`log`, `debug` | `LEVEL_*` karşılıkları |

## Uç noktalar

| Yol | Açıklama |
|---|---|
| `POST /api/{proje}/envelope/` | Güncel SDK'ların kullandığı zarf (envelope) biçimi. |
| `POST /api/{proje}/store/` | Eski SDK'ların tek olay biçimi. |

Anahtar şu sırayla aranır: `X-Sentry-Auth` başlığındaki `sentry_key`, `Authorization`, `?sentry_key=` sorgu parametresi, zarf başlığındaki `dsn`. Gövde `gzip`, `deflate` ya da `br` ile sıkıştırılabilir; sıkıştırılmış gövde 20 MiB, açılmış hâli 40 MiB ile sınırlıdır. Bir zarf en çok 100 olay taşır.

Zarftaki `event` dışındaki öğeler (`transaction`, `session`, `attachment`, `client_report`, `check_in`, `log`, `profile`, `replay_*` ve benzerleri) kabul edilir ama saklanmaz. Böylece SDK hata vermez; İzci yalnızca hataları tutar. İşlem örneklemesini (`TracesSampleRate`) sıfırlamanız ağ trafiğini de azaltır.

## Hız sınırı

Sınır aşılırsa İzci `429` ile birlikte Sentry SDK'larının anladığı başlıkları döner: `Retry-After` ve `X-Sentry-Rate-Limits`. SDK'lar bu süre boyunca gönderimi kendiliğinden erteler.

## Sınırlama

Kaynak eşlemeleri (source map) ve yerel sembol dosyaları henüz işlenmez. Küçültülmüş tarayıcı kodunun yığın izi, gönderildiği biçimde görünür.
