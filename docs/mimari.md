# Mimari

İzci tek bir Go süreci ve bir PostgreSQL veritabanından oluşur. Mesaj kuyruğu, önbellek sunucusu ya da ayrı bir arayüz sunucusu yoktur.

```
 Uygulamalar ── gRPC / Connect (izk_…) ──┐
 Sentry SDK'ları ── /api/{proje}/envelope ─┤
                                           ▼
┌──────────────────── izci (tek binary, tek port) ────────────────────┐
│ ingest: kimlik · hız/kota · normalize · maskele · grupla            │
│         → proje başına paralel yazıcılar → toplu işlem (group commit)│
│ api.v1 (Connect)  ◄── web arayüzü (gömülü React), CLI, MCP           │
│ /mcp · /auth/oidc · /telegram/webhook · /llms.txt · sağlık uçları    │
│ arka plan: bölüm yönetimi · saklama · bildirim dağıtıcı ·            │
│            Telegram · yapay zekâ koşucusu (git worktree + opencode)  │
└──────────────────────────────┬──────────────────────────────────────┘
               PostgreSQL 18 (satır düzeyi güvenlik + günlük bölümler)
```

## Tek port, üç protokol

Sunucu `net/http` ve connect-go ile yazılmıştır. Aynı dinleyici HTTP/1.1 ve HTTP/2'yi (TLS ya da şifresiz h2c) konuşur. Bu sayede aynı servis native gRPC, gRPC-Web ve Connect istemcilerine hizmet verir. Web arayüzü, Sentry uç noktaları ve MCP de aynı porttadır. Ters vekilin h2c desteklemesi yeterlidir (bkz. [İşletim](operasyon.md#ters-vekil)).

## Sözleşmeler

Bütün iletişim `proto/` altındaki protobuf sözleşmeleriyle tanımlanır:

- `izci.ingest.v1`: olay modeli ve `IngestService`. Uygulamalar ve SDK'lar kullanır. Go kodu `sdk/go` modülünde üretilir; sunucu da onu kullanır.
- `izci.api.v1`: web arayüzünün, CLI'nın ve MCP'nin kullandığı yönetim API'si.

Her mesaj, alan, enum değeri ve RPC belgelenmek zorundadır; `buf lint` bunu denetler. İngilizce başvuru belgeleri (`docs/api`) sözleşmelerden üretilir. TypeScript istemcisi `web/src/gen` altına üretilir.

## Ingest boru hattı

1. **Kimlik:** Anahtarın SHA-256 özeti önbellekte aranır; geçersiz anahtarlar da kısa süre önbelleğe alınır. Kapsam ve tarayıcı kökeni denetlenir.
2. **Hız ve kota:** Anahtar ve proje başına token kovası, organizasyonun aylık kotası.
3. **Normalize:** Varsayılanlar, saat düzeltmeleri, boyut sınırları (kırpma), [maskeleme](guvenlik.md#maskeleme), User-Agent ve istemci IP'si, uygulama kodu sınıflandırması, başlık ve culprit.
4. **[Gruplama](gruplama.md):** Parmak izi.
5. **Tekrar eleme:** Son alınan olay kimlikleri.
6. **Toplu yazım:** Olaylar proje başına bir yazıcıya gider. Her yazıcı birikenleri tek işlemde yazar: hatalar (PostgreSQL 18 `RETURNING old/new` ile yeni ve geri dönenler ayrılır), olaylar, sayaçlar, etiketler ve aynı işlemde uyarı kuralları ile bildirim kuyruğu. `INGEST_ACK=durable` iken istemciye işlem tamamlanınca yanıt verilir.
7. **Yayın:** Canlı akışa ve yeni hatalar için otomatik yapay zekâ analizine.

Kuyruk dolarsa istemci hemen `UNAVAILABLE` alır; SDK'lar bu geri baskıyla yavaşlar.

## Web arayüzü

React, TypeScript, Vite, Tailwind, shadcn/ui ve TanStack Router ile yazılmıştır. Arayüz derlenip binary'ye gömülür, önceden `br` ve `gzip` ile sıkıştırılmış olarak sunulur. Arayüz API ile Connect protokolüyle konuşur. İlk yükleme 200 KB (gzip) bütçesiyle sınırlıdır; kod renklendirme, grafikler ve Markdown gerektiğinde yüklenir. Sıkı bir CSP uygulanır: satır içi betik ve WebAssembly yoktur.

## Arka plan işleri

| İş | Ne zaman | Ne yapar |
|---|---|---|
| Bölüm yönetimi | Saatte bir | Günlük olay bölümlerini önceden oluşturur. |
| Saklama | 6 saatte bir | Süresi dolan bölümleri ve eski sayaçları siler. |
| Kullanım | 15 saniyede bir | Kota ve kullanım sayaçlarını yazar. |
| Proje temizliği | 5 dakikada bir | Silinen projelerin verisini temizler. |
| Bildirim dağıtıcı | Sürekli | Bildirim kuyruğunu Telegram'a gönderir; yeniden deneme ve hız sınırlarıyla. |
| Telegram polling | Sürekli | Tek örnekte, veritabanı kilidiyle. |
| Yapay zekâ koşucusu | Sürekli | Kuyruktaki analizleri alır ve yürütür. |

## Kod yerleşimi

| Yol | İçerik |
|---|---|
| `cmd/izci`, `internal/cli` | Komut satırı |
| `internal/server`, `internal/app` | HTTP sunucusu ve uygulamanın kurulması |
| `internal/ingest`, `internal/ingest/sentry` | Ingest boru hattı ve Sentry uyumluluğu |
| `internal/api`, `internal/auth` | Yönetim API'si, kimlik doğrulama ve yetki |
| `internal/db`, `internal/store` | Veritabanı, göçler, SQL |
| `internal/alert`, `internal/telegram` | Uyarı kuralları ve Telegram |
| `internal/github`, `internal/ai` | GitHub ve yapay zekâ analizi |
| `internal/mcp` | MCP sunucusu |
| `sdk/go` | Go SDK |
| `web` | Web arayüzü |
| `docs`, `examples` | Bu belgeler ve örnek istemciler |
