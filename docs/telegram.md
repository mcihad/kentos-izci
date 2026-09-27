# Telegram bildirimleri

İzci; yeni, geri dönen ya da hızla tekrarlayan hataları Telegram gruplarına ve özel sohbetlere bildirir. Geliştiriciler bildirimdeki düğmelerle hatayı Telegram'dan çözebilir, yoksayabilir ya da yapay zekâ analizini başlatabilir. E-posta, Slack, Teams, webhook ve Jira kanalları [Bildirim kanalları](bildirimler.md) rehberinde anlatılır; kurallar bütün kanallar için aynıdır.

## 1. Botu oluşturun

1. Telegram'da [@BotFather](https://t.me/BotFather) ile `/newbot` komutunu çalıştırın. Aldığınız anahtarı `TELEGRAM_BOT_TOKEN` olarak verin.
2. Sunucuyu yeniden başlatın. **Uyarılar** sayfası botun kullanıcı adını gösterir.

İki çalışma şekli vardır:

| `TELEGRAM_MODE` | Nasıl çalışır | Ne zaman |
|---|---|---|
| `polling` (varsayılan) | İzci güncellemeleri Telegram'dan kendisi çeker. | Genel adresi olmayan sunucular, iç ağ kurulumları. |
| `webhook` | Telegram `{IZCI_PUBLIC_URL}/telegram/webhook` adresini çağırır; istekler `TELEGRAM_WEBHOOK_SECRET` ile doğrulanır. | `https` ile dışarıdan erişilen sunucular. |

Birden fazla İzci örneği çalışıyorsa polling'i yalnız biri yapar (veritabanı kilidiyle); bildirim gönderimini hepsi paylaşır.

## 2. Sohbeti bağlayın

1. Botu bildirimlerin gideceği gruba ekleyin. Forum konuları olan bir grupta, bildirimlerin gideceği konunun içinde çalışın.
2. İzci'de **Uyarılar › Sohbet bağla** ile tek kullanımlık bir kod alın. Kod 30 dakika geçerlidir; isterseniz sohbeti tek bir projeyle sınırlayabilirsiniz.
3. Gruba `/baglan KOD` yazın.

Bot sohbetin bağlandığını yanıtlar. Organizasyonun **ilk** sohbeti bağlandığında iki varsayılan kural oluşturulur: "Üretimde yeni hatalar" ve "Üretimde geri dönen hatalar". Her ikisi de production ortamında, error ve üstü seviyedeki hatalar için çalışır ve aynı hata için 30 dakikada bir bildirir.

Grup süper gruba dönüştüğünde Telegram sohbetin kimliğini değiştirir; İzci bunu fark eder ve bağlantıyı kendiliğinden taşır.

## 3. Hesabınızı bağlayın

Düğmeleri kullanabilmek için Telegram hesabınızın İzci hesabınıza bağlı olması gerekir:

1. İzci'de **Hesabım › Telegram** bölümünden kod alın.
2. Bota **özel mesajla** `/ben KOD` yazın.

Düğmeler yalnız bağlı ve organizasyonda en az `member` rolündeki kişiler için çalışır. Düğme verileri imzalıdır; başkası taklit edemez.

## Kurallar

**Uyarılar › Kural ekle** ile:

| Tetikleyici | Ne zaman |
|---|---|
| Yeni hata | Hata ilk kez görüldüğünde. Ortam filtresi varsa, hatanın **o ortamda** ilk kez görüldüğü an da sayılır. |
| Geri dönen hata | Çözülmüş bir hata yeniden olay aldığında. |
| Sıklık | Bir hata, belirlenen süre içinde eşik kadar olay aldığında (ör. 15 dakikada 100). |

Filtreler: proje, en düşük seviye, ortamlar, müşteriler ve yalnız yakalanmamış istisnalar. API ile etiket filtresi (`anahtar=değer`) de verilebilir. **Aynı hata için en fazla** ayarı iki bildirim arasındaki en kısa süredir. Yoksayılan hatalar bildirim üretmez.

Kurallar olay kaydıyla **aynı veritabanı işleminde** değerlendirilir ve bildirim kuyruğuna yazılır. Sunucu çökse bile bildirim kaybolmaz; yeniden başlayınca gönderilir.

## Mesaj

Bildirim şunları gösterir: seviye, kısa kimlik ve başlık, dosya:satır ve fonksiyon, müşteri ve tenant, ortam, sürüm, olay ve kullanıcı sayıları. Altında düğmeler bulunur:

| Düğme | Ne yapar |
|---|---|
| **Aç** | Hatayı İzci'de açar (yalnız `IZCI_PUBLIC_URL` genel bir adresse görünür). |
| **✅ Çöz**, **🔕 Yoksay**, **↩️ Yeniden aç** | Durumu değiştirir; değişiklik arayüze anında yansır ve etkinliğe "Telegram'dan" notuyla yazılır. |
| **🤖 Yapay zekâ ile analiz et** | Yapay zekâ analizini başlatır (yalnız `AI_ENABLED=true` iken). |

Aynı hata 12 saat içinde sıklık kuralını yeniden tetiklerse yeni mesaj atılmaz; mevcut mesaj güncellenir. Geri dönen hatalar her zaman yeni mesajla bildirilir.

## Gönderim ve hatalar

- Telegram'ın hız sınırlarına uyulur (sohbet başına ve `retry_after`).
- Geçici hatalarda en çok 8 kez, artan aralıklarla yeniden denenir.
- Bot gruptan atılırsa ya da engellenirse sohbet **Uyarılar** sayfasında hata mesajıyla işaretlenir.
- **Uyarılar** sayfasında her sohbetin yanındaki **Test** düğmesi, kurulumu gerçek bir hata beklemeden sınar.

## Sorun giderme

**Bot gruptaki `/baglan` komutuna yanıt vermiyor.** Grupta birden fazla bot varsa komutu `/baglan@botunuzunadi KOD` biçiminde yazın. Botun gruptan atılmadığından ve `TELEGRAM_BOT_TOKEN`'ın doğru olduğundan emin olun; **Uyarılar** sayfası botun kullanıcı adını gösteremiyorsa anahtar geçersizdir.

**Webhook modunda hiç güncelleme gelmiyor.** `IZCI_PUBLIC_URL` Telegram'dan erişilebilir bir `https` adresi olmalıdır. Ters vekil `/telegram/webhook` yolunu İzci'ye iletmelidir.

**Kod geçersiz diyor.** Kodlar 30 dakika geçerlidir ve tek kullanımlıktır; yeni kod alın.
