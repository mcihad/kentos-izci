# İzci

**İzci**, KentOS uygulamaları için kendi sunucunuzda çalışan, Sentry tarzı bir hata izleme ve toplama platformudur. Tek bir Go binary'sidir ve her şeyi tek porttan sunar.

- **Hata toplama:** Uygulamalar hataları gRPC, gRPC-Web ya da Connect (JSON dahil) ile gönderir. Mevcut **Sentry SDK'ları** yalnız DSN değiştirilerek çalışır. Go için SDK hazırdır; C#, TypeScript, Dart ve Python için bağımlılıksız örnekler vardır.
- **Gruplama ve analiz:** Olaylar yığın izine göre hatalara toplanır. Müşteri (belediye), tenant, proje, sürüm, tarayıcı ve kullanıcı kırılımları, müşteri sağlığı ve canlı akış sunulur.
- **Bildirim:** Yeni, geri dönen ya da hızla tekrarlayan hatalar Telegram'a gider. Hatalar Telegram'dan çözülebilir.
- **Yapay zekâ:** opencode, hatanın oluştuğu sürümün kodunu inceler, kök nedeni açıklar ve düzeltme önerir. Öneri incelendikten sonra GitHub'da dal ya da taslak çekme isteği olarak açılır.
- **MCP:** Claude Code, opencode ve Cursor hataları arayabilir, triage edebilir ve analiz başlatabilir.
- **Çoklu kiracılık:** Organizasyonlar PostgreSQL satır düzeyi güvenliğiyle yalıtılır; SaaS'a hazırdır.
- **Türkçe arayüz**, koyu ve açık tema, klavye kısayolları.

## Hızlı başlangıç

Gereksinim: PostgreSQL 18.

```bash
make build                                   # bin/izci (arayüz gömülü)
cp .env.example .env                         # DATABASE_URL, DB_ADMIN_URL…
./bin/izci db bootstrap                      # izci rolü ve veritabanları
./bin/izci serve                             # http://localhost:5300 → kurulum sihirbazı
```

İlk olayı göndermek için:

```bash
IZCI_KEY=izk_… ./examples/curl/send-event.sh
```

Ayrıntılar: [Kurulum ve ilk çalıştırma](docs/kurulum.md).

## Belgeler

Rehberler [`docs/`](docs/README.md) klasöründedir (Türkçe) ve çalışan her sunucunun **Belgeler** sayfasında da okunur. Öne çıkanlar:

- [Hata gönderme](docs/ingest.md) · [Go SDK](docs/go-sdk.md) · [Sentry SDK'ları](docs/sentry.md)
- [Arama dili](docs/arama.md) · [Gruplama](docs/gruplama.md)
- [Telegram](docs/telegram.md) · [GitHub ve yapay zekâ](docs/yapay-zeka.md) · [MCP](docs/mcp.md)
- [Yapılandırma](docs/yapilandirma.md) · [İşletim](docs/operasyon.md) · [Güvenlik ve KVKK](docs/guvenlik.md)

API başvurusu [`docs/api`](docs/api/README.md) klasöründe, proto dosyalarından üretilir (İngilizce). Yapay zekâ araçları için sunucu `/llms.txt` ve `/llms-full.txt` adreslerini sunar.

## Geliştirme

Gereksinimler: Go 1.26+, Node 24+, pnpm 11+, buf, PostgreSQL 18+.

```bash
cp .env.example .env
make setup        # Go ve web bağımlılıkları
make db-setup     # izci rolü ve izci / izci_test veritabanları
make dev          # Go sunucusu :5300, arayüz (HMR) :5301
make check        # lint, tip denetimi ve bütün testler
```

`make help` bütün hedefleri, `make help T=<hedef>` bir hedefin ayrıntısını gösterir. Depo üzerinde çalışan geliştiriciler ve yapay zekâ ajanları için kurallar [AGENTS.md](AGENTS.md) dosyasındadır.
