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
| Çözme, yoksayma, atama, öncelik, yorum | | ✓ | ✓ | ✓ |
| Müşteri adını ve notlarını düzenleme | | ✓ | ✓ | ✓ |
| Test olayı gönderme | | ✓ | ✓ | ✓ |
| Yapay zekâ analizi, dal ve çekme isteği | | ✓ | ✓ | ✓ |
| Proje oluşturma ve ayarları, ingest anahtarları, depo bağlama | | | ✓ | ✓ |
| Uyarı kuralları ve Telegram sohbetleri | | | ✓ | ✓ |
| GitHub bağlantıları | | | ✓ | ✓ |
| Üye davet etme ve rol değiştirme, denetim kaydı | | | ✓ | ✓ |
| Proje silme | | | | ✓ |

Organizasyonun en az bir sahibi (`owner`) kalmalıdır.

**Sunucu yöneticisi** (`superadmin`) organizasyonlardan bağımsız bir yetkidir. **Sunucu yönetimi** sayfasında bütün organizasyonları, planlarını ve sınırlarını, kullanıcıları görür ve düzenler. Üyesi olmadığı bir organizasyona girdiğinde sahip gibi davranır. İlk kurulumu yapan kişi sunucu yöneticisi olur; başkalarına `izci user superadmin EMAIL` ile verilir.

## Üyelik

- **Davet:** yöneticiler **Organizasyon ayarları › Üyeler** bölümünden e-posta ve rol seçerek davet bağlantısı oluşturur. Bağlantı `AUTH_INVITATION_TTL` süresince geçerlidir.
- **OpenID Connect:** `OIDC_<ID>_DEFAULT_ORG` ve `OIDC_<ID>_ALLOWED_DOMAINS` ile kurumsal hesaplar ilk girişte doğrudan organizasyona katılır.
- **Açık kayıt:** `AUTH_SIGNUP=open` ise herkes hesap açıp kendi organizasyonunu oluşturabilir. SaaS senaryosu içindir.

## Erişim anahtarları

| Tür | Önek | Kimin | Ne için |
|---|---|---|---|
| Ingest anahtarı | `izk_` | Proje | Olay göndermek; "sürüm ve dosya yükleme" izniyle ayrıca sürüm kaydetmek ve [kaynak eşlemesi, sembol](kaynak-eslemeleri.md) yüklemek. Olay gönderen anahtar uygulamaların içine konur; yükleme izinli anahtar yalnız CI'da kullanılmalıdır. |
| Kişisel erişim anahtarı | `izp_` | Kullanıcı | API, komut satırı ve MCP. Sahibinin rolüyle çalışır; kapsamla ve tek organizasyonla daraltılabilir. |

Kişisel anahtar kapsamları: `read` (okuma), `write` (triage ve yönetim işlemleri), `ai` (yapay zekâ analizi ve çekme istekleri). Kapsam rolü genişletmez; yalnız daraltır. Anahtarlar veritabanında özet olarak saklanır, yalnız oluşturulurken bir kez gösterilir.

## Planlar ve sınırlar

Sunucu yöneticisi her organizasyona bir plan ve sınırlar atayabilir (**Sunucu yönetimi › Organizasyonlar**):

| Sınır | Etkisi |
|---|---|
| Aylık olay | Aşıldığında yeni olaylar `RESOURCE_EXHAUSTED` (Sentry SDK'larında `429`) ile reddedilir. |
| Proje ve üye sayısı | Yeni proje ya da davet oluşturulamaz. |
| Aylık yapay zekâ analizi | Yeni analiz başlatılamaz. |

Boş ya da sıfır sınır, sınırsız demektir. Kullanım **Organizasyon ayarları › Kullanım** bölümünde görünür.

## Yalıtım nasıl sağlanır

Kiracı yalıtımı uygulama koduna değil, **PostgreSQL satır düzeyi güvenliğine (RLS)** dayanır:

- Kiracıya ait her tablo `org_id` sütunu taşır ve `FORCE ROW LEVEL SECURITY` açıktır.
- Her istek, veritabanı işleminin başında yalnız kendi organizasyonunun kimliğini ayarlar. Politika bu kimliğe uymayan satırları görünmez kılar; bir sorguda `WHERE org_id = …` unutulsa bile başka organizasyonun verisi gelmez.
- İzci süper kullanıcı olmayan ve tabloların sahibi olan bir rolle bağlanır. Süper kullanıcı ya da `BYPASSRLS` rolü production'da reddedilir.
- Olay kaydı ve arka plan işleri gibi güvenilir sunucu yolları ayrı ve açıkça işaretlenmiş bir "sistem" kipinde çalışır.

Testler, bir organizasyonun oturumuyla başka bir organizasyonun hatasına, olayına, müşterisine ya da anahtarına erişilemediğini doğrular. Ayrıntılar: [Veri modeli](veri-modeli.md).
