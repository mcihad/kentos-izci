# Zamanlanmış görev izleme

Tahsilat, rapor üretimi, gece aktarımı gibi düzenli çalışan işler sessizce durabilir: sunucu yeniden başlar, zamanlayıcı bozulur, iş hata verip biter ya da takılıp kalır. İzci her iş için bir **izleyici** tutar. İş başlarken ve biterken İzci'ye haber verir (check-in); İzci işin zamanında çalışmadığını, hata ile bittiğini ya da çok uzun sürdüğünü fark eder, bir hata kaydı açar ve [uyarı kurallarıyla](telegram.md) haber verir.

İzci, Sentry'nin cron izleme protokolünü kullanır: Sentry SDK'larındaki `CaptureCheckIn`, `withMonitor` ve `@monitor` olduğu gibi çalışır.

## Nasıl çalışır

**İzleyici:** Bir projeye aittir ve projede kısa adıyla (`monitor_slug`) tanınır, ör. `gunluk-tahsilat`. Zamanlamayı (crontab ya da aralık), saat dilimini, gecikme payını, en uzun çalışma süresini ve eşikleri tutar.

**Check-in:** Görevin gönderdiği bildirim. Üç durumu vardır:

| Durum | Anlamı |
|---|---|
| `in_progress` | Görev başladı. |
| `ok` | Görev başarıyla bitti. |
| `error` | Görev hata ile bitti. |

Bir çalışma genellikle iki check-in'dir: başlarken `in_progress`, biterken aynı `check_in_id` ile `ok` ya da `error`. Kısa işler yalnız bitişi gönderebilir. Süre gönderilmezse İzci iki check-in arasındaki süreyi kullanır. `check_in_id` olmadan gelen bir kapanış, o ortamda hâlâ süren en yeni çalışmayı bitirir.

**İzci'nin kendi kaydettikleri:**

- **Kaçırıldı:** Beklenen zaman ve gecikme payı geçtiği hâlde check-in gelmediyse. Uzun bir kesintiden sonra kaçırılan her dilim için ayrı kayıt açılmaz; tek bir kaçırılmış çalışma kaydedilir ve sonraki çalışma şimdiden sonraki ilk zamana alınır.
- **Zaman aşımı:** `in_progress` bir çalışma en uzun çalışma süresini aştıysa. Zaman aşımına uğramış çalışmaya sonradan gelen `ok` bir şey değiştirmez.

Kontrol yarım dakikada bir yapılır. Birden çok İzci sunucusu aynı veritabanını kullanıyorsa her çalışma yalnız bir kez işaretlenir.

**Ortamlar:** Her ortam (`production`, `staging` …) ayrı izlenir. Bir ortamdan ilk check-in gelmeden o ortamda hiçbir çalışma kaçırılmış sayılmaz.

**Hata kaydı ve uyarı:** Art arda **hata eşiği** kadar sorunlu (hatalı, kaçırılmış ya da zaman aşımına uğramış) çalışmada, izleyici ve ortam için bir hata kaydı açılır: *"Zamanlanmış görev başarısız: Günlük tahsilat"*. Sonraki her sorun aynı kayda bir olay ekler. Olaylar şu etiketleri taşır:

| Etiket | Değer |
|---|---|
| `monitor.slug` | İzleyicinin kısa adı |
| `monitor.status` | `error`, `missed` ya da `timeout` |

Bu kayıtlar diğer hatalar gibi aranır (`monitor.slug:gunluk-tahsilat`), uyarı kurallarına girer ve Telegram'a bildirilir. Varsayılan kural `production` ortamındaki yeni ve geri dönen hataları bildirir.

Art arda **toparlanma eşiği** kadar başarılı çalışmadan sonra kayıt kendiliğinden çözülür; hatanın etkinlik akışında görünür.

**Duraklatma:** Duraklatılan izleyici check-in'leri kaydetmeyi sürdürür, ama kaçırılan çalışma saymaz ve hata kaydı açmaz. Sürdürüldüğünde sonraki çalışma şimdiden itibaren beklenir. Bakım çalışmaları sırasında işe yarar.

## Zamanlama

**Crontab:** Beş alan: dakika, saat, ayın günü, ay, haftanın günü (0 ya da 7 Pazar). İzleyicinin saat diliminde okunur; yaz saati geçişleri hesaba katılır.

| İfade | Anlamı |
|---|---|
| `0 3 * * *` | Her gün 03:00 |
| `30 2 * * 1-5` | Hafta içi her gün 02:30 |
| `*/10 * * * *` | 10 dakikada bir |
| `0 */6 * * *` | 6 saatte bir, saat başında |
| `15 4 1 * *` | Her ayın 1'i 04:15 |
| `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly` | Kısaltmalar |

**Aralık:** Her `N` dakika, saat, gün, hafta, ay ya da yıl. Aralık bir önceki çalışmadan sayılır: 2 saatte bir çalışan bir iş 09:10'da çalıştıysa sonraki çalışma 11:10'da beklenir.

**Saat dilimi:** IANA adıyla, ör. `Europe/Istanbul`. Verilmezse `UTC`.

## Ayarlar

| Ayar | Anlamı | Varsayılan | Sınır |
|---|---|---|---|
| Gecikme payı (`checkin_margin`) | Bu kadar dakika gecikirse çalışma kaçırılmış sayılır | 1 dk | 30 gün |
| En uzun çalışma (`max_runtime`) | Bu kadar dakikada bitmezse zaman aşımı sayılır | 30 dk | 7 gün |
| Hata eşiği (`failure_issue_threshold`) | Art arda kaç sorunda kayıt açılır | 1 | 720 |
| Toparlanma eşiği (`recovery_threshold`) | Art arda kaç başarıda kayıt çözülür | 1 | 720 |

Bir çalışma, beklenen zamandan gecikme payından daha erken gelirse (elle başlatılan ya da fazladan bir çalışma) beklenen zamanı karşılamış sayılmaz, ama sonraki çalışmanın zamanı yine ondan hesaplanır.

## İzleyici oluşturma

**Kendiliğinden:** `monitor_config` taşıyan ilk check-in izleyiciyi oluşturur. Sonraki check-in'lerdeki değişiklikler izleyiciyi günceller; böylece zamanlama kodla birlikte değişir. `monitor_config` olmadan, var olmayan bir izleyiciye gelen check-in reddedilir (HTTP 404, gRPC `NOT_FOUND`).

**Arayüzden:** **Zamanlanmış görevler › Yeni izleyici** (yönetici rolü). Kısa ad boş bırakılırsa addan üretilir: *Günlük tahsilat* → `gunluk-tahsilat`.

İzleyici sayfası her çalışmayı, ortamların durumunu, açık hata kaydını ve seçilen istemci için hazır kurulum kodunu gösterir.

## Sentry SDK'larıyla

DSN'i [Sentry rehberindeki](sentry.md#dsn) gibi verin; başka ayar gerekmez.

**.NET:**

```csharp
var checkInId = SentrySdk.CaptureCheckIn("gunluk-tahsilat", CheckInStatus.InProgress,
    configureMonitorOptions: o =>
    {
        o.Interval("0 3 * * *");
        o.TimeZone = "Europe/Istanbul";
        o.CheckInMargin = TimeSpan.FromMinutes(5);
        o.MaxRuntime = TimeSpan.FromMinutes(30);
    });
try
{
    await RunBillingAsync();
    SentrySdk.CaptureCheckIn("gunluk-tahsilat", CheckInStatus.Ok, checkInId);
}
catch
{
    SentrySdk.CaptureCheckIn("gunluk-tahsilat", CheckInStatus.Error, checkInId);
    throw;
}
```

**Node.js:**

```javascript
await Sentry.withMonitor("gunluk-tahsilat", () => runBilling(), {
  schedule: { type: "crontab", value: "0 3 * * *" },
  timezone: "Europe/Istanbul",
  checkinMargin: 5,
  maxRuntime: 30,
});
```

**Python:**

```python
from sentry_sdk.crons import monitor

@monitor(monitor_slug="gunluk-tahsilat", monitor_config={
    "schedule": {"type": "crontab", "value": "0 3 * * *"},
    "timezone": "Europe/Istanbul",
})
def run_billing():
    ...
```

**Go (`sentry-go`):** `hub.CaptureCheckIn(&sentry.CheckIn{MonitorSlug: "gunluk-tahsilat", Status: sentry.CheckInStatusInProgress}, &sentry.MonitorConfig{Schedule: sentry.CrontabSchedule("0 3 * * *")})`.

## İzci Go SDK'sıyla

`WithMonitor` görevi çalıştırır; başında `in_progress`, sonunda görevin hatasına göre `ok` ya da `error` gönderir:

```go
err := izci.WithMonitor(ctx, "gunluk-tahsilat", &izci.MonitorConfig{
	Crontab:    "0 3 * * *",
	Timezone:   "Europe/Istanbul",
	MaxRuntime: 30 * time.Minute,
}, runBilling)
```

Tek tek göndermek için `izci.CaptureCheckIn` vardır. Ayrıntılar: [Go SDK](go-sdk.md#zamanlanmış-görevler).

## gRPC ve Connect ile

`izci.ingest.v1.IngestService/SendCheckIn` ([başvuru](api/izci.ingest.v1.md)). Connect JSON ile:

```sh
curl -X POST https://izci.example.com/izci.ingest.v1.IngestService/SendCheckIn \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer izk_…' \
  -d '{"checkIn":{"monitorSlug":"gunluk-tahsilat","status":"CHECK_IN_STATUS_OK",
       "monitorConfig":{"crontab":"0 3 * * *","timezone":"Europe/Istanbul"}}}'
```

Yanıt çalışmanın kimliğini döner (`checkInId`); `in_progress` gönderdiyseniz kapanışta bu kimliği kullanın.

## HTTP ile

SDK'sı olmayan işler (kabuk betikleri, zamanlanmış veritabanı görevleri) Sentry'nin HTTP ucunu kullanır:

```sh
URL='https://izci.example.com/api/<proje numarası>/cron/gunluk-tahsilat/izk_…/'
curl -X POST "$URL?status=in_progress"
# ... iş ...
curl -X POST "$URL?status=ok"
```

Proje numarası, DSN'in sonundaki sayıdır. Anahtar adreste yerine `X-Sentry-Auth` ya da `Authorization: Bearer` başlığıyla da verilebilir: `…/cron/gunluk-tahsilat/?status=ok`.

| Parametre | Anlamı |
|---|---|
| `status` | `in_progress`, `ok` ya da `error`. Zorunlu. |
| `check_in_id` | Çalışmanın kimliği (UUID). Yoksa kapanış süren en yeni çalışmayı bitirir. |
| `environment` | Ortam; varsayılan `production`. |
| `release` | İşin sürümü. |
| `duration` | Süre, saniye. |

İzleyici yoksa, POST gövdesinde Sentry biçiminde `monitor_config` gönderin:

```sh
curl -X POST "$URL?status=ok" -H 'Content-Type: application/json' \
  -d '{"monitor_config":{"schedule":{"type":"interval","value":1,"unit":"hour"},"checkin_margin":5}}'
```

| Yanıt | Anlamı |
|---|---|
| `202` ve `{"id": "…"}` | Kaydedildi; `id` çalışmanın kimliğidir. |
| `400` | Geçersiz durum, kimlik ya da `monitor_config`. |
| `401`, `403` | Anahtar geçersiz ya da kapsamı yetmiyor. |
| `404` | İzleyici yok ve `monitor_config` gönderilmedi. |
| `429` | Projenin oran sınırı aşıldı (`Retry-After`). |

## Sorun giderme

**İzleyici oluşmuyor:** İlk check-in `monitor_config` taşımalı ya da izleyici arayüzden oluşturulmalı. Sentry SDK'larında yapılandırma `CaptureCheckIn`'in izleyici ayarları parametresidir.

**Çalıştığı hâlde "Kaçırıldı" görünüyor:** Saat dilimini ve gecikme payını kontrol edin; `0 3 * * *` UTC'de Türkiye saatiyle 06:00'dır. Görevin gönderdiği ortam adı izleyici sayfasındaki ortamla aynı olmalı.

**Uyarı gelmiyor:** Hata kaydı açıldıysa uyarı kurallarının ortam filtresine bakın; varsayılan kural yalnız `production` ortamını bildirir. Hata eşiği 1'den büyükse ilk sorunlarda kayıt açılmaz.

**Oran sınırı:** Check-in'ler projenin oran sınırına sayılır; çok sık çalışan (dakikada birkaç kez) işlerde sınırı buna göre ayarlayın.
