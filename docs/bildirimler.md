# Bildirim kanalları

İzci yeni, geri dönen ya da hızla tekrarlayan hataları uyarı kurallarıyla kanallara bildirir. Kanallar **Uyarılar** sayfasında **Kanal ekle** ile eklenir:

| Kanal | Ne olur | Gereken |
|---|---|---|
| [Telegram](telegram.md) | Gruba ya da özel sohbete mesaj; düğmelerle çözme, yoksayma, yapay zekâ analizi. | Sunucuda `TELEGRAM_BOT_TOKEN` |
| [E-posta](#e-posta) | Alıcılara e-posta; aynı hatanın bildirimleri tek konuşmada toplanır. | Sunucuda `SMTP_*` ayarları |
| [Slack](#slack) | Slack kanalına mesaj. | Slack incoming webhook adresi |
| [Microsoft Teams](#microsoft-teams) | Teams kanalına kart. | Teams Workflows webhook adresi |
| [Webhook](#webhook) | Sizin sisteminize imzalı JSON. | Bir HTTP adresi |
| [Jira](#jira) | Hata için iş kaydı; sonraki bildirimler kayda yorum olur. | Jira adresi, proje ve API anahtarı |

Kanal eklemek ve düzenlemek yönetici rolü ister. Organizasyonun ilk kanalıyla (Jira dışında) iki temel kural da eklenir: üretimdeki yeni ve geri dönen hatalar.

## Kurallar

Kurallar kanal türünden bağımsızdır: bir kural, seçilen bütün kanallara bildirir. Tetikleyiciler (yeni hata, geri dönen hata, sıklık), filtreler (proje, seviye, ortam, müşteri, yalnız işlenmemiş istisnalar) ve **Aynı hata için en fazla** ayarı [Telegram rehberinde](telegram.md#kurallar) anlatılır.

Kurallar olay kaydıyla aynı veritabanı işleminde değerlendirilir ve bildirim kuyruğuna yazılır; sunucu yeniden başlasa bile bildirim kaybolmaz. Birden çok İzci sunucusu varsa kuyruğu birlikte boşaltırlar.

Her kanalın yanındaki **Test** düğmesi, gerçek bir hata beklemeden kurulumu sınar. Jira'da test yalnız bağlantıyı ve projeyi kontrol eder, iş kaydı açmaz.

## E-posta

Sunucu yöneticisi SMTP ayarlarını yapar ([Yapılandırma](yapilandirma.md#e-posta-ve-diğer-bildirim-kanalları)):

```sh
SMTP_HOST=smtp.kentos.com.tr
SMTP_PORT=587
SMTP_USERNAME=izci@kentos.com.tr
SMTP_PASSWORD=…
SMTP_FROM=İzci <izci@kentos.com.tr>
```

Kanal en çok 20 alıcıya gider. Konu satırı hatayı özetler, ör. *[İzci] Yeni hata: KALEM-1A NullReferenceException: …*. İleti düz metin ve HTML olarak gönderilir; hatanın türü, mesajı, konumu, müşteri, ortam ve sürüm bilgisi ile İzci'de açma bağlantısını içerir. Aynı hatanın sonraki bildirimleri ilk e-postaya yanıt olarak gider; e-posta programları bunları tek konuşmada gösterir.

SMTP sunucusu bir alıcıyı reddederse (5xx yanıtı) bildirim bırakılır ve kanal **Uyarılar** sayfasında hatayla işaretlenir; geçici hatalarda (4xx) yeniden denenir.

## Slack

1. [api.slack.com/apps](https://api.slack.com/apps) adresinde bir uygulama oluşturun (**From scratch**).
2. **Incoming Webhooks** özelliğini açın, **Add New Webhook to Workspace** ile bildirimlerin gideceği kanalı seçin.
3. Verilen `https://hooks.slack.com/services/…` adresini İzci'de **Kanal ekle › Slack** penceresine yapıştırın.

Mesajda seviye, başlık, konum, müşteri, ortam, sürüm ve sayılar bulunur; kısa kimlik İzci'deki hataya bağlantıdır. Slack webhook'ları mesaj güncelleyemediği için her tetiklenme yeni mesajdır; sıklığı kuraldaki **Aynı hata için en fazla** ayarıyla sınırlayın.

## Microsoft Teams

Microsoft, Teams'in eski Incoming Webhook bağlayıcıları yerine Workflows kullanılmasını istiyor:

1. Teams'te kanalın **⋯ › Workflows** menüsünden **Post to a channel when a webhook request is received** (Bir webhook isteği alındığında kanala gönder) şablonunu seçin.
2. Akışı kaydedin ve verilen adresi kopyalayın.
3. İzci'de **Kanal ekle › Microsoft Teams** penceresine yapıştırın.

İzci bir Adaptive Card gönderir: başlık, hata mesajı, konum, bilgiler tablosu ve **Hatayı aç** düğmesi. Eski bağlayıcı adresleri de aynı biçimi kabul ettiği için çalışmaya devam eder.

## Webhook

Kendi sistemleriniz (olay yönetimi, nöbet çizelgesi, iç panolar) için. İzci her bildirimde adrese bir `POST` isteği atar:

```json
{
  "event": "issue.created",
  "delivery_id": "01a0e181-0ed5-72f8-bb5d-f9db9a31985d",
  "sent_at": "2026-09-27T06:15:26Z",
  "channel": {"id": "…", "name": "Olay yönetimi"},
  "organization": {"slug": "kentos", "name": "KentOS"},
  "project": {"slug": "kalem", "name": "KentOS Kalem"},
  "issue": {
    "id": "…", "short_id": "KALEM-16", "url": "https://izci.example.com/o/kentos/issues/KALEM-16",
    "title": "TahsilatException: Banka yanıtı zaman aşımına uğradı", "type": "TahsilatException",
    "value": "Banka yanıtı zaman aşımına uğradı", "culprit": "TahsilatService.Odeme in TahsilatService.cs",
    "filename": "Services/TahsilatService.cs", "lineno": 118, "level": "error", "status": "unresolved",
    "unhandled": false, "times_seen": 1, "users": 0, "customers": 1,
    "first_seen": "2026-09-27T06:15:26Z", "last_seen": "2026-09-27T06:15:26Z"
  },
  "latest_event": {"environment": "production", "release": "kalem@2.3.0", "customer": "Tokat Belediyesi"},
  "rule_id": "…"
}
```

| `event` | Ne zaman |
|---|---|
| `issue.created` | Yeni hata kuralı |
| `issue.regressed` | Geri dönen hata kuralı |
| `issue.frequency` | Sıklık kuralı; gövdede `trigger.threshold` ve `trigger.window_seconds` |
| `test` | **Test** düğmesi; gövdede yalnız `message` |

Başlıklar: `X-Izci-Event` (olay türü), `X-Izci-Delivery` (bildirimin kimliği; yeniden denemelerde aynıdır), `X-Izci-Timestamp` (Unix saniyesi) ve `X-Izci-Signature`.

### İmzayı doğrulama

Kanal eklenirken bir imza anahtarı (`whsec_…`) bir kez gösterilir. İmza, `zaman damgası + "." + gövde` metninin bu anahtarla HMAC-SHA256 özetidir: `v1=<hex>`. Gövdeyi ayrıştırmadan önce, alındığı hâliyle doğrulayın ve beş dakikadan eski istekleri reddedin.

```javascript
// Node.js
import crypto from "node:crypto";

function verify(headers, rawBody, secret) {
  const ts = headers["x-izci-timestamp"];
  const sig = headers["x-izci-signature"] ?? "";
  if (Math.abs(Date.now() / 1000 - Number(ts)) > 300) return false;
  const expected = "v1=" + crypto.createHmac("sha256", secret).update(`${ts}.${rawBody}`).digest("hex");
  return sig.length === expected.length && crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(sig));
}
```

```csharp
// .NET
using System.Security.Cryptography;
using System.Text;

static bool Verify(string timestamp, string signature, string body, string secret)
{
    if (Math.Abs(DateTimeOffset.UtcNow.ToUnixTimeSeconds() - long.Parse(timestamp)) > 300) return false;
    using var hmac = new HMACSHA256(Encoding.UTF8.GetBytes(secret));
    var hash = hmac.ComputeHash(Encoding.UTF8.GetBytes($"{timestamp}.{body}"));
    var expected = "v1=" + Convert.ToHexStringLower(hash);
    return CryptographicOperations.FixedTimeEquals(Encoding.UTF8.GetBytes(expected), Encoding.UTF8.GetBytes(signature));
}
```

```go
// Go
func verify(r *http.Request, body []byte, secret string) bool {
	ts := r.Header.Get("X-Izci-Timestamp")
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(n, 0)).Abs() > 5*time.Minute {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "." + string(body)))
	want := "v1=" + hex.EncodeToString(m.Sum(nil))
	return hmac.Equal([]byte(want), []byte(r.Header.Get("X-Izci-Signature")))
}
```

Yanıt olarak herhangi bir `2xx` durumu yeterlidir. `429`, `408` ve `5xx` yanıtları yeniden denenir (`Retry-After` başlığına uyulur); diğer `4xx` yanıtları bildirimi bırakır ve kanalı hatayla işaretler. Aynı bildirim birden fazla kez gelebilir; `X-Izci-Delivery` ile tekilleştirin.

## Jira

**Jira Cloud:** [id.atlassian.com › Security › API tokens](https://id.atlassian.com/manage-profile/security/api-tokens) sayfasından bir API anahtarı oluşturun. İzci'de hesabın e-postasını ve anahtarı girin.

**Jira Server ve Data Center:** Profil › **Personal Access Tokens** sayfasından bir anahtar oluşturun; e-posta alanını boş bırakın.

Hesabın projede iş kaydı oluşturma ve yorum yapma izni olmalıdır. **Kayıt türü**, Jira'daki adıyla yazılır (ör. `Bug`, Türkçe arayüzlü Jira'larda `Hata` olabilir). Kaydederken İzci Jira'ya bağlanıp hesabı ve projeyi kontrol eder; anahtar şifreli saklanır ve bir daha gösterilmez.

**Kurallarla:** Bir kural Jira kanalına bildirdiğinde hata için bir iş kaydı açılır: başlık `KALEM-16: TahsilatException: …`, açıklamada hatanın mesajı, konumu, müşteri, ortam, sürüm, sayılar ve İzci bağlantısı bulunur; `izci` etiketi ve kanalda tanımlı etiketler eklenir. Aynı hatanın sonraki bildirimleri (geri dönüş, sıklık) yeni kayıt açmaz, mevcut kayda yorum ekler.

**Elle:** Hata sayfasının yan panelindeki **Jira'da kayıt aç** düğmesi (üye rolü) hatayı seçilen Jira kanalının projesine gönderir. Bir hata için her kanalda tek kayıt açılır; açılan kayıtlar aynı panelde **İş kayıtları** başlığı altında ve hatanın etkinlik akışında görünür.

## Gönderim ve hatalar

- Geçici hatalarda en çok 8 kez, artan aralıklarla yeniden denenir; hız sınırı yanıtlarındaki bekleme süresine uyulur.
- Kalıcı hatalar (iptal edilmiş webhook, yanlış anahtar, reddedilen alıcı) bildirimi bırakır; kanal **Uyarılar** sayfasında son hatayla işaretlenir. Kanalı düzelttikten sonra **Test** ile sınayın.
- Sessize alınan kanallara bildirim gitmez.
- Slack, Teams, webhook ve Jira kanalları varsayılan olarak sunucunun iç ağındaki adreslere (`10.x`, `192.168.x`, `localhost` …) istek atamaz; organizasyon yöneticileri bu yolla sunucunun ağına erişemez. Şirket içi bir Jira ya da webhook alıcısı için sunucu yöneticisi `NOTIFY_ALLOW_PRIVATE_NETWORKS=true` ayarlar.
- Webhook adresleri, imza anahtarları ve Jira anahtarları veritabanında şifreli (`IZCI_SECRET_KEY` ile) saklanır; arayüzde yalnız adresin kısaltması görünür.

## Sorun giderme

**E-posta kanalı eklenemiyor.** **Kanal ekle** menüsünde e-posta "Sunucuda yapılandırılmamış" görünüyorsa `SMTP_HOST` ve `SMTP_FROM` tanımlı değildir.

**"the destination resolves to a private … address" hatası.** Adres iç ağda; yukarıdaki `NOTIFY_ALLOW_PRIVATE_NETWORKS` ayarına bakın. Sunucu dışarı bir HTTP vekili (`HTTPS_PROXY`) üzerinden çıkıyorsa ve vekil iç ağdaysa da bu ayar gerekir.

**Jira "hesap bilgilerini kabul etmedi".** Jira Cloud'da e-posta ve API anahtarı birlikte, Server/Data Center'da yalnız kişisel erişim anahtarı kullanılır. Anahtarın süresinin dolmadığından emin olun.

**Jira "isteği reddetti (400)".** Mesajda Jira'nın gerekçesi yazar; çoğu zaman kayıt türünün adı projede yoktur ya da proje zorunlu bir alan istiyordur.
