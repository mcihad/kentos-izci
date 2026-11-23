# Yapılacaklar

Sentry ile karşılaştırmada öne çıkan, KentOS için değeri yüksek eksikler (öncelik sırasıyla).

- [x] **Cron izleme (check-in).** Sentry SDK'ları (ör. .NET `CaptureCheckIn`) `check_in` öğesi gönderebiliyor. Tahsilat, rapor gibi zamanlanmış işler çalışmadığında, hata verdiğinde ya da süreyi aştığında uyarı (Telegram) üretilsin. Zamanlama (crontab/aralık), gecikme payı, en uzun çalışma süresi, ardışık hata/başarı eşikleri desteklensin.
- [x] **Kaynak eşlemesi (source map).** Web uygulamalarında küçültülmüş yığın izleri okunamıyor; JavaScript source map'leri ve Flutter karıştırma (obfuscation) sembolleri işlensin.
- [x] **Yeni bildirim kanalları.** Şu an yalnız Telegram var. E-posta, Slack, Microsoft Teams, webhook ve Jira (hatadan iş kaydı) eklensin.
- [x] **Gelen veri filtreleri.** Tarayıcı eklentisi hataları, botlar, eski tarayıcılar, localhost, belirli hata mesajları, IP adresleri ve sürümler için filtreler. Şu an yalnız ortam filtresi var.
- [x] **İki adımlı doğrulama.** SaaS için gerekli: TOTP (ve kurtarma kodları).
- [x] **Sahiplik kuralları.** Yol, modül, URL ve etikete göre sahiplik kuralları; CODEOWNERS içe aktarma, otomatik atama ve hatayı getiren commit'in (suspect commit) bulunması.
