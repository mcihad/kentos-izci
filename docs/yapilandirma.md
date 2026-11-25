# Yapılandırma

İzci bütün ayarlarını ortam değişkenlerinden okur. Geliştirmede aynı dizindeki `.env` dosyası da okunur (`--env-file` ya da `IZCI_ENV_FILE` ile başka bir dosya gösterilebilir). **Gerçek ortam değişkenleri her zaman `.env` dosyasındaki değerlerin önüne geçer.** Production dağıtımları (Docker, Dokploy) değişkenleri doğrudan verir ve `.env` dosyası taşımaz.

Hatalı bir değer sunucunun açılmasını engeller; hata mesajı hangi değişkenin neden reddedildiğini söyler. Sır içeren değerler (`IZCI_SECRET_KEY`, `*_TOKEN`, `*_SECRET`, `*_PRIVATE_KEY`) loglara hiçbir zaman yazılmaz.

## Çekirdek

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `IZCI_ENV` | `development` | `development`, `production` ya da `test`. Production'da `IZCI_SECRET_KEY` zorunludur, loglar JSON olur ve süper kullanıcı veritabanı rolü reddedilir. |
| `IZCI_HTTP_ADDR` | `:5300` | Dinlenen adres. Tek port şunları taşır: gRPC (h2c ya da TLS), gRPC-Web, Connect, Sentry uç noktaları, yönetim API'si, MCP ve web arayüzü. |
| `IZCI_PUBLIC_URL` | `http://localhost:5300` | Dışarıdan erişilen taban adres. Bildirim ve çekme isteği bağlantılarında, OIDC dönüş adreslerinde, Sentry DSN'lerinde ve `llms.txt` içinde kullanılır. Mutlak bir `http(s)` adresi olmalıdır. |
| `IZCI_SETUP_TOKEN` | (boş) | Kurulum sihirbazının istediği kod. Boşsa `IZCI_SECRET_KEY`'den türetilir ve kullanıcı yokken günlüğe yazılır (`izci setup --print-code`). |
| `IZCI_SECRET_KEY` | (boş) | 32 baytlık ana anahtar, base64 ya da hex. Saklanan erişim anahtarlarını ve iki adımlı doğrulama anahtarlarını AES-256-GCM ile şifreler, imzalar üretir; değiştirilirse bunlar çözülemez. Üretmek için: `openssl rand -base64 32`. Production'da zorunlu; geliştirmede boşsa her açılışta geçici bir anahtar üretilir. |
| `IZCI_DATA_DIR` | `./data` | Yapay zekâ çalışma alanı: depo aynaları (`ai/repos`), koşu dizinleri (`ai/work`) ve opencode'un yalıtılmış ayarları (`ai/opencode`). |
| `IZCI_TLS_CERT_FILE`, `IZCI_TLS_KEY_FILE` | (boş) | İzci TLS'i kendisi sonlandıracaksa sertifika ve anahtar dosyaları; ikisi birlikte verilmelidir. Verilmezse port HTTP/1.1 ve şifresiz HTTP/2 (h2c) konuşur. |
| `IZCI_TRUSTED_PROXIES` | (boş) | `X-Forwarded-For` ve `X-Real-IP` başlıklarına güvenilecek vekiller: virgülle ayrılmış IP ya da CIDR (ör. `10.0.0.0/8,172.16.0.0/12`). Olayların istemci IP'si ve denetim kaydı bu listeye göre belirlenir. |
| `IZCI_SHUTDOWN_TIMEOUT` | `20s` | Kapanışta istekleri ve ingest kuyruğunu boşaltmak için süre. |

## Loglama

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `IZCI_LOG_LEVEL` | `info` | `debug`, `info`, `warn` ya da `error`. |
| `IZCI_LOG_FORMAT` | ortama göre | `json` ya da `text`. Boşsa development'ta `text`, production'da `json`. Loglar her zaman stderr'e yazılır. |

## Veritabanı

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `DATABASE_URL` | (boş) | PostgreSQL 18+ bağlantısı. Rol, tabloların sahibi olmalı ve süper kullanıcı olmamalıdır (bkz. [Kurulum](kurulum.md#2-veritabanını-hazırlayın)). |
| `DB_MAX_CONNS` | `20` | Bağlantı havuzu üst sınırı; en az 2. |
| `DB_AUTO_MIGRATE` | `true` | `izci serve` açılırken bekleyen göçleri uygular. Kapalıysa `izci migrate up` elle çalıştırılır. |
| `DB_ADMIN_URL` | (boş) | Yalnız `izci db bootstrap` ve `izci db reset` için süper kullanıcı bağlantısı. Sunucu bunu kullanmaz. |
| `IZCI_TEST_DATABASE_URL` | (boş) | Entegrasyon testlerinin veritabanı (`make test`); `db bootstrap` bunu da oluşturur. |
| `DB_ALLOW_PRIVILEGED_ROLE` | `false` | Production'da süper kullanıcı ya da `BYPASSRLS` rolüne izin verir. **Kiracı yalıtımını kapatır**; yalnız bilinçli olarak kullanın. |

## Saklama süresi

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `RETENTION_DAYS` | `90` | Olayların en uzun saklanma süresi (1–3650 gün). Olaylar günlük bölümlerde tutulur; süresi dolan bölüm bütünüyle silinir. Süre bütün organizasyonlar için ortaktır. |
| `PARTITION_PREMAKE_DAYS` | `7` | Önceden oluşturulan günlük bölüm sayısı (1–60). |

## Ingest

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `INGEST_SHARDS` | `4` | Paralel toplu yazıcı sayısı (1–64). Bir projenin olayları hep aynı yazıcıya gider. |
| `INGEST_QUEUE_SIZE` | `5000` | Yazıcı başına kuyruk. Dolarsa istemci `UNAVAILABLE` alır ve yeniden dener. |
| `INGEST_BATCH_SIZE` | `500` | Tek veritabanı işlemindeki en çok olay (1–5000). |
| `INGEST_ATTACHMENT_MAX_BYTES` | `10485760` | Olayla gelen bir ekin (ekran görüntüsü, günlük dosyası) en büyük boyutu, bayt (en çok 40 MiB). Büyük ekler atılır. |
| `INGEST_SYMBOL_CACHE_MB` | `256` | Yığın izlerini çözmek için bellekte tutulan [kaynak eşlemeleri ve sembol dosyalarının](kaynak-eslemeleri.md) en çok boyutu, MiB (16–16384). |
| `INGEST_LINGER` | `0s` | Toplu yazmadan önce daha fazla olay için bekleme. `0s` doğal toplama demektir: yazıcı meşgulken biriken olaylar bir sonraki işlemde yazılır. |
| `INGEST_ACK` | `durable` | `durable`: olay veritabanına yazılınca yanıt verilir. `async`: kuyruğa alınınca yanıt verilir (daha hızlı, kapanışta kayıp riski). |
| `INGEST_ACK_TIMEOUT` | `15s` | `durable` modunda yazmayı bekleme süresi. |
| `INGEST_RATE_LIMIT_PER_MINUTE` | `6000` | Proje başına dakikada olay. Proje ayarları ve anahtarlar kendi sınırını koyabilir. |
| `INGEST_DEDUPE_SIZE` | `200000` | Yeniden denemelerde tekrar kaydı önlemek için hatırlanan olay kimliği sayısı. |

## Oturum açma

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `AUTH_SIGNUP` | `invite` | Kim hesap açabilir: `invite` (yalnız davetle), `open` (herkes; kendi organizasyonunu oluşturur) ya da `closed`. Açık kayıtta hesap, e-posta adresine gönderilen bağlantı açılınca oluşur; production'da `SMTP_HOST` gerekir, geliştirmede bağlantı sunucu günlüğüne yazılır. |
| `AUTH_SIGNUP_ORG_LIMIT` | `3` | Açık kayıtta bir kullanıcının sahibi olabileceği en çok organizasyon sayısı (kayıtta açılan dahil). Sunucu yöneticileri sınırsızdır; `0` sınırı kaldırır. |
| `AUTH_SESSION_TTL` | `720h` | Kullanılmayan tarayıcı oturumunun ömrü (boşta kalma süresi); her kullanım uzatır. Kısa tutmak (ör. `8h`) ortak bilgisayarlarda açık kalan oturumların riskini azaltır. |
| `AUTH_SESSION_MAX_AGE` | `720h` | Oturumun girişten itibaren en uzun ömrü. Ne kadar kullanılırsa kullanılsın bu süre dolunca yeniden giriş istenir; çalınan bir oturum çerezi süresiz kullanılamaz. |
| `AUTH_PASSWORD_LOGIN` | `true` | E-posta ve parolayla giriş. Kapatılırsa yalnız OpenID Connect ile girilir. |
| `AUTH_INVITATION_TTL` | `168h` | Davet bağlantısının geçerlilik süresi. |
| `AUTH_REQUIRE_2FA_SUPERADMIN` | `auto` | Sunucu yöneticilerinden [iki adımlı doğrulama](iki-adimli-dogrulama.md#sunucu-yöneticileri) ister: `auto` (yalnız `IZCI_ENV=production` iken), `true` ya da `false`. Açmamış bir yönetici hesabını ve üyeliklerini kullanır, sunucu yönetimini kullanamaz. |

### OpenID Connect

`OIDC_PROVIDERS` virgülle ayrılmış sağlayıcı kimlikleri listesidir. Her kimlik için `OIDC_<KİMLİK>_*` değişkenleri tanımlanır. Sağlayıcıda kayıtlı olması gereken dönüş adresi: `{IZCI_PUBLIC_URL}/auth/oidc/<kimlik>/callback`.

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `OIDC_<ID>_NAME` | sağlayıcı kimliği | Giriş düğmesinde görünen ad, ör. `KentOS Kimlik`. |
| `OIDC_<ID>_ISSUER` | — | Zorunlu. Keşif belgesinin (`/.well-known/openid-configuration`) bulunduğu issuer adresi. |
| `OIDC_<ID>_CLIENT_ID` | — | Zorunlu. |
| `OIDC_<ID>_CLIENT_SECRET` | (boş) | Gizli istemciler için. PKCE her durumda kullanılır. |
| `OIDC_<ID>_SCOPES` | `openid,profile,email` | İstenen kapsamlar. |
| `OIDC_<ID>_ICON` | `generic` | `kimlik`, `google`, `microsoft`, `keycloak`, `github` ya da `generic`. |
| `OIDC_<ID>_AUTO_CREATE` | `true` | İlk girişte hesap oluşturur. `false` ise yalnız önceden davet edilmiş ya da oluşturulmuş kullanıcılar girer. |
| `OIDC_<ID>_ALLOWED_DOMAINS` | (boş) | Yalnız bu e-posta alan adlarına izin verir, ör. `kentos.com.tr`. Boşsa hepsi. |
| `OIDC_<ID>_DEFAULT_ORG` | (boş) | İlk girişte katılınacak organizasyon (kısa ad). |
| `OIDC_<ID>_DEFAULT_ROLE` | `member` | O organizasyondaki rol: `viewer`, `member`, `admin` ya da `owner`. |
| `OIDC_<ID>_TRUST_EMAIL` | `false` | Sağlayıcının e-posta adreslerine güvenir: `email_verified` göndermeyen bir adres doğrulanmış sayılır ve ilk giriş, aynı e-postalı mevcut İzci hesabına kendiliğinden bağlanır. Yalnız kullanıcıların adreslerini kendilerinin değiştiremediği kurum dizinleri (KentOS.Kimlik, şirket Keycloak'u) için açın; Google gibi herkese açık ya da çok kiracılı (Entra ID) sağlayıcılarda kapalı tutun. |

**E-posta adresleri ve hesap bağlama.** Bir kimlik ilk kez giriş yaptığında İzci, sağlayıcının e-postayı doğruladığını (`email_verified=true`) görmeden hesap açmaz, organizasyona katmaz ve `ALLOWED_DOMAINS` denetimini geçmiş saymaz. Aynı e-postalı bir İzci hesabı zaten varsa (ör. davetle parolayla açılmış), kimlik o hesaba kendiliğinden bağlanmaz: kullanıcı önce hesabına girer, **Hesabım › Profil › Giriş yöntemleri** bölümünden sağlayıcıyı bağlar (son 10 dakika içinde giriş yapmadıysa önce kimliğini yeniden doğrular). Böylece e-posta adresini başka birinin adresiyle değiştirebilen bir sağlayıcı hesabı (ör. Entra ID'deki "nOAuth" sorunu) bir İzci hesabını ele geçiremez. `TRUST_EMAIL=true` bu iki kuralı o sağlayıcı için gevşetir.

Örnek (KentOS.Kimlik):

```bash
OIDC_PROVIDERS=kimlik
OIDC_KIMLIK_NAME="KentOS Kimlik"
OIDC_KIMLIK_ISSUER=https://kimlik.kentos.com.tr/
OIDC_KIMLIK_CLIENT_ID=izci
OIDC_KIMLIK_CLIENT_SECRET=…
OIDC_KIMLIK_ALLOWED_DOMAINS=kentos.com.tr
OIDC_KIMLIK_DEFAULT_ORG=kentos
OIDC_KIMLIK_TRUST_EMAIL=true   # kurum dizini: adresleri yalnız yöneticiler değiştirir
```

## Telegram

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | (boş) | @BotFather'dan alınan bot anahtarı. Boşsa Telegram bildirimleri kapalıdır. |
| `TELEGRAM_MODE` | `polling` | `polling`: İzci güncellemeleri kendisi çeker, genel adres gerekmez. `webhook`: Telegram `{IZCI_PUBLIC_URL}/telegram/webhook` adresini çağırır; `https` gerekir. |
| `TELEGRAM_WEBHOOK_SECRET` | (boş) | Yalnız webhook modunda: 16–256 karakter, `A-Z a-z 0-9 _ -`. Üretmek için: `openssl rand -hex 24`. |
| `TELEGRAM_API_URL` | `https://api.telegram.org` | Kendi Bot API sunucunuzu kullanıyorsanız değiştirin. |

Ayrıntılar: [Telegram bildirimleri](telegram.md).

## E-posta ve diğer bildirim kanalları

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `SMTP_HOST` | (boş) | E-posta kanallarının ve açık kayıt bağlantılarının kullandığı SMTP sunucusu. Boşsa e-posta kanalı eklenemez ve production'da açık kayıt kullanılamaz. |
| `SMTP_PORT` | `587` | STARTTLS için 587, doğrudan TLS için 465. |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | (boş) | Verilirse PLAIN ile kimlik doğrulanır (yalnız şifreli bağlantıda). |
| `SMTP_FROM` | (boş) | Gönderen adres, ör. `İzci <izci@example.com>`. `SMTP_HOST` verilince zorunludur. |
| `SMTP_TLS` | `starttls` | `starttls`, `tls` (465 portu) ya da `none` (yalnız aynı makinedeki ya da ağdaki aktarıcılar için). |
| `NOTIFY_ALLOW_PRIVATE_NETWORKS` | `false` | Slack, Teams, webhook ve Jira kanallarının özel ağdaki (10.x, 192.168.x, localhost…) adreslere istek atmasına izin verir; ör. şirket içi Jira. Kapalıyken organizasyon yöneticileri sunucunun iç ağına erişemez. HTTP vekili (`HTTPS_PROXY`) özel ağdaysa açın. |

Ayrıntılar: [Bildirim kanalları](bildirimler.md).

## GitHub

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `GITHUB_API_URL` | `https://api.github.com` | GitHub Enterprise Server için `https://github.example.com/api/v3`. |
| `GITHUB_WEB_URL` | `https://github.com` | Bağlantılar ve git adresleri için web tabanı. |
| `GITHUB_APP_ID` | (boş) | İsteğe bağlı GitHub App. Aşağıdaki beş değer birlikte verilmelidir. |
| `GITHUB_APP_SLUG` | (boş) | App'in kısa adı (kurulum adresinde görünür). |
| `GITHUB_APP_CLIENT_ID`, `GITHUB_APP_CLIENT_SECRET` | (boş) | App'in OAuth bilgileri. İzci, App'i kuran kişinin kuruluma gerçekten erişebildiğini bunlarla doğrular. |
| `GITHUB_APP_PRIVATE_KEY` | (boş) | App'in PEM özel anahtarı ya da… |
| `GITHUB_APP_PRIVATE_KEY_FILE` | (boş) | …anahtar dosyasının yolu. |

Organizasyonlar App olmadan da ince ayarlı erişim anahtarıyla bağlanabilir. Ayrıntılar: [GitHub ve yapay zekâ analizi](yapay-zeka.md).

## Yapay zekâ analizi

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `AI_ENABLED` | `false` | Yapay zekâ analizini açar. |
| `AI_OPENCODE_BIN` | `opencode` | opencode çalıştırılabilir dosyası. |
| `AI_OPENCODE_MODEL` | (boş) | `sağlayıcı/model` biçiminde varsayılan model, ör. `anthropic/claude-sonnet-5`. Boşsa opencode'un varsayılanı. Projeler kendi modelini seçebilir. |
| `AI_OPENCODE_CONFIG` | (boş) | Koşulara uygulanacak isteğe bağlı opencode ayar dosyası (ör. özel sağlayıcılar). İzci'nin izinleri her zaman önceliklidir. |
| `AI_MAX_CONCURRENT` | `2` | Bu sunucuda aynı anda çalışan en çok analiz (1–32). |
| `AI_TIMEOUT` | `20m` | Bir analizin en uzun süresi (en az `1m`). |
| `AI_OUTPUT_LANGUAGE` | `Turkish` | İnsanlar için yazılan raporun dili. Kod ve kod yorumları her zaman İngilizcedir. |
| `AI_GIT_AUTHOR_NAME`, `AI_GIT_AUTHOR_EMAIL` | `izci-bot`, `izci-bot@users.noreply.github.com` | Düzeltme commit'lerinin yazarı. |
| `AI_ALLOWED_COMMANDS` | (boş) | Projelerin ajana çalıştırtabileceği kabuk komutları, virgülle: ör. `dotnet test,go test`. Proje ayarındaki bir komut bunlardan birine eşit olmalı ya da ona argüman eklemeli (`go test ./internal/...`). Boşsa ajan hiçbir komut çalıştırmaz. Test ve derleme komutları, ajanın değiştirebildiği kodu çalıştırır: yalnız `AI_SANDBOX=bwrap` ile ya da analizlere ayrılmış bir sunucuda izin verin. |
| `AI_SANDBOX` | `none` | `bwrap` (yalnız Linux, [bubblewrap](https://github.com/containers/bubblewrap)): ajan kendi süreç ad alanında, sistem klasörleri salt okunur, yalnız koşunun klasörünü ve deposunu görerek çalışır; başka projelerin depoları, İzci'nin verisi, ayarları ve süreci görünmez. Ağ açık kalır (model sağlayıcısı için). `izci ai doctor` yalıtımın çalıştığını denetler. |

Kurulumu `izci ai doctor` denetler; bu komut modele istek göndermez.

## MCP

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `MCP_ENABLED` | `true` | `{IZCI_PUBLIC_URL}/mcp` adresinde MCP sunar (kişisel erişim anahtarıyla). |

Ayrıntılar: [MCP ile yapay zekâ asistanları](mcp.md).

## Metrikler

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `METRICS_ENABLED` | `true` | `/metrics` adresinde Prometheus metrikleri sunar. |
| `METRICS_TOKEN` | (boş) | Verilirse her istek `Authorization: Bearer <token>` taşımalıdır. Boşsa yalnız loopback ve özel ağ adreslerinden doğrudan (vekilsiz) gelen istekler yanıtlanır. |

Metrik listesi ve erişim kuralları: [İşletim](operasyon.md#metrikler).

## Öz-izleme

İzci'nin kendi hatalarını bir İzci projesine göndermesi için:

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `IZCI_SELF_URL` | (boş) | Olayları alacak İzci sunucusunun taban adresi; kendi adresi de olabilir. |
| `IZCI_SELF_KEY` | (boş) | O sunucudaki projenin ingest anahtarı (`izk_…`). İki değişken birlikte verilmelidir. |
| `IZCI_SELF_ENVIRONMENT` | `IZCI_ENV` değeri | Raporlanan olayların ortamı. |

Ayrıntılar: [İşletim](operasyon.md#öz-izleme).

## Komut satırı istemcileri

Bunlar sunucu değil, `izci` komutları tarafından okunur:

| Değişken | Komut | Açıklama |
|---|---|---|
| `IZCI_ENV_FILE` | hepsi | Okunacak `.env` dosyası. |
| `IZCI_PASSWORD` | `setup`, `user create`, `user reset-password` | Parolayı komut satırına yazmadan vermek için. |
| `IZCI_TOKEN` | `mcp` | Kişisel erişim anahtarı (`izp_…`). |
| `IZCI_URL`, `IZCI_ORG` | `mcp` | Uzak sunucu adresi ve varsayılan organizasyon. |
