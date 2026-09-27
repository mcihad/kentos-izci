# Sentry SDK'larıyla kullanım

İzci, Sentry SDK'larının kullandığı protokolü anlar. Sentry SDK'sı kullanan bir uygulamayı İzci'ye bağlamak için **yalnız DSN'i değiştirmek** yeter; kod değişmez. Sentry olayları İzci olay modeline çevrilir ve gRPC ile gelen olaylarla aynı yoldan geçer: aynı gruplama, uyarılar, müşteri analizi ve yapay zekâ.

Hatalar dışında SDK'ların hata izleme için gönderdikleri de saklanır: **ekler** (ekran görüntüsü, günlük dosyası), **kullanıcı geri bildirimi**, **oturumlar** (sürüm sağlığı) ve zamanlanmış görevlerin **check-in**'leri ([cron izleme](cron.md)).

## Denenmiş SDK'lar

Uyumluluk, gerçek SDK'ların gönderdiği zarflarla denenir. Bu zarflar depoda test verisi olarak durur ve her değişiklikte yeniden işlenir (`internal/ingest/sentry/testdata/sdk`).

| SDK | Sürüm | Denenenler |
|---|---|---|
| Sentry .NET (konsol, ASP.NET Core) | 6.11.1 | istisna zinciri, `ILogger` hataları, istek bilgisi, ek, geri bildirim, oturum, yakalanmamış çökme |
| `@sentry/browser` | 11.0.0 | yakalanan ve yakalanmamış hata, reddedilen promise, geri bildirim, oturum |
| `@sentry/node` | 11.0.0 | hata, ek, geri bildirim, oturum, span |
| `sentry-python` | 2.70.0 | hata (kod bağlamı ve yerel değişkenlerle), ek, işlem |
| `sentry` (Dart, Flutter'ın temeli) | 9.30.1 | hata, ek, geri bildirim |
| `sentry` (Rust) | 0.49.3 | hata, mesaj, panic |
| `sentry-go` | 0.49.0 | hata, mesaj, panic |

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
    o.SendDefaultPii = true;   // kullanıcı IP'si ve istek başlıkları
    o.TracesSampleRate = 0;    // İzci performans verisi saklamaz
});

// İstek boyunca müşteri ve tenant:
SentrySdk.ConfigureScope(s =>
{
    s.SetTag("customer", "sivas-belediyesi");
    s.SetTag("customer_name", "Sivas Belediyesi");
    s.SetTag("tenant", "sivas");
});
```

`ILogger` ile yazılan `Error` ve üstü kayıtlar olay olur; şablon ve parametreleri (`"Ödeme {Provider} tarafından reddedildi"`) ayrı saklanır. .NET yığın izlerinde satır numarası, uygulamayla birlikte dağıtılan PDB dosyalarından okunur (portable PDB, yayın klasöründe varsayılan olarak bulunur).

**Tarayıcı (JavaScript, React)**

```ts
import * as Sentry from "@sentry/browser"; // ya da @sentry/react

Sentry.init({
  dsn: "https://izk_…@izci.example.com/4",
  environment: "production",
  release: "kalem-web@2.3.0",
  integrations: [Sentry.feedbackIntegration()], // isteğe bağlı: geri bildirim düğmesi
});
Sentry.setTag("customer", musteri.kod);
```

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
  o.attachScreenshot = true; // hata anının ekran görüntüsü, olayın eki olarak
}, appRunner: () => runApp(const App()));
Sentry.configureScope((s) => s.setTag('customer', 'yozgat-belediyesi'));
```

**Rust**

```rust
let mut options = sentry::ClientOptions::default();
options.release = sentry::release_name!();
options.environment = Some("production".into());
let _guard = sentry::init(("https://izk_…@izci.example.com/7", options));
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

## Hangi veri ne olur

| SDK'nın gönderdiği | İzci'de |
|---|---|
| Hata ve mesaj (`event`) | Hata olarak gruplanır; uyarı, müşteri analizi ve yapay zekâ analizine girer. |
| Ek (`attachment`): `AddAttachment`, ekran görüntüsü, görünüm hiyerarşisi | Olay sayfasında **Ekler** bölümü: görseller önizlenir, metinler açılır, diğerleri indirilir. |
| Kullanıcı geri bildirimi (`feedback`; eski SDK'larda `user_report`) | **Geri bildirim** sayfası ve hatanın **Geri bildirim** sekmesi; ilgili olay biliniyorsa hataya bağlanır. |
| Oturum (`session`, sunucu SDK'larında `sessions`) | **Projeler › Sürümler**: çökmesiz oturum ve kullanıcı oranı, oturum sayısı, benimsenme. |
| İstemci raporu (`client_report`) | SDK'nın göndermeden attığı verinin sayısı, [metrik](operasyon.md#metrikler) olarak. |
| Kaynak eşlemesi ve sembol bilgisi (`debug_meta`): debug kimlikli betikler, Flutter uygulama görüntüsü | Yığın izleri yüklenen kaynak eşlemeleri ve sembol dosyalarıyla özgün koda çevrilir; bkz. [Kaynak eşlemeleri ve semboller](kaynak-eslemeleri.md). |
| Cron kontrolü (`check_in`): `CaptureCheckIn`, `withMonitor`, `@monitor` | **Zamanlanmış görevler**: çalışmalar, kaçırılan ve zaman aşımına uğrayan çalışmalar; sorun olunca hata kaydı ve uyarı. Bkz. [Zamanlanmış görev izleme](cron.md). |
| Performans işlemleri ve span'ler, profiller, oturum kayıtları (replay), loglar | Kabul edilir, saklanmaz. Bunlar Sentry'nin hata izleme dışındaki ürünleridir. |

Saklanmayan öğeler için SDK hata almaz. Ağ trafiğini azaltmak için örneklemeyi kapatın (`TracesSampleRate = 0`, `replaysSessionSampleRate: 0` gibi).

### Ekler

- Bir ekin boyutu `INGEST_ATTACHMENT_MAX_BYTES` (varsayılan 10 MiB) ile sınırlıdır; daha büyük dosyalar atılır.
- Ekler olaylarla aynı süre saklanır (`RETENTION_DAYS`).
- Ekran görüntüleri ve dosyalar gizleme kurallarından **geçmez**. Kişisel veri içerebilirler; gerekmiyorsa **Projeler › proje › Gizlilik** sayfasında "Olay eklerini sakla" seçeneğini kapatın.
- Ekler izci arayüzünde oturum açmış organizasyon üyelerine sunulur. HTML, SVG ve betikler tarayıcıda çalıştırılmaz, yalnız indirilir.
- Oran sınırına ya da [gelen veri filtrelerine](filtreler.md) takılan olayların ekleri de atılır.

### Kullanıcı geri bildirimi

Geri bildirim metni olaylardaki gibi maskelenir (kart numarası, TCKN, parola vb.). Ad ve e-posta, kullanıcının kendisinin yazdığı iletişim bilgisidir ve olduğu gibi saklanır. Geri bildirimler projenin oran sınırına sayılır.

### Sürüm sağlığı

Bir oturum; mobil ya da masaüstü uygulamanın bir çalışması, tarayıcıda bir sayfa ziyareti ya da sunucuda bir istektir. İzci her sürüm ve ortam için saatlik olarak başlayan, hata içeren, çöken ve anormal biten oturumları sayar:

- **Çökmesiz oturum:** çökmeyen oturumların oranı.
- **Çökmesiz kullanıcı:** hiç çökme yaşamayan kullanıcıların oranı. Kullanıcı, oturumun `did` değeridir (kullanıcı kimliği ya da kurulum kimliği) ve yalnız anahtarlı özeti saklanır.
- **Benimsenme:** projenin dönem içindeki oturumlarının bu sürümden gelen payı.

`release` değeri olmayan oturumlar sayılmaz. Beş günden eski oturum güncellemeleri atılır. Hiç hata göndermemiş ama oturum bildiren sürümler de listede görünür.

## Eşleme

| Sentry | İzci |
|---|---|
| `exception.values[]` | `exceptions[]` (sıra aynı: kök neden ilk) |
| `stacktrace.frames[]` | `frames[]` (sıra aynı: en eski çağrı ilk) |
| `threads` | `threads`; istisnanın yığın izi yoksa mevcut iş parçacığının yığını gösterilir |
| `breadcrumbs` (dizi ya da `values`) | `breadcrumbs` |
| `message`, `logentry` | `message.formatted`, `message.template` |
| `request` | `request` (başlıklar sunucuda da maskelenir) |
| `user` | `user`; `ip_address: "{{auto}}"` isteği gönderen adres olur |
| `contexts.device`, `os`, `runtime`, `browser`, `app`, `gpu`, `culture`, `trace`, `cloud_resource` | aynı adlı İzci bağlamları; İzci'nin ayrı alan olarak tutmadığı bilgiler (ör. .NET'in `device.timezone`) kaybolmaz, ilgili kartta gösterilir |
| .NET `contexts["Current Culture"]` | `culture` |
| `contexts.response` | `response.status_code` |
| diğer `contexts.*` | `contexts.custom` |
| `tags`, `extra`, `fingerprint`, `sdk`, `modules` | aynı alanlar |
| `level`: `fatal`/`critical`, `error`, `warning`, `info`/`log`, `debug` | `LEVEL_*` karşılıkları |
| `platform: "other"` ya da `"native"` | SDK adından: Dart, Rust, .NET … |

## Uç noktalar

| Yol | Açıklama |
|---|---|
| `POST /api/{proje}/envelope/` | Güncel SDK'ların kullandığı zarf (envelope) biçimi. |
| `POST /api/{proje}/store/` | Eski SDK'ların tek olay biçimi. |
| `POST /api/{proje}/cron/{izleyici}/{anahtar}/` | SDK'sı olmayan işler için check-in (GET de kabul edilir); bkz. [HTTP ile](cron.md#http-ile). |
| `/api/0/…` | `sentry-cli` ve derleme eklentilerinin yükleme uçları; bkz. [Kaynak eşlemeleri](kaynak-eslemeleri.md#uç-noktalar). |

Anahtar şu sırayla aranır: `X-Sentry-Auth` başlığındaki `sentry_key`, `Authorization`, `?sentry_key=` sorgu parametresi, zarf başlığındaki `dsn`. Gövde `gzip`, `deflate`, `br` ya da `zstd` ile sıkıştırılabilir; sıkıştırılmış gövde 20 MiB, açılmış hâli 40 MiB ile sınırlıdır. Bir zarf en çok 100 olay taşır.

**Tarayıcılar:** Anahtarın izinli kökenleri boşsa her siteden gelen istek kabul edilir (Sentry'deki gibi). Anahtar zaten sayfa kaynağında göründüğünden bu liste yalnız başka sitelerin tarayıcılarını engeller. Tarayıcı uygulamalarında kendi adreslerinizi yazmanız önerilir (bkz. [Hata gönderme](ingest.md)).

**Cihaz saati:** Zarf, SDK'nın gönderme anını (`sent_at`) taşır. Cihaz saati sunucudan 55 dakikadan fazla sapıyorsa olayın, izlerin ve oturumların zamanları bu farka göre düzeltilir. Yanlış saatli telefonlardan gelen hatalar böylece doğru zamanda görünür.

## Hız sınırı

Sınır aşılırsa İzci `429` ile birlikte Sentry SDK'larının anladığı başlıkları döner: `Retry-After` ve `X-Sentry-Rate-Limits`. Başlık hata ve ek kategorilerini kapsar; SDK'lar bu süre boyunca gönderimi kendiliğinden erteler.

## Sınırlamalar

- **Sembol çözme kısmen var:** JavaScript kaynak eşlemeleri ile Flutter ve Dart sembol dosyaları işlenir ([Kaynak eşlemeleri ve semboller](kaynak-eslemeleri.md)). .NET PDB yüklemeleri (`SentryUploadSymbols`), Android ProGuard/R8 eşlemeleri ve diğer yerel sembol dosyaları işlenmez; PDB'leri uygulamayla dağıtmayan .NET uygulamalarında satır numarası olmaz.
- **Ek uç noktalar yok:** CSP raporları (`/api/{proje}/security/`), minidump (`/api/{proje}/minidump/`), Unreal ve OTLP uç noktaları sunulmaz.
- **Eski rapor penceresi yok:** JavaScript SDK'sının `showReportDialog()` penceresi Sentry'nin sunduğu bir sayfayı yükler; İzci'de çalışmaz. Yerine geri bildirim düğmesini (`feedbackIntegration`) ya da `captureFeedback` çağrısını kullanın.
- **Sentry'nin web API'si kısmen var:** Yalnız `sentry-cli`, derleme eklentileri ve `sentry_dart_plugin`'in kaynak eşlemesi, sembol, sürüm ve dağıtım yüklemek için kullandığı uçlar sunulur ([Uç noktalar](kaynak-eslemeleri.md#uç-noktalar)). Hataları ya da olayları okuyan Sentry API'leri yoktur; bunun için İzci'nin kendi API'si ve [MCP sunucusu](mcp.md) vardır.
