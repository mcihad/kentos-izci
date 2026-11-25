# Güvenlik ve KVKK

Hata kayıtları kişisel veri içerebilir: kullanıcı kimlikleri, e-postalar, IP adresleri, istek gövdeleri. İzci bu verileri **toplamadan önce maskeler**, saklarken **kiracılar arasında yalıtır** ve yapay zekâya **göndermeden önce ayıklar**. Bu sayfa hangi korumaların olduğunu ve neyin sizin elinizde olduğunu anlatır.

## Maskeleme

Olaylar saklanmadan önce sunucuda maskelenir. SDK'lar ayrıca kendi taraflarında da maskeler; sunucudaki maskeleme, eski ya da yanlış ayarlanmış istemcilere karşı son savunma hattıdır. Maskelenen değerin yerine `[Filtered]` yazılır.

**Her zaman maskelenen başlıklar:** `Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`, `X-Api-Key`, `X-Izci-Key`, `X-Sentry-Auth`, `X-Auth-Token`, `X-CSRF-Token`, `X-XSRF-Token`, `X-Amz-Security-Token`.

**Adı şunları içeren alanlar** (başlıklar, sorgu parametreleri, JSON ve form alanları, çerezler, yığın değişkenleri, ek veri):

```
password passwd parola sifre şifre secret token apikey api_key api-key authorization credential
private_key privatekey cookie session csrf xsrf card_number cardnumber kart_no cvv cvc iban
tckn tc_kimlik kimlik_no ssn otp pin_code
```

**Değerde aranan desenler** (mesajlar, breadcrumb'lar, gövdeler ve diğer serbest metin):

- `Bearer …`, `Basic …`, `token …` biçimindeki kimlik bilgileri;
- URL içindeki parolalar (`https://kullanici:parola@…`);
- `password=…`, `api_key: …` gibi anahtar–değer sırları;
- Türk IBAN'ları;
- kredi kartı numaraları (Luhn doğrulamasıyla);
- T.C. kimlik numaraları (sağlama algoritmasıyla; rastgele 11 haneli sayılar maskelenmez).

Projeye özel ayarlar **Projeler › proje › Gizlilik** bölümündedir:

| Ayar | Açıklama |
|---|---|
| Ek olarak gizlenecek alanlar | Yukarıdaki listeye eklenen alan adları (ör. `vergi_no`). |
| Gizlenmeyecek alanlar | Yanlışlıkla eşleşen alanlar (ör. `token_type`); tam adla yazılır. |
| IP adresleri | **Tam**, **anonimleştirilmiş** (IPv4 /24, IPv6 /48) ya da **hiç saklanmaz**. |
| Çerez değerleri | Kapalıysa yalnız çerez adları saklanır. |
| İstek gövdeleri | Kapalıysa gövde hiç saklanmaz. |
| Olay ekleri | Kapalıysa SDK'ların gönderdiği ekran görüntüleri ve dosyalar saklanmaz. |

**Maskelenmeyenler:** Olay ekleri (ekran görüntüleri, günlük dosyaları) ikili dosya olarak olduğu gibi saklanır; içlerindeki kişisel veri ayıklanmaz. Kullanıcı geri bildirimlerinin metni maskelenir, kullanıcının kendi yazdığı ad ve e-posta ise iletişim için olduğu gibi saklanır. Oturumlardaki kullanıcı kimlikleri (sürüm sağlığı) yalnız anahtarlı özet olarak tutulur.

## Saklama ve silme

- Olaylar `RETENTION_DAYS` gün (varsayılan 90) saklanır. Günlük bölümler bütünüyle silinir; silinen olay geri getirilemez.
- Silinen bir projenin bütün verisi arka planda temizlenir.
- [Kaynak eşlemeleri ve sembol dosyaları](kaynak-eslemeleri.md) yığın izi çözdükçe tutulur; 90 gün (ya da daha uzunsa `RETENTION_DAYS`) kullanılmayanlar silinir. Kaynak eşlemeleri çoğu zaman uygulamanın kaynak kodunu içerir; organizasyon üyeleri hata sayfasında bu kodun satırlarını görür.
- Son olayı bu süreden eski olan hatalar da; saatlik sayaçları, etiket dağılımları ve etkilenen kullanıcı kayıtlarıyla birlikte silinir. Organizasyonların kullanım istatistikleri (günlük olay sayıları) 400 gün tutulur.

## Kimlik doğrulama

- İlk kurulum sihirbazı, sunucunun günlüğüne yazdığı kurulum kodunu ister; internete açılan yeni bir sunucuyu ilk ulaşan kişi ele geçiremez. Kurulum bitmeden açık kayıtla ya da kimlik sağlayıcısıyla da hesap açılmaz.
- Açık kayıtta (`AUTH_SIGNUP=open`) hesap, e-posta adresine gönderilen tek kullanımlık bağlantı açılınca oluşur: kimse başkasının adresiyle önceden hesap açıp, adresin sahibi sonra kimlik sağlayıcısıyla girdiğinde o hesabı paylaşamaz. Kayıt e-postası kayıt olanın yazdığı hiçbir metni (ad, organizasyon adı) içermez; bir adrese 15 dakikada en çok 3 bağlantı gönderilir. Bir kullanıcı en çok `AUTH_SIGNUP_ORG_LIMIT` organizasyonun sahibi olabilir.
- Parolalar **argon2id** ile özetlenir; en az 10 karakter olmalıdır. Başarısız girişler 15 dakikalık pencerelerde sayılır: aynı istemciden bir hesaba 10, aynı istemciden bütün hesaplara 30, her yerden bir hesaba 100 hatalı deneme. Böylece bir saldırgan, hesabın sahibini başka bir adresten kilitleyemez. IPv6 istemcileri /64 ağlarına göre gruplanır; sayaçlar veritabanında tutulur, bütün sunucu örnekleri ortak görür. Oturum açmadan çağrılabilen kayıt, davet kabulü, kurulum ve iki adımlı doğrulama istekleri de IP başına sınırlanır; davet kodu ve kurulum durumu, pahalı parola özeti hesaplanmadan önce denetlenir ve aynı anda en çok işlemci sayısı kadar özet hesaplanır.
- Hesaplar [iki adımlı doğrulama](iki-adimli-dogrulama.md) (TOTP) kullanabilir; organizasyonlar bunu üyeleri için zorunlu tutabilir. Production'da sunucu yöneticileri iki adımlı doğrulama açmadan yönetici haklarını kullanamaz (`AUTH_REQUIRE_2FA_SUPERADMIN`). Doğrulama uygulamasının anahtarı şifreli, kurtarma kodları anahtarlı özet olarak saklanır; hatalı kodlar giderek uzayan bir kilide yol açar.
- Hesabın güvenliğini etkileyen değişiklikler, son **10 dakika** içinde kimliğini yeniden doğrulamış bir tarayıcı oturumu ister: kişisel erişim anahtarı oluşturma, iki adımlı doğrulamayı açma, kimlik sağlayıcısı bağlama, parolası olmayan hesaba parola koyma ve organizasyonun iki adımlı doğrulama zorunluluğunu kaldırma. Arayüz gerektiğinde parolayı, iki adımlı doğrulama kodunu ya da bağlı kimlik sağlayıcısıyla yeniden girişi sorar. Böylece açık bırakılmış ya da çerezi çalınmış bir oturum, hesaba kalıcı bir arka kapı açamaz. Parola değişikliği şu anki parolayı ister; bu değişikliklerin hiçbiri kişisel erişim anahtarıyla yapılamaz.
- Oturum çerezi `HttpOnly`, `SameSite=Lax` ve `https` adreslerde `Secure`'dur; `https` adreslerde çerezler `__Host-` önekiyle (`__Host-izci_session`, `__Host-izci_2fa`) gönderilir, böylece aynı alan adının alt alan adlarındaki başka uygulamalar bu çerezleri koyamaz ya da ezemez. Veritabanında yalnız çerezin özeti tutulur; oturumlar **Hesabım › Oturumlar** bölümünden kapatılabilir.
- Oturum kullanılmadığında `AUTH_SESSION_TTL` sonra, her durumda girişten `AUTH_SESSION_MAX_AGE` (varsayılan 30 gün) sonra biter; düzenli kullanılan çalıntı bir çerez de süresiz geçerli kalmaz. Parola değişince, iki adımlı doğrulama açılıp kapanınca diğer oturumlar; sunucu yöneticisi iki adımlı doğrulamayı sıfırlayınca ya da hesabı devre dışı bırakınca kişinin bütün oturumları kapatılır.
- OpenID Connect akışı PKCE, `state` ve `nonce` kullanır. Sağlayıcının doğrulamadığı e-posta adresiyle hesap açılmaz ve organizasyona katılınmaz; aynı e-postalı mevcut bir hesap, sahibi oturum açmışken bağlamadıkça başka bir kimlikle girilemez (bkz. [OpenID Connect](yapilandirma.md#openid-connect)).
- İstek gövdesini çok yavaş gönderen istemciler kesilir (30 saniye artı saniyede 64 KiB'a göre gereken süre); yavaş gövdeyle bağlantı tüketme saldırıları sınırlıdır.
- Yönetim API'si tarayıcıdan yalnızca aynı kökenden çağrılabilir: her istek `Connect-Protocol-Version` başlığını taşımak zorundadır ve CORS izni verilmez. Bu, CSRF'ye karşı korur.
- Ingest ve kişisel erişim anahtarları veritabanında SHA-256 özeti olarak saklanır. Organizasyonlar kişisel anahtarlara en uzun ömür ve kullanılmama süresi sınırı koyabilir; kullanılmayan anahtarlar kendiliğinden iptal edilir, her anahtarın son kullanım zamanı ve adresi görünür (bkz. [Erişim anahtarları](cok-kiracilik.md#erişim-anahtarları)).
- Kaynak eşlemesi ve sembol yükleme, sürüm ve dosya yükleme izni olan proje anahtarı ya da `write` kapsamlı kişisel anahtar ister; tarayıcı oturumlarıyla yapılamaz. Tarayıcıya ve mobil uygulamaya konan anahtarlara bu izni vermeyin: projeyle oluşan ilk anahtar yalnız olay gönderir, yükleme izinli bir anahtar bir tarayıcıdan olay gönderdiği anda bu iznini kalıcı olarak kaybeder. Bir proje en çok 10 GiB yükleme saklayabilir.

## Şifreleme

- GitHub erişim anahtarları gibi üçüncü taraf sırları `IZCI_SECRET_KEY`'den türetilen anahtarla **AES-256-GCM** ile şifrelenir. Şifreli metin kaydın kimliğine bağlıdır; başka bir kayda kopyalanırsa çözülmez.
- Telegram düğmeleri ve GitHub App kurulum dönüşü HMAC ile imzalanır.
- Bildirim kanallarının webhook adresleri, imza anahtarları ve Jira anahtarları `IZCI_SECRET_KEY` ile şifrelenir; şifreli değer kanala bağlıdır, başka bir satıra taşınınca çözülmez. Webhook istekleri HMAC-SHA256 ile [imzalanır](bildirimler.md#imzayı-doğrulama).
- Bildirimlerde olay verisi (hata mesajı, etiketler, sürüm) biçimlendirme olarak yorumlanmaz: Slack'te kaçışlanır, Teams kartında Markdown işlenmeyen metin olarak gönderilir, Telegram'da HTML'e karşı kaçışlanır. Böylece hata mesajına yazılmış `[Hesabınız kilitlendi](https://…)` gibi bir metin, oltalama için kullanılabilecek bir bağlantıya dönüşmez.
- Slack, Teams, webhook ve Jira kanalları varsayılan olarak iç ağ adreslerine istek atamaz (bağlantı anında, DNS çözümlemesinden sonra denetlenir); böylece organizasyon yöneticileri sunucunun ağına ulaşamaz. İzin vermek için `NOTIFY_ALLOW_PRIVATE_NETWORKS=true`.
- Aktarım şifrelemesi için İzci'yi TLS ile (`IZCI_TLS_*`) ya da TLS sonlandıran bir ters vekilin arkasında çalıştırın.

### Ana anahtarı döndürme

Ana anahtar (`IZCI_SECRET_KEY`) sızdığında ya da kurum politikası gerektirdiğinde değiştirilebilir:

1. Yeni bir anahtar üretin: `openssl rand -base64 32`.
2. Bütün örneklerde `IZCI_SECRET_KEY`'e yeni anahtarı, `IZCI_SECRET_KEY_PREVIOUS`'a eskisini yazın ve örnekleri yeniden başlatın. Yeni sırlar yeni anahtarla mühürlenir; eski anahtarla mühürlenmişler açılmaya devam eder.
3. `izci secrets rotate` ile saklanan sırları (GitHub erişim anahtarları, bildirim kanalı sırları, iki adımlı doğrulama anahtarları) yeni anahtarla yeniden mühürleyin. `--dry-run` hiçbir şeyi değiştirmeden ne yapılacağını gösterir.
4. Kurtarma kodları özet olarak saklandığı için taşınamaz: rapor, kodları eski anahtara bağlı kullanıcı sayısını gösterir. Bu kullanıcılar **Hesabım › İki adımlı doğrulama › Yeni kurtarma kodları** ile yeni kod oluşturana kadar eski anahtarı listede tutun.
5. Rapor eski anahtara bağlı bir şey göstermiyorsa `IZCI_SECRET_KEY_PREVIOUS`'u kaldırıp örnekleri yeniden başlatın. Eski anahtarla imzalanmış Telegram düğmeleri bundan sonra çalışmaz; yeni bildirimlerinki çalışır.

## Kiracı yalıtımı

Organizasyonlar arası yalıtım PostgreSQL satır düzeyi güvenliğiyle sağlanır; ayrıntılar [Organizasyonlar ve roller](cok-kiracilik.md#yalıtım-nasıl-sağlanır) sayfasında.

## Yapay zekâ

- Ajan kabuk, web ve çalışma dizini dışındaki dosyalara erişemez. İzci'nin sırlarını ve GitHub anahtarını göremez.
- Olaydaki e-posta, IP, ad, çerez ve yönlendirme başlıkları model sağlayıcısına **gönderilmez**. Yığın izi, mesaj, istek yolu ve gövdesi (maskelenmiş hâliyle) gönderilir.
- Hangi model sağlayıcısının kullanıldığını ve verinin nerede işlendiğini siz seçersiniz (`AI_OPENCODE_MODEL`, `AI_OPENCODE_CONFIG`). [Kurum içi bir model](yapay-zeka.md#kurum-içi-model) de kullanılabilir.
- Analiz raporları HTML içeremez; başka sitelerdeki görseller yüklenmez, bağlantı olarak gösterilir. Böylece olay verisinden etkilenen bir rapor, açanların IP adresini dışarıya bildiremez.
- Bir organizasyonun verisi, organizasyon **Yapay zekâ ile analize izin ver** ayarını açmadıkça sağlayıcıya gitmez (varsayılan kapalı). Her analizin sağlayıcıya ne gönderdiği **Gönderilen bağlam** sekmesinde görünür; izni kimin değiştirdiği denetim kaydına yazılır.

Ayrıntılar: [GitHub ve yapay zekâ analizi](yapay-zeka.md#güvenlik).

## KVKK için kontrol listesi

- [ ] IP saklamayı ihtiyacınıza göre **anonimleştirilmiş** ya da **hiç** olarak ayarlayın.
- [ ] Uygulamalarınızın özel alan adlarını **ek olarak gizlenecek alanlara** ekleyin.
- [ ] İstek gövdelerine gerçekten ihtiyacınız yoksa saklamayı kapatın.
- [ ] Mobil uygulamalar ekran görüntüsü gönderiyorsa (`attachScreenshot`) bunun aydınlatma metninizde yer aldığından emin olun ya da **olay eklerini** kapatın.
- [ ] `RETENTION_DAYS` değerini veri envanterinizdeki süreyle eşleyin.
- [ ] Yapay zekâ kullanıyorsanız model sağlayıcısını aydınlatma metninize ekleyin ya da kurum içi model kullanın; organizasyonda izni (**Organizasyon ayarları › Genel**) bundan sonra açın.
- [ ] Veritabanı yedeklerini şifreli saklayın (bkz. [İşletim](operasyon.md)).

## Güvenlik açığı bildirimi

Bir açık bulduğunuzu düşünüyorsanız herkese açık bir kanala yazmak yerine KentOS güvenlik ekibine doğrudan bildirin.
