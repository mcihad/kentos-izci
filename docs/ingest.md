# Hata gönderme

Uygulamalar hataları `izci.ingest.v1.IngestService` ile gönderir. Servis tek porttan üç protokolle konuşur:

| Protokol | Kimler için | Adres |
|---|---|---|
| **gRPC** | .NET, Go, Java, Dart ve diğer gRPC istemcileri | `izci.example.com:443` (TLS) ya da `host:5300` (h2c) |
| **Connect** | HTTP/1.1 ile JSON ya da protobuf; `curl` dahil her dil | `https://izci.example.com/izci.ingest.v1.IngestService/<Metot>` |
| **gRPC-Web** | Tarayıcılar | Aynı adres |

Kod üretmeden başlamanın en kısa yolu Connect JSON'dur: `POST` ile tek bir JSON gövdesi gönderilir. Tipli istemci isteyenler `proto/izci/ingest/v1` dosyalarından kod üretebilir. Sunucu gRPC reflection desteklediği için `grpcurl` ve `buf curl` proto dosyası olmadan da çalışır.

Mevcut Sentry SDK'larını kullanıyorsanız yalnız DSN'i değiştirmeniz yeter; bkz. [Sentry SDK'larıyla kullanım](sentry.md). Go için hazır bir SDK vardır: [Go SDK](go-sdk.md).

## Kimlik doğrulama

Her istek projenin **ingest anahtarını** (`izk_…`) taşır:

```
Authorization: Bearer izk_…
```

İstemci `Authorization` başlığını kullanamıyorsa `x-izci-key: izk_…` da kabul edilir. Anahtarlar **Projeler › proje › Anahtarlar** bölümünden ya da `izci key create` ile oluşturulur. Anahtar yalnız olay göndermeye (ve izin verilmişse sürüm kaydetmeye) yetkilidir, başka hiçbir veriyi okuyamaz.

Anahtarın **izinli kökenleri** (ör. `https://kalem.example.com`) tarayıcılardan gelen istekleri sınırlar: liste doluysa yalnız bu sitelerdeki sayfalar gönderebilir, boşsa her siteden kabul edilir (Sentry'deki gibi). Sunucu uygulamaları `Origin` başlığı göndermediği için bu listeden etkilenmez. Anahtar tarayıcı kodunda göründüğünden, tarayıcı uygulamalarında listeyi doldurmanız önerilir.

## Metotlar

| Metot | Ne yapar |
|---|---|
| `SendEvent` | Tek olay gönderir. Yanıt; olay kimliğini, sonucu, hatanın kısa kimliğini (`KALEM-1A`) ve bağlantısını içerir. |
| `SendEvents` | En çok 100 olayı bir istekte gönderir; her olay için ayrı sonuç döner. SDK'lar kuyruğu bununla boşaltır. |
| `RegisterRelease` | CI'dan sürüm, commit, depo ve deploy bilgisi kaydeder. Yapay zekâ analizi doğru commit'i buradan bulur. |
| `GetClientConfig` | SDK'ya örnekleme oranı, breadcrumb sınırı, maskelenecek alanlar ve hız sınırını söyler. |
| `SendCheckIn` | Zamanlanmış bir görevin çalışmasını (başladı, bitti, hata verdi) izleyicisine bildirir; bkz. [Zamanlanmış görev izleme](cron.md). |

## Olay modeli

Olay, Sentry olay modeline bilinçli olarak yakındır. En önemli alanlar:

| Alan | Açıklama |
|---|---|
| `level` | `LEVEL_DEBUG`, `LEVEL_INFO`, `LEVEL_WARNING`, `LEVEL_ERROR` (varsayılan) ya da `LEVEL_FATAL`. |
| `platform` | `dotnet`, `go`, `node`, `javascript`, `python`, `dart`, `rust`, `java`… Gruplama ve kod renklendirme bunu kullanır. |
| `environment`, `release` | Ör. `production`, `kalem@1.4.2`. Filtreler, gerileme tespiti ve yapay zekâ analizi bunlara dayanır. |
| `customer` | `{ "id": "sivas-belediyesi", "name": "Sivas Belediyesi" }`. Müşteri sağlığı ve müşteri filtreleri bundan beslenir. |
| `tenant` | `{ "id": "sivas", "name": "Sivas" }`. Müşterinin içindeki kiracı, varsa. |
| `message` | `{ "formatted": "…", "template": "Sipariş {id} bulunamadı" }`. Şablon verilirse gruplamada kullanılır. |
| `exceptions` | İstisna zinciri: **kök neden ilk, fırlatılan istisna son**. Her birinde `type`, `value`, `mechanism` ve `stacktrace` vardır. |
| `stacktrace.frames` | Çağrı yığını: **en eski çağrı ilk, hatanın oluştuğu çağrı son**. Her çerçevede `function`, `module`, `filename`, `lineno`, `inApp`, `contextLine`, `preContext`, `postContext` vardır. |
| `breadcrumbs` | Hatadan önceki adımlar (en eskisi ilk): gezinme, HTTP çağrıları, SQL, loglar. |
| `request` | `method`, `url`, `route`, `queryString`, `headers`, `body`. Hassas başlıklar sunucuda da maskelenir. |
| `user` | `id`, `email`, `username`, `ipAddress`, `segment`. |
| `contexts` | `device` (CPU, bellek, disk), `os`, `runtime`, `browser`, `app`, `process`, `database` (hatalı SQL), `cloud`, `trace` ve serbest `custom` blokları. |
| `tags`, `extra`, `fingerprint` | Serbest etiketler, ek veri ve isteğe bağlı gruplama anahtarı (bkz. [Gruplama](gruplama.md)). |
| `debugImages` | Yığın izinin çalıştığı kod: debug kimlikli betikler (`type: "sourcemap"`, `codeFile`, `debugId`) ve yerel görüntüler. Küçültülmüş kodun [kaynak eşlemeleriyle](kaynak-eslemeleri.md) çözülmesi için. |

Alanların tamamı, birimleri, sınırları ve Sentry karşılıkları [Ingest API başvurusunda](api/izci.ingest.v1.md) açıklanır. JSON'da alan adları `lowerCamelCase` ya da proto'daki `snake_case` biçiminde yazılabilir. Zamanlar RFC 3339'dur; enum'lar ad (`"LEVEL_ERROR"`) ya da sayı olarak verilebilir.

Küçültülmüş tarayıcı kodu gönderen istemciler olaya `release` ya da `debugImages` eklerse yığın izleri yüklenen kaynak eşlemeleriyle çözülür; sunucu çevirdiği çerçevelerin gönderildiği hâlini `stacktrace.rawFrames`, çeviri sorunlarını `processingErrors` alanına yazar (bu iki alan istemciden kabul edilmez).

`eventId` ve `timestamp` verilmezse sunucu üretir. İstemci her olaya bir `eventId` (UUID) verirse, ağ hatasından sonraki yeniden gönderimler tekrar sayılmaz.

## Örnekler

Bütün örnekler depodaki [`examples/`](../examples) klasöründedir, `IZCI_URL` ve `IZCI_KEY` değişkenlerini okur ve ek paket gerektirmez.

### curl (Connect JSON)

```bash
curl -sS https://izci.example.com/izci.ingest.v1.IngestService/SendEvent \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer izk_…' \
  --data @examples/event.json
```

Yanıt:

```json
{
  "result": {
    "eventId": "3a2fca47-40a0-4799-b330-2f5e17ed4f82",
    "outcome": "EVENT_OUTCOME_ACCEPTED",
    "issueId": "01a0e024-f263-72f2-9948-6515a8a2f4e1",
    "issueShortId": "KALEM-8",
    "issueUrl": "https://izci.example.com/o/kentos/issues/KALEM-8",
    "issueIsNew": true
  }
}
```

### gRPC (buf curl, grpcurl)

```bash
# Servisleri reflection ile listele (şifresiz h2c için --http2-prior-knowledge gerekir)
buf curl --protocol grpc --http2-prior-knowledge --list-methods http://localhost:5300

buf curl --protocol grpc -H 'Authorization: Bearer izk_…' \
  -d @examples/event.json https://izci.example.com/izci.ingest.v1.IngestService/SendEvent

grpcurl -H 'authorization: Bearer izk_…' -d @ izci.example.com:443 \
  izci.ingest.v1.IngestService/SendEvent < examples/event.json
```

### C# (.NET)

[`examples/csharp/IzciOrnek`](../examples/csharp/IzciOrnek/Program.cs) yakalanan bir istisnayı iç istisnalarıyla birlikte gönderir. .NET çerçeveleri en yeniden eskiye sıraladığı için liste ters çevrilir:

```bash
IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… dotnet run --project examples/csharp/IzciOrnek
# EVENT_OUTCOME_ACCEPTED KALEM-9 http://localhost:5300/o/kentos/issues/KALEM-9
```

ASP.NET Core uygulamalarında `Sentry.AspNetCore` paketini DSN ile kullanmak daha az iş gerektirir; bkz. [Sentry SDK'larıyla kullanım](sentry.md). Tipli gRPC istemcisi için `Grpc.Tools` ile `proto/izci/ingest/v1/*.proto` dosyalarından kod üretin.

### TypeScript ve Node.js

[`examples/node/send-event.mjs`](../examples/node/send-event.mjs) yalnızca `fetch` kullanır ve `error.cause` zincirini de gönderir:

```bash
IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… node examples/node/send-event.mjs
```

Tipli istemci için `@bufbuild/protoc-gen-es` ile kod üretip `@connectrpc/connect-node` (sunucu) ya da `@connectrpc/connect-web` (tarayıcı) kullanın.

### Dart ve Flutter

[`examples/dart/send_event.dart`](../examples/dart/send_event.dart) `dart:io` ile gönderir. Flutter'da aynı isteği `package:http` ile yapın ya da `sentry_flutter` kullanın.

```bash
IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… dart run examples/dart/send_event.dart
```

### Python

[`examples/python/send_event.py`](../examples/python/send_event.py) yalnız standart kütüphaneyi kullanır; `traceback` çerçeveleri zaten en eskiden yeniye sıralıdır.

```bash
IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… python3 examples/python/send_event.py
```

### Go

[Go SDK](go-sdk.md) panikleri, hata zincirlerini, HTTP isteklerini ve çalışma zamanı bilgisini kendisi toplar. Örnek uygulama: [`examples/go-basic`](../examples/go-basic/main.go).

## Sürüm kaydetme

CI'da her dağıtımdan sonra sürümü kaydedin. Yapay zekâ analizi hatayı bu commit'te inceler, [şüpheli commit](sahiplik.md#şüpheli-commit) aramasında bu commit'in satırlarına bakılır, arayüz de hangi sürümün hangi hataları getirdiğini gösterir:

```bash
curl -sS https://izci.example.com/izci.ingest.v1.IngestService/RegisterRelease \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $IZCI_KEY" \
  -d '{"version":"kalem@1.4.2","commitSha":"'"$GITHUB_SHA"'","repository":"kentos/kalem","environment":"production"}'
```

## Sonuçlar ve hatalar

`SendEvents` her olay için bir `outcome` döndürür:

| Sonuç | Anlamı |
|---|---|
| `EVENT_OUTCOME_ACCEPTED` | Saklandı. |
| `EVENT_OUTCOME_DUPLICATE` | Aynı `eventId` kısa süre önce alınmıştı; tekrar saklanmadı. |
| `EVENT_OUTCOME_FILTERED` | Projenin [gelen veri filtrelerine](filtreler.md) takıldı (ör. kayıt dışı ortam, bot, eski tarayıcı); saklanmadı, sınırlara da sayılmadı. |
| `EVENT_OUTCOME_RATE_LIMITED` | Hız ya da aylık kota aşıldı. |
| `EVENT_OUTCOME_INVALID` | Olay kullanılamaz (ör. ne mesaj ne istisna var); `error` alanı nedenini söyler. |

İsteğin kendisi başarısız olursa standart gRPC/Connect kodları döner:

| Kod | Ne zaman | İstemci ne yapmalı |
|---|---|---|
| `UNAUTHENTICATED` | Anahtar yok ya da geçersiz. | Yeniden denemeyin; anahtarı düzeltin. |
| `PERMISSION_DENIED` | Anahtarın kapsamı ya da isteğin kökeni uygun değil. | Yeniden denemeyin. |
| `RESOURCE_EXHAUSTED` | Hız ya da kota aşıldı. | Yanıttaki `RetryInfo` süresi kadar bekleyin. |
| `INVALID_ARGUMENT` | İstek bozuk. | Yeniden denemeyin. |
| `UNAVAILABLE` | Geçici durum (kuyruk dolu, kapanış). | Artan beklemeyle yeniden deneyin. |

## Sınırlar

Sınırı aşan veri reddedilmez, **kırpılır**; yanıttaki uyarılar neyin kırpıldığını söyler.

| Öğe | Sınır |
|---|---|
| Olay (açılmış hâliyle) | 1 MiB |
| Mesaj | 8 KiB |
| İstisna zinciri | 10 istisna |
| Çerçeve | istisna başına 250 (en eski 50 ve en yeni 200 korunur) |
| Breadcrumb | 100 |
| Etiket | 50 (anahtar 64, değer 200 karakter) |
| İstek gövdesi | 16 KiB |
| `SendEvents` | istek başına 100 olay |
