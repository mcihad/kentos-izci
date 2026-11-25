# Organizasyonlar ve roller

İzci baştan **çok kiracılı** tasarlanmıştır: bir sunucu birden fazla **organizasyon** (kiracı) barındırır ve organizasyonların verileri birbirinden tamamen yalıtılmıştır. KentOS için tek organizasyon yeterlidir; ileride İzci'yi hizmet olarak (SaaS) sunmak için altyapı hazırdır.

## Kavramlar

| Kavram | Açıklama |
|---|---|
| **Organizasyon** | Kiracı. Projeler, hatalar, müşteriler, uyarılar ve entegrasyonlar organizasyona aittir. URL'de kısa adıyla görünür: `/o/kentos/…`. |
| **Proje** | İzlenen uygulama (ör. Kalem API, Kalem Web). Ingest anahtarları, ayarlar ve depo bağlantısı projeye aittir. Hata kısa kimliği proje adıyla başlar: `KALEM-1A`. |
| **Müşteri** | İzlenen uygulamanın müşterisi, ör. bir belediye. Olayla birlikte gelir (`customer`); organizasyon içinde tutulur. İzci'nin kiracısıyla karıştırılmamalıdır. |
| **Tenant** | Müşterinin içindeki alt birim, varsa (ör. bir belediyenin müdürlüğü). |

Bir kullanıcı birden fazla organizasyonun üyesi olabilir; organizasyonlar arasında sol üstteki seçiciyle geçilir.

## Roller

| Yetki | viewer | member | admin | owner |
|---|:---:|:---:|:---:|:---:|
| Hataları, olayları, müşterileri, panoyu görme | ✓ | ✓ | ✓ | ✓ |
| Çözme, yoksayma, atama, öncelik, yorum, hataları birleştirme ve ayırma | | ✓ | ✓ | ✓ |
| Müşteri adını ve notlarını düzenleme | | ✓ | ✓ | ✓ |
| Test olayı gönderme | | ✓ | ✓ | ✓ |
| Yapay zekâ analizi, dal ve çekme isteği | | ✓ | ✓ | ✓ |
| Proje oluşturma ve ayarları (gruplama kuralları dahil), ingest anahtarları, depo bağlama | | | ✓ | ✓ |
| Uyarı kuralları ve Telegram sohbetleri | | | ✓ | ✓ |
| GitHub bağlantıları | | | ✓ | ✓ |
| Üye davet etme ve rol değiştirme, denetim kaydı | | | ✓ | ✓ |
| Hata ve olay silme, kişisel veri talepleri, saklama süresi | | | ✓ | ✓ |
| Proje ve organizasyon silme | | | | ✓ |

Organizasyonun en az bir sahibi (`owner`) kalmalıdır.

**Sunucu yöneticisi** (`superadmin`) organizasyonlardan bağımsız bir yetkidir. **Sunucu yönetimi** sayfasında bütün organizasyonları, planlarını ve sınırlarını, kullanıcıları görür ve düzenler. Üyesi olmadığı bir organizasyona girdiğinde sahip gibi davranır. İlk kurulumu yapan kişi sunucu yöneticisi olur; başkalarına `izci user superadmin EMAIL` ile verilir.

## Üyelik

- **Davet:** yöneticiler **Organizasyon ayarları › Üyeler** bölümünden e-posta ve rol seçerek davet bağlantısı oluşturur. Bağlantı `AUTH_INVITATION_TTL` süresince geçerlidir. Sunucuda SMTP ayarlıysa bağlantı davet edilen adrese e-postayla da gönderilir (**Bağlantıyı e-postayla da gönder**, varsayılan açık); e-postada organizasyonun adı, daveti yapanın adı ve rol yazar. Seçenek kapalıysa ya da e-posta gönderilemezse bağlantıyı kendiniz iletirsiniz. Kötüye kullanıma karşı 15 dakikada bir adrese en çok 3, bir organizasyondan ve bir kişiden en çok 30 davet e-postası gider; sınır aşılırsa davet yine oluşur, yalnız e-postası gitmez.
- **OpenID Connect:** `OIDC_<ID>_DEFAULT_ORG` ve `OIDC_<ID>_ALLOWED_DOMAINS` ile kurumsal hesaplar ilk girişte doğrudan organizasyona katılır. Bunun için sağlayıcının e-posta adresini doğrulamış olması gerekir (ya da sağlayıcıya `OIDC_<ID>_TRUST_EMAIL` ile güvenilmesi).
- **Açık kayıt:** `AUTH_SIGNUP=open` ise herkes hesap açıp kendi organizasyonunu oluşturabilir. SaaS senaryosu içindir. Hesap ve organizasyon, e-posta adresine gönderilen bağlantı 24 saat içinde açılınca oluşur; böylece kimse başkasının adresiyle hesap tutamaz. Bir kullanıcı en çok `AUTH_SIGNUP_ORG_LIMIT` (varsayılan 3) organizasyonun sahibi olabilir. Kurulum tamamlanmadan kayıt alınmaz.
- **İki adımlı doğrulama:** yöneticiler **Organizasyon ayarları › Genel** bölümünden üyeler için [iki adımlı doğrulamayı](iki-adimli-dogrulama.md#organizasyonda-zorunlu-tutma) zorunlu tutabilir. Açmamış üyeler, açana kadar organizasyona ne tarayıcıdan ne de kişisel erişim anahtarlarıyla erişebilir.

## Erişim anahtarları

| Tür | Önek | Kimin | Ne için |
|---|---|---|---|
| Ingest anahtarı | `izk_` | Proje | Olay göndermek; "sürüm ve dosya yükleme" izniyle ayrıca sürüm kaydetmek ve [kaynak eşlemesi, sembol](kaynak-eslemeleri.md) yüklemek. Olay gönderen anahtar uygulamaların içine konur; yükleme izinli anahtar yalnız CI'da kullanılmalıdır. Projenin ilk anahtarı yalnız olay gönderir; yükleme izinli bir anahtar bir tarayıcıdan kullanılırsa bu izni kendiliğinden kaybeder. |
| Kişisel erişim anahtarı | `izp_` | Kullanıcı | API, komut satırı ve MCP. Sahibinin rolüyle çalışır; kapsamla ve tek organizasyonla daraltılabilir. |

Kişisel anahtar kapsamları: `read` (okuma), `write` (triage ve yönetim işlemleri), `ai` (yapay zekâ analizi ve çekme istekleri). Kapsam rolü genişletmez; yalnız daraltır. Anahtarlar veritabanında özet olarak saklanır, yalnız oluşturulurken bir kez gösterilir. **Hesabım › Erişim anahtarları** her anahtarın son kullanıldığı zamanı ve istemci adresini gösterir.

Yöneticiler **Organizasyon ayarları › Genel** bölümünden kişisel anahtarlar için politika koyabilir:

- **En uzun ömür (gün):** anahtarlar oluşturulduktan bu kadar gün sonra organizasyonda çalışmaz; organizasyona bağlı anahtarlar daha uzun süreli (ya da süresiz) oluşturulamaz.
- **Kullanılmayan anahtarların iptali (gün):** bu kadar gün hiç kullanılmamış bir anahtar, organizasyona eriştiği ilk istekte iptal edilir.

Politikaya uymayan anahtarla yapılan çağrılar `PERMISSION_DENIED` ve `ERROR_REASON_ACCESS_TOKEN_POLICY` döner, `sentry-cli` 403 alır. Politika değişiklikleri denetim kaydına yazılır.

## Planlar ve sınırlar

Sunucu yöneticisi her organizasyona bir plan ve sınırlar atayabilir (**Sunucu yönetimi › Organizasyonlar**):

| Sınır | Etkisi |
|---|---|
| Aylık olay | Aşıldığında yeni olaylar `RESOURCE_EXHAUSTED` (Sentry SDK'larında `429`) ile reddedilir. |
| Proje ve üye sayısı | Yeni proje ya da davet oluşturulamaz. |
| Aylık yapay zekâ analizi | Yeni analiz başlatılamaz. |
| Saklama süresi | Olaylar en çok bu kadar gün saklanır (sunucunun `RETENTION_DAYS` değerini aşamaz); organizasyon ve projeler daha kısasını seçebilir. |

Boş ya da sıfır sınır, sınırsız demektir. Kullanım **Organizasyon ayarları › Kullanım** bölümünde görünür.

## Organizasyonu ve hesabı silme

- **Organizasyon:** sahipler **Organizasyon ayarları › Genel › Organizasyonu sil** ile, kısa adı yazıp kimliklerini yeniden doğrulayarak siler. Organizasyon hemen kaybolur: üyelikler, davetler ve organizasyona bağlı erişim anahtarları silinir, ingest anahtarları çalışmaz, kısa adı boşa çıkar. Projeler, olaylar ve diğer veriler arka planda temizlenir. Denetim kaydı da silindiği için silme işlemi sunucu günlüğüne yazılır.
- **Hesap:** herkes kendi hesabını **Hesabım › Profil** bölümünden siler; sunucu yöneticisi başkasınınkini **Sunucu yönetimi › Kullanıcılar** bölümünden. Kişi organizasyonların tek sahibiyse önce sahipliği devretmeli ya da organizasyonu silmelidir. Silme, kişinin üye olduğu organizasyonların denetim kaydına yazılır ve kişiye e-postayla bildirilir.

## Yalıtım nasıl sağlanır

Kiracı yalıtımı uygulama koduna değil, **PostgreSQL satır düzeyi güvenliğine (RLS)** dayanır:

- Kiracıya ait her tablo `org_id` sütunu taşır ve `FORCE ROW LEVEL SECURITY` açıktır.
- Her istek, veritabanı işleminin başında yalnız kendi organizasyonunun kimliğini ayarlar. Politika bu kimliğe uymayan satırları görünmez kılar; bir sorguda `WHERE org_id = …` unutulsa bile başka organizasyonun verisi gelmez.
- İzci süper kullanıcı olmayan ve tabloların sahibi olan bir rolle bağlanır. Süper kullanıcı ya da `BYPASSRLS` rolü production'da reddedilir.
- Olay kaydı ve arka plan işleri gibi güvenilir sunucu yolları ayrı ve açıkça işaretlenmiş bir "sistem" kipinde çalışır.

Testler, bir organizasyonun oturumuyla başka bir organizasyonun hatasına, olayına, müşterisine ya da anahtarına erişilemediğini doğrular. Ayrıntılar: [Veri modeli](veri-modeli.md).
