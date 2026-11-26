# İşletim

İzci tek binary'dir ve tek porttan çalışır. Bu sayfa İzci'nin production'da nasıl çalıştırılacağını, bir ters vekilin (reverse proxy) arkasına nasıl konacağını, nasıl yedekleneceğini ve nasıl ölçekleneceğini anlatır.

## Docker

İmaj üç aşamada derlenir: web arayüzü, statik binary ve çalışma ortamı. Çalışma ortamı `git` içeren Debian slim'dir ve İzci'yi root olmayan `izci` kullanıcısıyla (uid 10001) çalıştırır. Veri klasörü `/var/lib/izci`'dir; yapay zekâ çalışma alanı ve opencode oturumu burada durur.

```bash
make docker                                  # izci:<sürüm>
make docker WITH_OPENCODE=true               # yapay zekâ analizi için opencode dahil
make docker IMAGE=registry.example.com/izci:v1.0.0
make docker KENTOS_COMPONENTS=/yol/kentos-components   # bileşen seti başka bir yerdeyse
```

Arayüz KentOS bileşen setini (`kentos-components`) kullanır; `make docker` onu izci'nin yanındaki klasörden adlı bir derleme bağlamı olarak verir. Elle derlerken aynı bağlamı ekleyin: `docker build --build-context kentos-components=../kentos-components .`

İmaj `IZCI_ENV=production`, `IZCI_HTTP_ADDR=:5300` ve `IZCI_DATA_DIR=/var/lib/izci` ile gelir. Veritabanı adresi, gizli anahtar ve dış adres sizin vermeniz gereken değerlerdir:

```yaml
# compose.yaml
services:
  izci:
    image: izci:v1.0.0
    restart: unless-stopped
    environment:
      IZCI_PUBLIC_URL: https://izci.example.com
      IZCI_SECRET_KEY: ${IZCI_SECRET_KEY}          # openssl rand -base64 32
      DATABASE_URL: postgres://izci:${IZCI_DB_PASSWORD}@postgres:5432/izci?sslmode=disable
      IZCI_TRUSTED_PROXIES: 172.16.0.0/12          # ters vekilin ağı
    volumes:
      - izci-data:/var/lib/izci
    ports:
      - "5300:5300"
    stop_grace_period: 30s
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:18
    restart: unless-stopped
    environment:
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}     # yönetici rolü: postgres
    volumes:
      - postgres-data:/var/lib/postgresql
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "postgres"]
      interval: 5s
      retries: 10

volumes:
  izci-data:
  postgres-data:
```

Resmî `postgres` imajı `POSTGRES_USER` rolünü **süper kullanıcı** olarak açar; İzci ise production'da süper kullanıcı rolüyle açılmayı reddeder, çünkü satır düzeyi güvenlik böyle bir rolde uygulanmaz. Bu yüzden İzci kendi rolüyle bağlanır. İlk açılıştan önce `izci` rolünü ve veritabanını bir kez oluşturun; rolün parolası `DATABASE_URL`'den alınır:

```bash
docker compose run --rm -e DB_ADMIN_URL="postgres://postgres:${POSTGRES_PASSWORD}@postgres:5432/postgres?sslmode=disable" izci db bootstrap
docker compose up -d
```

- **Sağlık denetimi:** İmajın `HEALTHCHECK` komutu `izci healthcheck`'tir; `/healthz` 200 dönmezse konteyner `unhealthy` olur. Veritabanını da denetlemek için `izci healthcheck --ready` kullanın. İmajda curl yoktur.
- **Kapanış:** İzci kapanırken ingest kuyruğunu `IZCI_SHUTDOWN_TIMEOUT` (varsayılan 20 saniye) boyunca boşaltır. Docker'ın varsayılan bekleme süresi 10 saniyedir; `stop_grace_period` değerini bundan uzun tutun.
- **Komutlar:** İmajın giriş noktası `izci`'dir: `docker compose exec izci izci user list`, `docker compose run --rm izci migrate status`.
- **opencode:** `WITH_OPENCODE=true` ile derlenen imajda sağlayıcı girişi için `docker compose exec izci opencode auth login` çalıştırın; oturum bilgisi veri biriminde saklanır. Sağlayıcı anahtarları ortam değişkeniyle de verilebilir (ör. `ANTHROPIC_API_KEY`); analiz süreci yalnız bu tür bilinen değişkenleri görür.
- **Ajan yalıtımı:** Bu imajda bubblewrap da vardır. `AI_SANDBOX=bwrap` için konteynerin ad alanı açmasına izin verin: `security_opt: ["seccomp=unconfined", "apparmor=unconfined"]`. `docker compose exec izci izci ai doctor` çıktısındaki **sandbox** satırı yalıtımın çalıştığını gösterir. Ajana komut çalıştırma izni (`AI_ALLOWED_COMMANDS`) yalnız yalıtım çalışırken verin.

**Dokploy:** Uygulamayı bu depodan "Dockerfile" türüyle oluşturun, ortam değişkenlerini Dokploy'a girin ve `/var/lib/izci` için kalıcı bir birim ekleyin. Alan adını bağlarken hizmet portu 5300'dür. Native gRPC istemcileri için Traefik'in İzci'ye h2c ile bağlanması gerekir (aşağıdaki etiketlere bakın).

## systemd ile çalıştırma

```ini
# /etc/systemd/system/izci.service
[Unit]
Description=izci error tracking
After=network-online.target
Wants=network-online.target

[Service]
User=izci
Group=izci
EnvironmentFile=/etc/izci/izci.env
Environment=IZCI_DATA_DIR=/var/lib/izci HOME=/var/lib/izci
WorkingDirectory=/var/lib/izci
ExecStart=/usr/local/bin/izci serve
Restart=on-failure
TimeoutStopSec=30
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/izci

[Install]
WantedBy=multi-user.target
```

`/etc/izci/izci.env` dosyası `KEY=değer` satırlarından oluşur (bkz. [Yapılandırma](yapilandirma.md)) ve yalnız `root` ile `izci` kullanıcısı tarafından okunabilmelidir. Yapay zekâ analizi açıksa opencode'un oturum bilgileri `HOME` altında durur; `opencode auth login` komutunu `izci` kullanıcısıyla çalıştırın.

İzci'yi kendi kullanıcısıyla çalıştırın. Yapay zekâ projelerinde komut çalıştırma izni veriyorsanız bu ayrım daha da önemlidir (bkz. [GitHub ve yapay zekâ](yapay-zeka.md#güvenlik)).

## Ters vekil

Tek port; tarayıcı trafiğini, Connect ve Sentry isteklerini (HTTP/1.1) ve **native gRPC**'yi (HTTP/2) birlikte taşır. Native gRPC'nin çalışması için vekil, İzci'ye **h2c** (şifresiz HTTP/2) ile bağlanmalıdır. Vekil TLS'i sonlandırıyorsa `IZCI_TRUSTED_PROXIES` değerine vekilin adresini yazın; istemci IP'leri ancak böyle doğru kaydedilir.

**Traefik (Dokploy):**

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.izci.rule=Host(`izci.example.com`)
  - traefik.http.routers.izci.entrypoints=websecure
  - traefik.http.routers.izci.tls.certresolver=letsencrypt
  - traefik.http.services.izci.loadbalancer.server.port=5300
  - traefik.http.services.izci.loadbalancer.server.scheme=h2c
```

`scheme=h2c` olmadan web arayüzü, Connect ve Sentry çalışır; native gRPC istemcileri hata alır.

**Caddy:**

```
izci.example.com {
	reverse_proxy h2c://127.0.0.1:5300
}
```

**nginx:** gRPC isteklerini içerik türüne göre ayırın:

```nginx
map $http_content_type $izci_grpc {
    ~^application/grpc 1;
    default            0;
}

server {
    listen 443 ssl;
    http2 on;
    server_name izci.example.com;
    # ssl_certificate …

    client_max_body_size 25m;

    location / {
        if ($izci_grpc) {
            grpc_pass grpc://127.0.0.1:5300;
        }
        proxy_pass http://127.0.0.1:5300;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;          # canlı akış ve yapay zekâ günlüğü
        proxy_read_timeout 1h;
    }
}
```

**Canlı akış** sayfası ve yapay zekâ günlüğü uzun süreli akış bağlantıları kullanır. Vekildeki okuma zaman aşımını en az birkaç dakika tutun ve yanıt tamponlamayı kapatın.

İzci, istek gövdesini çok yavaş gönderen istemcileri keser: bir gövde için 30 saniye ile gövdenin saniyede 64 KiB hızla gelmesi için gereken süre tanınır (en çok 32 MiB'lık gövdeye göre, yaklaşık 9 dakika). Gövde okunduktan sonra süre sınırı kalkar; uzun süren işlemler ve akış yanıtları etkilenmez. Vekil önde olsa da olmasa da yavaş gövdeyle bağlantıları tüketme saldırısı bu yüzden sınırlıdır.

## Veritabanı

- PostgreSQL 18 gerekir. İzci'nin rolü süper kullanıcı olmamalıdır (bkz. [Kurulum](kurulum.md#2-veritabanını-hazırlayın)).
- Olaylar günlük bölümlerde tutulur ve `lz4` ile sıkıştırılır. Bölümler `PARTITION_PREMAKE_DAYS` gün önceden oluşturulur, saklama süresi dolanlar bütünüyle silinir. Bu sayede büyük `DELETE` işlemleri ve tablo şişmesi olmaz.
- Başlangıç için 2 vCPU ve 4 GB belleğe sahip bir PostgreSQL, günde birkaç milyon olayı rahatça taşır. Disk için olay başına ortalama 2–4 KB hesaplayın.

## Yedekleme

```bash
# Günlük tam yedek (sıkıştırılmış özel biçim)
pg_dump -Fc -d "$DATABASE_URL" -f izci-$(date +%F).dump

# Hızlı, küçük yedek: ayarlar, hatalar ve kullanıcılar; olaylar hariç
pg_dump -Fc -d "$DATABASE_URL" --exclude-table-data='events_*' -f izci-ayarlar-$(date +%F).dump

# Geri yükleme (boş bir veritabanına, izci rolüyle)
pg_restore -d "$DATABASE_URL" --no-owner --role=izci izci-2026-09-27.dump
```

**`IZCI_SECRET_KEY`'i ayrıca ve güvenle yedekleyin.** Anahtar olmadan saklanan GitHub erişim anahtarları, bildirim kanalı sırları ve iki adımlı doğrulama anahtarları çözülemez; bu durumda bağlantılar yeniden kurulmalı, kullanıcıların iki adımlı doğrulaması `izci user reset-2fa` ile sıfırlanmalıdır. `IZCI_DATA_DIR` yedeklenmek zorunda değildir: içindeki depo aynaları gerektiğinde yeniden oluşturulur.

## Sürüm yükseltme

1. Yeni binary'yi (ya da imajı) yerleştirin.
2. Yeniden başlatın. `DB_AUTO_MIGRATE=true` iken göçler açılışta uygulanır.

Birden fazla örnek çalışıyorsa yükseltmeden önce göçleri bir kez uygulayın (`izci migrate up`), sonra örnekleri sırayla yeniden başlatın. Göçler geriye uyumlu yazılır; eski sürüm yeni şemayla çalışmayı sürdürür. `izci migrate status` uygulanmış göçleri listeler.

Kapanışta İzci önce yeni istek almayı bırakır, ardından ingest kuyruğunu `IZCI_SHUTDOWN_TIMEOUT` süresince veritabanına boşaltır. Süren yapay zekâ analizleri "sunucu kapandığı için durduruldu" olarak işaretlenir.

## Ölçekleme

Tek bir örnek saniyede binlerce olay alır. Kendi donanımınızda ölçmek için:

```bash
izci bench ingest --url https://izci.example.com --key izk_… --rate 5000 --duration 30s
```

Önce dikey ölçekleyin: `INGEST_SHARDS` (paralel yazıcı), `DB_MAX_CONNS` ve veritabanı kaynakları.

Birden fazla örneği aynı veritabanıyla bir yük dengeleyicinin arkasında çalıştırabilirsiniz:

- API, ingest ve arayüz durumsuzdur; oturumlar veritabanındadır.
- Hız sınırları ve tekrar eleme belleği **örnek başınadır**. N örnekte etkili hız sınırı yaklaşık N katına çıkar; sınırları buna göre ayarlayın.
- Telegram polling'i veritabanı kilidiyle tek örnekte çalışır; bildirim gönderimini (bütün kanallar) bütün örnekler paylaşır.
- Yapay zekâ analizleri ortak kuyruktan alınır; her örnek en çok `AI_MAX_CONCURRENT` analiz yürütür. Her örneğin kendi `IZCI_DATA_DIR` klasörü olmalıdır.
- Bakım işleri (bölüm oluşturma, saklama, proje temizliği) her örnekte çalışır ve tekrarlanabilir işlerdir.
- Zamanlanmış görev kontrolü her örnekte çalışır; satırlar kilitlenerek alındığı için bir çalışma yalnız bir kez kaçırılmış sayılır.
- Her örnek, yığın izi çözmek için kaynak eşlemelerini ve sembol dosyalarını kendi belleğinde önbelleğe alır (`INGEST_SYMBOL_CACHE_MB`, varsayılan 256 MiB). Büyük Flutter uygulamalarının sembol dosyaları çözülmüş hâlde yüz megabaytı bulabilir; bellek sınırlarını buna göre verin. Yüklemeler `IZCI_DATA_DIR/tmp` altında birleştirilir; aynı anda en çok iki birleştirme yapılır ve her biri yaklaşık `INGEST_UPLOAD_MAX_MB` kadar bellek kullanır.

## İzleme

| Kaynak | Açıklama |
|---|---|
| `GET /healthz` | Süreç canlı. `izci healthcheck` bunu denetler. |
| `GET /readyz` | Veritabanına ulaşılıyor. `izci healthcheck --ready` bunu denetler. |
| `grpc.health.v1.Health/Check` | gRPC yük dengeleyicileri için. |
| `GET /metrics` | Prometheus metrikleri (aşağıda). |
| Loglar | stderr'e; production'da JSON. Her istek bir `X-Request-Id` taşır ve loglarda aynı kimlikle görünür. |

Ingest kuyruğu dolduğunda istemciler `UNAVAILABLE` alır ve yeniden dener. Bu durum sürekli oluyorsa yazıcıların veritabanına yetişemediğini gösterir: `INGEST_SHARDS` ve veritabanı kaynaklarını artırın.

### Metrikler

`/metrics` Prometheus metin biçimindedir ve varsayılan olarak açıktır (`METRICS_ENABLED`). Etiketler organizasyon ya da proje bilgisi taşımaz; bütün seriler örnek geneli toplamlardır.

**Erişim:** `METRICS_TOKEN` tanımlıysa her istek `Authorization: Bearer <token>` taşımalıdır. Tanımlı değilse yalnız loopback ve özel ağ adreslerinden (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`) **doğrudan** gelen istekler yanıtlanır. `X-Forwarded-For`, `Forwarded` ya da `X-Real-Ip` taşıyan, yani bir ters vekilden geçen istek reddedilir; böylece dışarıya açık vekil uç noktayı kazara yayımlamaz. Portu doğrudan dışarı açıyorsanız ya da ağınız kaynak adresi korumuyorsa (ör. rootless Docker) bir anahtar tanımlayın.

```yaml
# prometheus.yml
scrape_configs:
  - job_name: izci
    static_configs:
      - targets: ["izci:5300"]
    authorization:
      credentials_file: /etc/prometheus/izci-token   # METRICS_TOKEN kullanıyorsanız
```

| Metrik | Tür | Açıklama |
|---|---|---|
| `izci_ingest_events_total{outcome}` | sayaç | Yanıtlanan olaylar: `accepted`, `duplicate`, `filtered`, `rate_limited`, `invalid`, `unavailable` (kuyruk dolu ya da yazılamadı; istemci yeniden dener). |
| `izci_ingest_filtered_total{reason}` | sayaç | [Gelen veri filtrelerinin](filtreler.md) attığı olaylar: `environment`, `browser_extension`, `web_crawler`, `legacy_browser`, `localhost`, `error_message`, `ip_address`, `release`. |
| `izci_ingest_stored_events_total`, `izci_ingest_batches_total` | sayaç | Veritabanına yazılan olaylar ve toplu yazma işlemleri. |
| `izci_ingest_failed_events_total` | sayaç | Yeniden denemelere rağmen yazılamayan olaylar. |
| `izci_ingest_batch_duration_seconds` | histogram | Bir toplu yazmanın süresi (yeniden denemeler dahil). |
| `izci_ingest_queue_depth`, `izci_ingest_queue_capacity` | gösterge | Kuyruktaki olaylar ve toplam kuyruk kapasitesi. |
| `izci_ingest_queue_full_total` | sayaç | Kuyruk dolu olduğu için geri çevrilen gönderimler. |
| `izci_issues_created_total`, `izci_issues_regressed_total` | sayaç | Yeni açılan ve geri dönen hatalar. |
| `izci_sentry_items_dropped_total{type}` | sayaç | İşlenmeden atılan Sentry öğeleri (`transaction`, `span`, `profile`, `replay_event`, …). |
| `izci_sentry_attachments_total{outcome}`, `izci_sentry_attachment_bytes_total` | sayaç | Olay ekleri: `stored`, `discarded` (proje ayarı ya da kabul edilmeyen olay), `too_large`; saklanan bayt. |
| `izci_sentry_feedback_total{outcome}` | sayaç | Kullanıcı geri bildirimleri: `stored`, `duplicate`, `rejected`. |
| `izci_sentry_sessions_total{outcome}` | sayaç | Sürüm sağlığı oturum güncellemeleri: `accepted`, `invalid` (sürümsüz, çok eski), `filtered` (yok sayılan ortam). |
| `izci_symbol_uploads_total{outcome}` | sayaç | [Kaynak eşlemesi ve sembol](kaynak-eslemeleri.md) yüklemeleri: `stored`, `duplicate` (zaten vardı), `rejected` (okunamayan ya da kotayı aşan dosya). |
| `izci_symbolicated_frames_total{kind,outcome}` | sayaç | Çözülmeye çalışılan yığın çerçeveleri; `kind`: `sourcemap`, `native`; `outcome`: `resolved`, `missing`. |
| `izci_symbol_cache_bytes` | gösterge | Bellekteki çözülmüş kaynak eşlemeleri ve sembol dosyaları (`INGEST_SYMBOL_CACHE_MB` ile sınırlı). |
| `izci_monitor_checkins_total{outcome}` | sayaç | Zamanlanmış görev check-in'leri: `accepted`, `unknown_monitor` (izleyici yok, yapılandırma da yok), `invalid`, `rate_limited`. |
| `izci_monitor_missed_total`, `izci_monitor_timeouts_total` | sayaç | İzci'nin kaydettiği kaçırılan ve zaman aşımına uğrayan çalışmalar. |
| `izci_monitor_incident_events_total`, `izci_monitor_recoveries_total` | sayaç | Zamanlanmış görev sorunları için üretilen hata olayları ve kendiliğinden çözülen kayıtlar. |
| `izci_sentry_client_discarded_total{reason,category}` | sayaç | SDK'ların istemci raporlarında bildirdiği, göndermeden attıkları (ör. `sample_rate`, `queue_overflow`, `ratelimit_backoff`). |
| `izci_http_requests_total{handler,code}` | sayaç | HTTP istekleri; `handler`: `ingest`, `sentry`, `api`, `stream`, `mcp`, `auth`, `docs`, `grpc`, `health`, `metrics`, `telegram`, `integrations`, `ui`. |
| `izci_http_request_duration_seconds{handler}` | histogram | İstek süreleri; uzun ömürlü akışlar (`stream`) dahil değildir. |
| `izci_http_requests_in_flight` | gösterge | Süren istekler (açık akışlar dahil). |
| `izci_live_subscribers` | gösterge | Canlı akışı izleyen bağlantılar. |
| `izci_notifications_total{channel,result}` | sayaç | Bildirimler; `channel`: `telegram`, `email`, `slack`, `teams`, `webhook`, `jira`; `result`: `delivered`, `skipped` (kanal sessiz ya da silinmiş), `retried`, `dropped`. |
| `izci_ai_runs_total{status}`, `izci_ai_run_duration_seconds` | sayaç, histogram | Biten yapay zekâ analizleri (`succeeded`, `failed`, `cancelled`) ve süreleri. |
| `izci_ai_runs_running` | gösterge | Bu örnekte süren analizler. |
| `izci_ai_tokens_total{kind}`, `izci_ai_cost_usd_total` | sayaç | Modelin bildirdiği token ve maliyet. |
| `izci_db_pool_connections{state}`, `izci_db_pool_max_connections` | gösterge | Bağlantı havuzu: `acquired`, `idle`, `constructing`. |
| `izci_db_pool_empty_acquires_total`, `izci_db_pool_empty_acquire_wait_seconds_total` | sayaç | Boş bağlantı bulamayıp bekleyen istekler ve toplam bekleme. Hızla artıyorsa `DB_MAX_CONNS` düşüktür. |
| `izci_build_info{version,commit,go_version}` | gösterge | Çalışan sürüm (değeri 1). |
| `go_*`, `process_*` | | Go çalışma zamanı ve süreç metrikleri (süreç metrikleri Linux'ta). |

Başlangıç için uyarılar:

```promql
# Olayların %1'inden fazlası geri çevriliyor
sum(rate(izci_ingest_events_total{outcome="unavailable"}[5m]))
  / sum(rate(izci_ingest_events_total[5m])) > 0.01

# Kuyruk yarıdan fazla dolu
izci_ingest_queue_depth / izci_ingest_queue_capacity > 0.5

# Yazılamayan olay var
increase(izci_ingest_failed_events_total[10m]) > 0
```

### Öz-izleme

İzci kendi hatalarını Go SDK'sıyla bir İzci sunucusuna gönderebilir: ayrı bir izleme örneğine ya da kendi üzerindeki bir projeye. `IZCI_SELF_URL` ve `IZCI_SELF_KEY` birlikte verildiğinde sunucu logunun `error` kayıtları olay olur; `info` ve `warn` kayıtları bu olayların breadcrumb'larıdır. Olaylar `izci@<sürüm>` sürümüyle ve `IZCI_SELF_ENVIRONMENT` ortamıyla (varsayılan `IZCI_ENV`) gelir. İstek işlerken oluşan panic'ler, panic'in oluştuğu satırı gösteren yığın iziyle raporlanır.

1. İzci'de bir proje açın (ör. `izci`, platform Go) ve yalnız `ingest` kapsamlı bir anahtar oluşturun.
2. `IZCI_SELF_URL=https://izci.example.com` ve `IZCI_SELF_KEY=izk_…` değerlerini verip yeniden başlatın.

Hedef aynı örnekse, bildirilen bir hatanın kaydedilmesi sırasında oluşan yeni bir hata döngü kurabilir. Bu yüzden raporlar dakikada 60 ile sınırlıdır; fazlası atılır ve kapanışta sayısı loglanır. Veritabanı tamamen erişilemezken oluşan hatalar aynı örneğe ulaşamaz; bu durumlar için ayrı bir izleme örneği daha güvenilirdir.
