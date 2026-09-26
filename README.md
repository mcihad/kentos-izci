# İzci

**İzci**, KentOS uygulamaları için Sentry tarzı bir hata izleme ve toplama
platformudur. Tek bir Go binary'si olarak çalışır ve her şeyi tek bir porttan
sunar:

- **Hata toplama:** Uygulamalar hataları gRPC, gRPC-Web ya da Connect ile
  proje API anahtarı (`izk_…`) kullanarak gönderir. Mevcut **Sentry SDK'ları**
  da DSN'i İzci'ye çevirerek doğrudan çalışır.
- **Gruplama ve analiz:** Olaylar imzaya göre gruplanır. Müşteri, proje,
  tenant, sürüm, tarayıcı ve kullanıcı kırılımları görülür.
- **Bildirim:** Yeni ya da geri dönen hatalar Telegram ile geliştiriciye
  anında ulaşır.
- **Yapay zekâ:** opencode ile kök neden analizi yapılır; gerekirse GitHub'da
  düzeltme branch'i ya da taslak PR açılır.
- **MCP sunucusu:** Claude Code, opencode ve Cursor gibi AI araçları hataları
  sorgulayabilir.
- **Çoklu kiracılık:** PostgreSQL satır düzeyi güvenliği (RLS) ile ileride
  SaaS olarak sunulmaya hazırdır.

> Durum: geliştirme aşamasında (F0 — iskelet tamamlandı).

## Hızlı başlangıç (geliştirme)

Gereksinimler: Go 1.26+, Node 24+, pnpm 11+, buf, PostgreSQL 18+.

```bash
cp .env.example .env      # DATABASE_URL, IZCI_SECRET_KEY vb. doldurun
make setup                # Go ve web bağımlılıkları
make db-setup             # izci rolü + izci / izci_test veritabanları
make dev                  # Go sunucusu :5300, arayüz (HMR) :5301
```

Tek binary üretmek için:

```bash
make build && ./bin/izci serve
```

Tüm hedefler için `make help`, bir hedefin ayrıntısı için
`make help T=<hedef>` kullanın.

## Dokümantasyon

Rehberler `docs/` klasöründedir (Türkçe). API referansı `docs/api/`
klasöründe, proto dosyalarından üretilir (İngilizce). Depo üzerinde çalışacak
geliştiriciler ve AI ajanları için kurallar [AGENTS.md](AGENTS.md) dosyasındadır.
