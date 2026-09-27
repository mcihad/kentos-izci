# Veri modeli

İzci'nin bütün verisi PostgreSQL 18'dedir. Şema `internal/db/migrations` altındaki göçlerle oluşturulur ve binary'ye gömülüdür. Sorgular elle yazılmış SQL'dir (pgx); ORM kullanılmaz.

## Kimlik alanları

- Birincil anahtarlar **UUIDv7**'dir (`uuidv7()`): zamana göre sıralanır, dağıtık üretime uygundur.
- Olay kimliğinin zaman kısmı, olayın sunucuya ulaştığı zamana eşittir. Bu sayede bir olay kimliğiyle arandığında yalnız ilgili günlük bölüm taranır.
- Hata kısa kimliği `<PROJE>-<sıra>` biçimindedir; sıra 36 tabanında yazılır (`KALEM-1A`).

## Tablolar

**Kimlik ve üyelik (organizasyondan bağımsız):**

| Tablo | İçerik |
|---|---|
| `users` | Kullanıcılar; argon2id parola özeti, sunucu yöneticisi bayrağı, bağlı Telegram hesabı, şifreli iki adımlı doğrulama anahtarı ve hatalı kod sayacı. |
| `sessions` | Tarayıcı oturumları (çerezin özeti). |
| `recovery_codes` | [İki adımlı doğrulamanın](iki-adimli-dogrulama.md) kurtarma kodları (anahtarlı özet, kullanılma zamanı). |
| `login_challenges` | İkinci adımını bekleyen girişler (çerezin özeti); 10 dakikada geçersiz olur. |
| `oidc_identities` | OpenID Connect hesap bağlantıları. |
| `personal_tokens` | Kişisel erişim anahtarları (özet, kapsamlar, organizasyon sınırı). |
| `organizations`, `org_members` | Organizasyonlar, planlar, sınırlar ve roller. |
| `link_codes` | Telegram bağlama kodları. |
| `instance_settings` | Sunucu düzeyi ayarlar (ör. Telegram güncelleme konumu). |

**Organizasyona ait tablolar (satır düzeyi güvenlik açık):**

| Tablo | İçerik |
|---|---|
| `projects` | Projeler ve ayarları (JSON: gizlilik, uygulama kodu kuralları, hız sınırı, yapay zekâ). |
| `api_keys` | Ingest anahtarları (özet, kapsamlar, izinli kökenler). |
| `customers` | Müşteriler; olaylardan kendiliğinden oluşur, adları düzenlenebilir. |
| `environments`, `releases`, `deploys` | Ortamlar, sürümler ve dağıtımlar. |
| `issues` | Hatalar: parmak izi, başlık, culprit, seviye, durum, sayaçlar, ilk ve son görülme, atanan kişi, yapay zekâ durumu. |
| `events` | Olaylar; **günlük bölümlere** ayrılmıştır (aşağıya bakın). |
| `event_attachments` | Olay ekleri (ekran görüntüsü, günlük dosyası); istemci olay kimliğiyle olaya bağlanır, içerik `bytea` olarak lz4 ile sıkıştırılır. |
| `user_feedback` | Kullanıcı geri bildirimleri; ilgili olay saklandığında hataya bağlanır. |
| `project_filtered_daily` | [Gelen veri filtrelerinin](filtreler.md) attığı olaylar; proje, gün ve neden başına. |
| `artifact_bundles`, `artifact_files` | Yüklenen kaynak eşleme paketleri ve dosyaları (betik, eşleme; debug kimliği, sürüm, adres); içerik `bytea`, lz4 ile sıkıştırılır. |
| `debug_files` | Flutter ve Dart sembol dosyaları (ELF, Mach-O) ve karıştırma haritaları; debug kimliğiyle aranır. |
| `upload_chunks` | Birleştirilmeyi bekleyen yükleme parçaları; en çok bir saat tutulur. |
| `monitors` | Zamanlanmış görev izleyicileri: kısa ad, zamanlama (crontab ya da aralık), saat dilimi, gecikme payı, en uzun süre, eşikler. |
| `monitor_environments` | İzleyicinin her ortamdaki durumu: son ve beklenen çalışma, art arda hata ve başarı sayısı, açık hata kaydı. |
| `monitor_checkins` | Çalışmalar (check-in'ler): durum, süre, sürüm, beklenen zaman. `RETENTION_DAYS` kadar saklanır. |
| `release_sessions`, `release_session_users` | Sürüm sağlığı: saatlik oturum sayaçları ve günlük tekil kullanıcılar (kimliğin anahtarlı özeti). |
| `issue_hourly`, `project_hourly`, `customer_hourly` | Grafikler, eğilimler ve sıklık kuralları için saatlik sayaçlar. |
| `issue_tags` | Etiket dağılımları (müşteri, sürüm, tarayıcı…); aramanın ve **Etki** sekmesinin kaynağı. |
| `issue_users` | Etkilenen tekil kullanıcılar (kimliğin 8 baytlık özeti). |
| `issue_activity` | Etkinlik akışı: durum değişiklikleri, yorumlar, atamalar (otomatik olanlar kuralı ya da commit'iyle), bildirimler, yapay zekâ, çekme istekleri. |
| `project_ownership`, `owner_aliases` | [Sahiplik](sahiplik.md) kuralları, CODEOWNERS metni ve otomatik atama; `@ad` ve `#ekip` sahiplerinin üye eşlemeleri. |
| `issue_suspect_commits` | Hataların şüpheli commit'i (git blame sonucu) ya da neden bulunamadığı. |
| `alert_rules`, `notification_channels` | Uyarı kuralları ve bildirim kanalları (Telegram, e-posta, Slack, Teams, webhook, Jira); kanal ayarlarındaki adres ve anahtarlar şifreli saklanır. |
| `notification_outbox`, `notification_threads` | Gönderilecek bildirimler (işlem içi kuyruk) ve gönderilen mesajlar. |
| `integrations`, `project_repositories` | GitHub bağlantıları (şifreli anahtarlar) ve proje-depo eşlemesi. |
| `ai_runs`, `ai_run_logs` | Yapay zekâ analizleri, sonuçları, diff'leri ve günlükleri. |
| `invitations`, `audit_log`, `usage_daily` | Davetler, denetim kaydı ve günlük kullanım. |

## Olaylar ve bölümleme

`events` tablosu alındığı güne göre (`received_at`) **günlük RANGE bölümlerine** ayrılır:

- Bölümler `PARTITION_PREMAKE_DAYS` gün önceden oluşturulur; bakım işi saatte bir kontrol eder.
- Saklama süresi dolan bölümler `DROP` ile bütünüyle silinir. Satır satır silme ve tablo şişmesi olmaz.
- Olayın tamamı `payload` sütununda protojson olarak durur ve `lz4` ile sıkıştırılır. Filtrelenen alanlar (ortam, sürüm, müşteri, tenant, kullanıcı, tarayıcı, işletim sistemi, URL…) ayrıca sütun olarak tutulur ve indekslenir.
- Bir hatanın "önerilen olayı", bilgi puanı en yüksek yakın tarihli olaydır: uygulama kodu içeren yığın izi, istek ve kullanıcı bilgisi puanı artırır.

## Ingest yazımı

Olaylar proje bazında paralel yazıcılara dağıtılır. Her yazıcı olayları toplar ve **tek bir işlemde** yazar:

1. `issues` upsert: parmak izine göre kilitlenir (sabit sırayla, kilitlenme olmaz). PostgreSQL 18'in `RETURNING old/new` özelliğiyle yeni ve geri dönen hatalar ayırt edilir; yeni hatalara kısa numara verilir.
2. `events` toplu `INSERT`.
3. Saatlik sayaçlar, etiket dağılımları, kullanıcı ve müşteri sayaçları bellekte toplanıp tek seferde yazılır.
4. Uyarı kuralları değerlendirilir; bildirimler aynı işlemde `notification_outbox`'a yazılır.

İşlem tamamlanınca bekleyen istemcilere yanıt verilir ve canlı akışa yayın yapılır.

## Satır düzeyi güvenlik

Organizasyona ait her tabloda `tenant_isolation` politikası vardır. Politika hem okumada (`USING`) hem yazmada (`WITH CHECK`) şu fonksiyonu kullanır:

```sql
CREATE FUNCTION izci_tenant_visible(org uuid) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT coalesce(current_setting('izci.system', true), '') = 'on'
      OR org = nullif(current_setting('izci.org_id', true), '')::uuid
$$;
```

Ayar yapılmamış bir bağlantı hiçbir satırı görmez ve yazamaz.

Uygulama her işlemin başında yalnız ilgili organizasyonun kimliğini (`izci.org_id`) ya da güvenilir sunucu işleri için sistem kipini ayarlar. Tablolarda `FORCE ROW LEVEL SECURITY` açıktır. Böylece tabloların sahibi olan uygulama rolü de politikaya tabidir. Ayrıntılar: [Organizasyonlar ve roller](cok-kiracilik.md#yalıtım-nasıl-sağlanır).

## Göçler

```bash
izci migrate status   # uygulanmış ve bekleyen göçler
izci migrate up       # bekleyenleri uygular (serve bunu açılışta yapar)
izci migrate down     # son göçü geri alır
```

Yayımlanmış bir göç değiştirilmez; her değişiklik yeni bir göç dosyasıdır ve `Down` bölümü de yazılır.
