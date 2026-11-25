# İki adımlı doğrulama

İki adımlı doğrulama açık olan hesapta girişte parolanın ardından telefondaki doğrulama uygulamasının ürettiği altı haneli kod da istenir. Parola başkasının eline geçse bile kod olmadan hesaba girilemez. Organizasyonlar bunu üyeleri için zorunlu tutabilir.

İzci standart zamana dayalı tek kullanımlık parola (TOTP, RFC 6238) kullanır: SHA-1, 6 hane, 30 saniye. Google Authenticator, Microsoft Authenticator, 1Password, Bitwarden, Aegis, 2FAS gibi uygulamaların hepsiyle çalışır.

## Açma

1. **Hesabım › İki adımlı doğrulama** bölümünde **İki adımlı doğrulamayı aç**'a basın. Son 10 dakika içinde giriş yapmadıysanız önce parolanız (ya da bağlı kimlik sağlayıcınızla yeniden giriş) istenir.
2. Doğrulama uygulamanızla QR kodu okutun. Okutamıyorsanız altındaki anahtarı uygulamaya elle girin.
3. Uygulamanın gösterdiği 6 haneli kodu girip **Doğrula ve aç**'a basın. Kurulum bir saat içinde tamamlanmazsa yeniden başlatılır.
4. Ekrana gelen **10 kurtarma kodunu** saklayın: kopyalayın ya da `.txt` olarak indirin ve parola yöneticinize koyun. Kodlar bir daha gösterilmez.

Açtığınız anda diğer cihazlardaki oturumlarınız kapatılır; oralarda parola ve kodla yeniden girersiniz. Böylece yalnız parolayla açılmış, belki de başkasının elindeki bir oturum açık kalmaz.

## Giriş

- E-posta ve parolanın ardından kod ekranı açılır. Kodu **10 dakika** içinde girin; süre dolarsa parolayla yeniden başlarsınız.
- OpenID Connect (ör. KentOS Kimlik) ile girişte de kimlik sağlayıcısından dönünce kod sorulur.
- Her kod bir kez kabul edilir. Telefon saatindeki 30 saniyelik kaymalar tolere edilir; daha büyük farkta uygulamanın saat eşitleme ayarını kullanın.
- Arka arkaya 5 hatalı kod girilirse ikinci adım 5 dakika kilitlenir. Kilit her 5 hatada ikiye katlanır (10, 20, 40 dakika…), en çok 1 gün sürer; doğru bir kod sayacı sıfırlar. Kilitlenme parolanızın başkasında olduğunu gösterebilir: kilit açılınca parolanızı değiştirin.

## Kurtarma kodları

Telefonunuza erişemezseniz kod ekranında **Kurtarma kodu kullan**'a basıp kodlardan birini girin (`xxxxx-xxxxx`, büyük küçük harf fark etmez). Her kurtarma kodu bir kez kullanılır.

**Hesabım › İki adımlı doğrulama** kaç kod kaldığını gösterir; 3 ve altında uyarır. **Yeni kurtarma kodları** doğrulama uygulamasındaki bir kodla onaylanır, 10 yeni kod üretir ve eskileri geçersiz kılar.

## Kapatma ve telefon değiştirme

**Kapat**, doğrulama uygulamasındaki bir kodla ya da bir kurtarma koduyla onaylanır. Kurtarma kodları da silinir ve diğer cihazlardaki oturumlarınız kapatılır. İki adımlı doğrulamayı zorunlu tutan organizasyonlar, yeniden açana kadar erişiminizi keser; pencere bunları listeler.

Telefon değiştirirken eski telefon elinizdeyken kapatıp yeni telefonla yeniden açın. 1Password gibi eşitlenen uygulamalarda bu gerekmez.

## Telefonu da kodları da kaybettiyseniz

İzci hesabı kişiye aittir; organizasyon sahipleri başkasının iki adımlı doğrulamasını sıfırlayamaz. Sunucu yöneticisi, kişinin kimliğini başka yoldan doğruladıktan sonra şunlardan birini yapar:

- **Sunucu yönetimi › Kullanıcılar** bölümünde kişinin satırındaki **İki adımlı doğrulamayı sıfırla**,
- komut satırında:

  ```bash
  izci user reset-2fa ayse@kentos.com.tr
  ```

Sıfırlama kişinin bütün oturumlarını kapatır (yönetici kendi hesabını sıfırlıyorsa o anki oturumu açık kalır). Sıfırlanan hesap yalnız parolayla girer ve iki adımlı doğrulamayı yeniden açabilir. `izci user list` hangi hesaplarda açık olduğunu gösterir.

## Organizasyonda zorunlu tutma

Yöneticiler **Organizasyon ayarları › Genel** bölümünde **İki adımlı doğrulama zorunlu** seçeneğini açar. Kendi hesabında açık olmayan biri seçeneği açamaz; böylece kimse kendini dışarıda bırakmaz. Zorunluluğu kaldırmak bütün üyelerin hesaplarını zayıflattığı için, son 10 dakika içinde kimliğini doğrulamış bir oturum ister; gerekirse arayüz parolanızı ya da kodunuzu sorar.

Zorunluluk açıkken iki adımlı doğrulaması olmayan üyeler:

- organizasyonu açtığında sayfalar yerine kurulum ekranını görür; açtığı anda kaldığı yerden devam eder ya da başka organizasyonuna geçer,
- kişisel erişim anahtarlarıyla (API, komut satırı, MCP, `sentry-cli`) bu organizasyona erişemez; API `FAILED_PRECONDITION` ve `ERROR_REASON_TWO_FACTOR_REQUIRED` döner, Sentry uyumlu yükleme uçları 403 döner,
- Telegram bildirimlerindeki düğmeleri kullanamaz.

Proje anahtarları (`izk_`) bir kişiye bağlı değildir; olay gönderme ve CI'daki sürüm ve dosya yüklemeleri etkilenmez. Zorunluluk sunucu yöneticileri için de geçerlidir.

**Organizasyon ayarları › Üyeler** listesinde iki adımlı doğrulaması açık olanların adının yanında kalkan simgesi görünür; zorunluluk açıkken açmamış olanlar işaretlenir. Açma, kapatma ve kurtarma kodu yenileme, kişinin üye olduğu her organizasyonun **denetim kaydına** yazılır.

## Sunucu yöneticileri

Sunucu yöneticisi bütün organizasyonlara ve kullanıcılara erişir; parolası ele geçen bir yönetici hesabı bütün sunucuyu açar. Bu yüzden `IZCI_ENV=production` olan sunucular yöneticilerden iki adımlı doğrulama ister (`AUTH_REQUIRE_2FA_SUPERADMIN`, bkz. [Yapılandırma](yapilandirma.md#oturum-açma)).

Açmamış bir yönetici:

- hesabına girer, üyesi olduğu organizasyonları her zamanki gibi kullanır,
- **Sunucu yönetimi** sayfası yerine iki adımlı doğrulamayı açma çağrısını görür; API'de `AdminService` çağrıları `FAILED_PRECONDITION` ve `ERROR_REASON_TWO_FACTOR_REQUIRED` döner,
- üyesi olmadığı organizasyonlara yönetici olarak (API, `sentry-cli`) erişemez ve açık kayıt kapalıyken yeni organizasyon oluşturamaz.

Açtığı anda yönetici hakları geri gelir. Komut satırı (`izci user …`, `izci token create`) sunucuda çalıştığı için bu kuraldan etkilenmez; yöneticinin telefonu kaybolursa başka bir yönetici ya da `izci user reset-2fa` sıfırlar. `AuthService.GetSession` bu durumu `superadmin_needs_two_factor` alanıyla bildirir.

## API ile giriş

Kendi istemcinizi yazıyorsanız:

1. `AuthService.Login` iki adımlı doğrulaması açık hesaplarda oturum yerine `two_factor_required: true` döner ve 10 dakikalık `izci_2fa` çerezini (HttpOnly; https'de `__Host-izci_2fa`) koyar.
2. `AuthService.VerifyTwoFactor` aynı çerezle ve `code` alanıyla (6 hane ya da kurtarma kodu) çağrılır; başarıda oturum çerezi konur ve oturum döner.

Hatalı kod `ERROR_REASON_INVALID_TWO_FACTOR_CODE`, süresi dolan giriş `ERROR_REASON_TWO_FACTOR_EXPIRED`, kilit `ERROR_REASON_TOO_MANY_ATTEMPTS` (`retry_after_seconds` ile) döner. Kurulum ve kapatma (`UserService.BeginTwoFactorSetup`, `EnableTwoFactor`, `DisableTwoFactor`, `RegenerateRecoveryCodes`) yalnız tarayıcı oturumuyla yapılır, kişisel erişim anahtarıyla yapılamaz.

Kurulumu başlatmak, erişim anahtarı oluşturmak ve zorunluluğu kaldırmak gibi hassas istekler, oturum son 10 dakika içinde kimliğini doğrulamadıysa `FAILED_PRECONDITION` ve `ERROR_REASON_REAUTHENTICATION_REQUIRED` döner. `AuthService.Reauthenticate` aynı oturumla `password` ya da `code` (6 hane ya da kurtarma kodu) alanıyla çağrılır; başarıda `reauthenticated_at` döner ve istek yeniden denenir. Yanlış parola `ERROR_REASON_INVALID_CREDENTIALS` döner (oturum geçerli kalır). Yalnız kimlik sağlayıcısıyla giren hesaplar tarayıcıyı `/auth/oidc/<id>/start?reauth=1&next=<sayfa>` adresine yönlendirir; dönüşte sayfanın adresine `oidc_reauth=1` eklenir. `AuthService.GetSession` son doğrulamanın zamanını `reauthenticated_at` alanında verir. Ayrıntılar [Yönetim API başvurusunda](api/izci.api.v1.md).

## Nasıl saklanır

- Doğrulama uygulamasının anahtarı `IZCI_SECRET_KEY`'den türetilen anahtarla **AES-256-GCM** ile şifrelenir ve kullanıcıya bağlanır.
- Kurtarma kodları yalnız HMAC özeti olarak saklanır; özet anahtarı da `IZCI_SECRET_KEY`'den türetilir. Veritabanının bir kopyası kodları tahmin etmeye yetmez.
- Girişin ikinci adımını bekleyen çerezin veritabanında yalnız özeti tutulur; süresi dolanlar saklama işinde silinir.

**`IZCI_SECRET_KEY` değiştirilecekse** eskisini `IZCI_SECRET_KEY_PREVIOUS`'a yazıp [anahtarı döndürün](guvenlik.md#ana-anahtarı-döndürme): iki adımlı doğrulama anahtarları yeni anahtara taşınır, kurtarma kodları eski anahtar listede kaldıkça çalışır. Eskisi olmadan değiştirilirse bu anahtarlar çözülemez; hesaplar giriş yapamaz ve sunucu günlüğünde bunu söyleyen bir hata görünür. Bu durumda hesapları `izci user reset-2fa` ile sıfırlayın. Geliştirmede de sabit bir anahtar kullanın: anahtar boşken her açılışta yenisi üretildiğinden yeniden başlatmadan sonra iki adımlı doğrulama çalışmaz.
