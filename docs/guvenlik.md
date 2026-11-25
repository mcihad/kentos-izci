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
- Organizasyonlar (**Organizasyon ayarları › Genel › Saklama süresi**) ve projeler (**Proje ayarları › Gizlilik › Saklama süresi**) daha kısa bir süre seçebilir; sunucu yöneticisinin plan sınırı (`retention_days`) da süreyi kısaltır. Süresi dolan olaylar, ekleri, geri bildirimler, sayaçlar ve bu süreden beri görülmeyen hatalar altı saatte bir çalışan saklama işiyle silinir. Süreyi kısaltmak eski veriyi sildiği için kimliğin yeniden doğrulanmasını ister.
- Yöneticiler tek bir olayı (olay sayfasında **Olayı sil**) ya da bir hatayı bütün olaylarıyla (**Sil**) silebilir; hatanın olayları arka planda temizlenir. Silinen bir projenin, organizasyonun bütün verisi de arka planda temizlenir; organizasyon hemen kaybolur ve kısa adı boşa çıkar.
- Bir kişi hesabını **Hesabım › Profil › Hesabı sil** ile silebilir (sunucu yöneticisi de **Sunucu yönetimi › Kullanıcılar** bölümünden). Üyelikler, oturumlar, erişim anahtarları ve kişisel ayarlar silinir; yorumlar ve denetim kayıtları yazarsız kalır. Tek sahibi olunan organizasyon ya da sunucunun son yöneticisi varken hesap silinemez.
- [Kaynak eşlemeleri ve sembol dosyaları](kaynak-eslemeleri.md) yığın izi çözdükçe tutulur; 90 gün (ya da daha uzunsa `RETENTION_DAYS`) kullanılmayanlar silinir. Kaynak eşlemeleri çoğu zaman uygulamanın kaynak kodunu içerir; organizasyon üyeleri hata sayfasında bu kodun satırlarını görür.
- Son olayı bu süreden eski olan hatalar da; saatlik sayaçları, etiket dağılımları ve etkilenen kullanıcı kayıtlarıyla birlikte silinir. Organizasyonların kullanım istatistikleri (günlük olay sayıları) 400 gün tutulur.

## Kimlik doğrulama

- İlk kurulum sihirbazı, sunucunun günlüğüne yazdığı kurulum kodunu ister; internete açılan yeni bir sunucuyu ilk ulaşan kişi ele geçiremez. Kurulum bitmeden açık kayıtla ya da kimlik sağlayıcısıyla da hesap açılmaz.
- Açık kayıtta (`AUTH_SIGNUP=open`) hesap, e-posta adresine gönderilen tek kullanımlık bağlantı açılınca oluşur: kimse başkasının adresiyle önceden hesap açıp, adresin sahibi sonra kimlik sağlayıcısıyla girdiğinde o hesabı paylaşamaz. Kayıt e-postası kayıt olanın yazdığı hiçbir metni (ad, organizasyon adı) içermez; bir adrese 15 dakikada en çok 3 bağlantı gönderilir. Bir kullanıcı en çok `AUTH_SIGNUP_ORG_LIMIT` organizasyonun sahibi olabilir.
- Parolalar **argon2id** ile özetlenir; en az 10 karakter olmalıdır. Başarısız girişler 15 dakikalık pencerelerde sayılır: aynı istemciden bir hesaba 10, aynı istemciden bütün hesaplara 30, her yerden bir hesaba 100 hatalı deneme. Böylece bir saldırgan, hesabın sahibini başka bir adresten kilitleyemez. IPv6 istemcileri /64 ağlarına göre gruplanır; sayaçlar veritabanında tutulur, bütün sunucu örnekleri ortak görür. Oturum açmadan çağrılabilen kayıt, davet kabulü, kurulum ve iki adımlı doğrulama istekleri de IP başına sınırlanır; davet kodu ve kurulum durumu, pahalı parola özeti hesaplanmadan önce denetlenir ve aynı anda en çok işlemci sayısı kadar özet hesaplanır.
- Parolasını unutan, giriş sayfasındaki **Parolamı unuttum** ile e-posta adresine tek kullanımlık bir bağlantı ister (sunucuda SMTP ayarlıysa). Bağlantı 1 saat geçerlidir; biri kullanılınca hesabın bütün sıfırlama bağlantıları geçersizleşir. Yanıt, adresin hesabı olsa da olmasa da aynıdır; böylece hangi adreslerin hesabı olduğu öğrenilemez. Bir adrese 15 dakikada en çok 3 bağlantı gönderilir. Yeni parola konunca hesabın bütün oturumları kapanır; iki adımlı doğrulama açıksa girişte kod yine istenir. E-posta göndermeyen sunucularda sunucu yöneticisi `izci user reset-password EMAIL` ile yeni parola koyar.
- Hesabın güvenliğini etkileyen olaylar sahibine e-postayla bildirilir (sunucuda SMTP ayarlıysa): daha önce kullanılmamış bir cihazdan (tarayıcı ve işletim sistemi ailesi) giriş, parolanın değişmesi ya da sıfırlanması, iki adımlı doğrulamanın kapatılması (kişinin kendisi ya da sunucu yöneticisi tarafından) ve yeni kişisel erişim anahtarı. Böylece hesabı ele geçiren biri bunu sahibinden gizleyemez. Hesabın ilk girişi bildirilmez; bir yıl kullanılmayan cihazlar unutulur.
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

## Denetim kaydı

Güvenliği etkileyen her işlem kimin, ne zaman, hangi adresten yaptığıyla kaydedilir:

- **Organizasyonun kaydı** (**Organizasyon ayarları › Denetim kaydı**, yöneticiler): anahtarlar, üyeler, roller, kanallar, kurallar, projeler, entegrasyonlar, ayarlar, birleştirme ve silmeler, kişisel veri talepleri; üyelerin hesap güvenliği değişiklikleri (parola değişikliği ve sıfırlaması, iki adımlı doğrulama, erişim anahtarları, hesap silme); sunucu yöneticisinin **üyesi olmadığı** organizasyona girmesi (aynı kişi için saatte bir kayıt).
- **Sunucunun güvenlik kaydı** (**Sunucu yönetimi › Güvenlik kaydı**, sunucu yöneticileri): girişler (yöntem ve cihazla), başarısız girişler ve nedenleri (hatalı parola, devre dışı hesap, çok fazla deneme, hatalı doğrulama kodu), çıkışlar, parola sıfırlama istekleri, hesap güvenliği değişiklikleri, kullanıcı yönetimi ve komut satırı işlemleri. `izci user create`, `reset-password`, `reset-2fa`, `superadmin` ve `izci secrets rotate` kayda komutu çalıştıran işletim sistemi kullanıcısı ve makine adıyla yazılır; hesap güvenliğini değiştirenler kişinin organizasyonlarının kaydına da düşer.

Girişler ve başarısız girişler iki yıl (ya da daha uzunsa `RETENTION_DAYS`) tutulur; diğer kayıtlar organizasyonla birlikte silinene kadar kalır.

### SIEM aktarımı

`AUDIT_WEBHOOK_URL` ve `AUDIT_SYSLOG_ADDR` (bkz. [Yapılandırma](yapilandirma.md#denetim-kaydı-aktarımı)) verilince bütün kayıtlar (organizasyonların ve sunucunun) sırayla ve en az bir kez aktarılır. Aktarma, ayar ilk verildiği andan sonraki kayıtlarla başlar; kayıtlar 30 saniye geriden okunur, böylece geç tamamlanan bir işlemin kaydı atlanmaz. Teslim edilemeyen kayıtlar bir sonraki denemede yeniden gönderilir; birden çok sunucu örneğinde aktarmayı biri yapar.

Webhook her çağrıda `{"entries": [...]}` gövdesini `POST` eder. `AUDIT_WEBHOOK_SECRET` verildiyse `X-Izci-Signature` başlığı `sha256=` ile gövdenin HMAC-SHA256 özetini taşır; alıcı aynı özeti hesaplayıp karşılaştırmalıdır:

```python
import hashlib, hmac
expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, request.headers["X-Izci-Signature"])
```

Syslog'a her kayıt bir RFC 5424 iletisi olarak gider: tesis `log audit` (13), önem başarısız girişlerde `warning`, diğerlerinde `notice`; `APP-NAME` `izci`, `MSGID` işlemin adı (`auth.login_failed`), ileti gövdesi kaydın JSON hâli. TCP ve TLS'te iletiler uzunluk önekiyle (RFC 6587) çerçevelenir.

Bir kaydın JSON hâli:

```json
{"id": "019…", "time": "2026-11-25T08:15:02.41Z", "organization": {"id": "…", "slug": "kentos"},
 "actor": {"id": "…", "email": "ayse@kentos.com.tr", "name": "Ayşe"}, "action": "member.update",
 "target": {"type": "user", "id": "…", "name": "mehmet@kentos.com.tr"}, "metadata": {"role": "ROLE_ADMIN"}, "ip": "10.1.2.3"}
```

## Kişisel veri talepleri

KVKK'nın 11. maddesi (GDPR'da erişim ve silme hakları) kapsamındaki talepler **Organizasyon ayarları › Kişisel veriler** bölümünden karşılanır (yalnız yöneticiler):

1. Kişiyi tanıtan bilgileri yazın: e-posta, uygulamanın gönderdiği kullanıcı kimliği (`user.id`), kullanıcı adı, IP adresi. Bunlardan herhangi biri geçen olaylar kişiye sayılır; e-posta büyük/küçük harf ayırmadan karşılaştırılır. Müşteri (ör. sözleşmesi biten bir belediye) için müşteri anahtarını yazın. İsterseniz tek bir projeyle sınırlayın.
2. **Verileri bul** olay, hata, ek ve geri bildirim sayılarını ve son olayları gösterir.
3. **JSON olarak dışa aktar** bütün olayları (saklanan hâliyle, eklerin ad ve boyutlarıyla) ve geri bildirimleri tek bir dosyaya indirir. Ek içerikleri dosyaya konmaz; gerekiyorsa olay sayfasından indirilir.
4. **Verileri sil** olayları, eklerini, kişinin geri bildirimlerini ve hatalardaki etkilenen kullanıcı ve etiket kayıtlarını siler; müşteri için müşteri kaydını ve sayaçlarını da. Hatalar ve olay sayaçları kalır.

Dışa aktarmalar ve silmeler denetim kaydına yazılır; kayıtta kimlik bilgileri maskelenir (`ay***@example.com`). Bir IP adresi birden çok kişiye ait olabilir, yalnız gerekiyorsa kullanın. Hata başlıkları, yorumlar ve yapay zekâ analizleri taranmaz: kişisel veri içerebilecek hataları ayrıca silin. Sürüm sağlığı kayıtlarındaki kullanıcılar yalnız anahtarlı özet olarak tutulur ve saklama süresiyle silinir.

Aynı işlemler [Yönetim API'sinde](api/izci.api.v1.md) `PrivacyService` ile yapılabilir. Bir kişinin hangi hatalarda geçtiği, kimliklerinin özetleriyle (`issue_subjects`) tutulur; böylece talepler olay tablosunun büyüklüğünden bağımsız olarak dizinden yanıtlanır.

## KVKK için kontrol listesi

- [ ] IP saklamayı ihtiyacınıza göre **anonimleştirilmiş** ya da **hiç** olarak ayarlayın.
- [ ] Uygulamalarınızın özel alan adlarını **ek olarak gizlenecek alanlara** ekleyin.
- [ ] İstek gövdelerine gerçekten ihtiyacınız yoksa saklamayı kapatın.
- [ ] Mobil uygulamalar ekran görüntüsü gönderiyorsa (`attachScreenshot`) bunun aydınlatma metninizde yer aldığından emin olun ya da **olay eklerini** kapatın.
- [ ] `RETENTION_DAYS` değerini ve organizasyon ya da proje saklama sürelerini veri envanterinizdeki süreyle eşleyin.
- [ ] Kişisel veri taleplerini kimin karşılayacağını belirleyin; o kişiler organizasyonda yönetici olmalı.
- [ ] Yapay zekâ kullanıyorsanız model sağlayıcısını aydınlatma metninize ekleyin ya da kurum içi model kullanın; organizasyonda izni (**Organizasyon ayarları › Genel**) bundan sonra açın.
- [ ] Veritabanı yedeklerini şifreli saklayın (bkz. [İşletim](operasyon.md)).

## Bağımlılıklar

İzci'nin bağımlılıkları sürekli taranır:

- Her değişiklikte ve haftada bir CI, Go kodunu `govulncheck` ile (yalnız izci'nin gerçekten çağırdığı koddaki açıklar), arayüzün üretim bağımlılıklarını `pnpm audit` ile ve konteyner imajını Trivy ile tarar; yüksek ya da kritik, düzeltmesi yayımlanmış bir açık derlemeyi durdurur. İmajın yazılım malzeme listesi (SBOM, SPDX) iş akışının çıktısı olarak saklanır.
- Dependabot Go modüllerini, web paketlerini, GitHub Actions'ı ve Docker taban imajını haftalık günceller; güvenlik güncellemeleri için depo ayarlarından **Dependabot security updates** açılmalıdır.
- Yerelde `make audit` aynı bağımlılık taramalarını çalıştırır.
- Dışarıdan gelen veriyi ayrıştıran kod fuzz testleriyle sınanır: Sentry zarfları ve olayları, kaynak eşlemeleri, ELF, Mach-O ve DWARF dosyaları, Dart karıştırma haritaları, arama dili (ve oluşturduğu SQL'e kullanıcı metninin girmediği), sahiplik kuralları ve CODEOWNERS. CI haftada bir her birini iki dakika çalıştırır; yerelde `make fuzz`. Bulunan hatalı girdiler `testdata/fuzz` altında saklanır ve her `go test` ile yeniden denenir.

## Güvenlik açığı bildirimi

Bir açık bulduğunuzu düşünüyorsanız herkese açık bir kanala yazmak yerine KentOS güvenlik ekibine doğrudan bildirin.
