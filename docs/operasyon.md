# İşletim

İzci tek binary'dir ve tek porttan çalışır. Bu sayfa İzci'nin production'da nasıl çalıştırılacağını, bir ters vekilin (reverse proxy) arkasına nasıl konacağını, nasıl yedekleneceğini ve nasıl ölçekleneceğini anlatır.

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

**`IZCI_SECRET_KEY`'i ayrıca ve güvenle yedekleyin.** Anahtar olmadan saklanan GitHub erişim anahtarları çözülemez; bu durumda bağlantılar yeniden kurulmalıdır. `IZCI_DATA_DIR` yedeklenmek zorunda değildir: içindeki depo aynaları gerektiğinde yeniden oluşturulur.

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
- Telegram polling'i veritabanı kilidiyle tek örnekte çalışır; bildirim gönderimini bütün örnekler paylaşır.
- Yapay zekâ analizleri ortak kuyruktan alınır; her örnek en çok `AI_MAX_CONCURRENT` analiz yürütür. Her örneğin kendi `IZCI_DATA_DIR` klasörü olmalıdır.
- Bakım işleri (bölüm oluşturma, saklama, proje temizliği) her örnekte çalışır ve tekrarlanabilir işlerdir.

## İzleme

| Kaynak | Açıklama |
|---|---|
| `GET /healthz` | Süreç canlı. |
| `GET /readyz` | Veritabanına ulaşılıyor. |
| `grpc.health.v1.Health/Check` | gRPC yük dengeleyicileri için. |
| Loglar | stderr'e; production'da JSON. Her istek bir `X-Request-Id` taşır ve loglarda aynı kimlikle görünür. |

Ingest kuyruğu dolduğunda istemciler `UNAVAILABLE` alır ve yeniden dener. Bu durum sürekli oluyorsa yazıcıların veritabanına yetişemediğini gösterir: `INGEST_SHARDS` ve veritabanı kaynaklarını artırın.
