# Kurulum ve ilk çalıştırma

İzci tek bir Go binary'sidir. Web arayüzü, belgeler ve veritabanı göçleri binary'nin içindedir. Çalışması için yalnızca bir PostgreSQL 18 veritabanı gerekir.

## Gereksinimler

| Bileşen | Sürüm | Ne için |
|---|---|---|
| PostgreSQL | 18 veya üstü | Zorunlu. `uuidv7()` ve `RETURNING old/new` kullanılır; açılışta sürüm kontrol edilir. |
| git | 2.30+ | Yalnız yapay zekâ analizi için (depo aynaları ve worktree'ler). |
| opencode | 1.18+ | Yalnız yapay zekâ analizi için. Bkz. [GitHub ve yapay zekâ analizi](yapay-zeka.md). |
| Go, Node, pnpm, buf | Go 1.26, Node 24, pnpm 11 | Yalnız kaynaktan derlerken. |

## 1. Binary'yi edinin

Kaynaktan derlemek için:

```bash
make setup     # Go ve web bağımlılıkları
make build     # bin/izci (arayüz gömülü, CGO kapalı)
./bin/izci version
```

Docker ile çalıştırma için [İşletim](operasyon.md) rehberine bakın.

## 2. Veritabanını hazırlayın

İzci, tablolarının sahibi olan ve **süper kullanıcı olmayan** bir rolle bağlanmalıdır. Kiracı yalıtımı PostgreSQL satır düzeyi güvenliğiyle (RLS) sağlanır; süper kullanıcı ya da `BYPASSRLS` yetkili bir rol bu korumayı atlar. Bu yüzden `izci serve` production ortamında böyle bir rolle açılmayı reddeder.

En kolay yol, `izci db bootstrap` komutudur. Komut `DB_ADMIN_URL` içindeki süper kullanıcı bağlantısıyla şunları yapar:

- `DATABASE_URL` içindeki rolü oluşturur ya da parolasını günceller;
- rolden `SUPERUSER` ve `BYPASSRLS` yetkilerini alır;
- `DATABASE_URL` ve `IZCI_TEST_DATABASE_URL` içindeki veritabanlarını bu rolün sahipliğinde açar.

Komut tekrar çalıştırılabilir; var olan veriye dokunmaz.

```bash
cp .env.example .env
# .env içinde DATABASE_URL ve DB_ADMIN_URL değerlerini düzenleyin
./bin/izci db bootstrap
```

Elle yapmak isterseniz:

```sql
CREATE ROLE izci LOGIN PASSWORD 'güçlü-bir-parola' NOSUPERUSER NOBYPASSRLS;
CREATE DATABASE izci OWNER izci;
```

## 3. Yapılandırın

`.env.example` dosyasındaki her değişken açıklamalıdır; tam başvuru için [Yapılandırma](yapilandirma.md) rehberine bakın. Production için en az şunlar gerekir:

```bash
IZCI_ENV=production
IZCI_PUBLIC_URL=https://izci.example.com
IZCI_SECRET_KEY=$(openssl rand -base64 32)   # bir kez üretin, saklayın, değiştirmeyin
DATABASE_URL=postgres://izci:…@db:5432/izci?sslmode=require
```

`IZCI_SECRET_KEY`, saklanan GitHub erişim anahtarlarını şifreler ve imzalar üretir. Anahtar kaybolursa bu kayıtlar yeniden girilmelidir. Geliştirmede boş bırakılırsa her açılışta geçici bir anahtar üretilir.

## 4. Sunucuyu başlatın

```bash
./bin/izci serve
```

`serve`, bekleyen veritabanı göçlerini uygular (`DB_AUTO_MIGRATE=true`) ve olay bölümlerini önceden oluşturur. Ardından tek porttan (varsayılan `:5300`) şunları sunar: web arayüzü, gRPC ve Connect ingest, Sentry uç noktaları, yönetim API'si, `/mcp`, `/healthz` ve `/readyz`.

## 5. İlk yöneticiyi oluşturun

Hiç kullanıcı yokken `IZCI_PUBLIC_URL` adresi kurulum sihirbazına yönlenir. Sihirbazda yönetici, ilk organizasyon ve isteğe bağlı ilk proje oluşturulur.

Aynı işi komut satırından da yapabilirsiniz:

```bash
IZCI_PASSWORD='en az 10 karakterlik parola' ./bin/izci setup \
  --email admin@kentos.com.tr --name "Ayşe Yılmaz" \
  --org-name "KentOS" --project "Kalem" --platform dotnet
```

Kurulumu yapan kişi hem organizasyon sahibi (`owner`) hem de sunucu yöneticisi (`superadmin`) olur.

## 6. İlk olayı gönderin

1. Arayüzde **Projeler › proje › Anahtarlar** bölümünden bir ingest anahtarı (`izk_…`) oluşturun ya da `izci key create` komutunu kullanın.
2. **Projeler › proje › Kurulum** sayfası; Go SDK, gRPC, Connect JSON, C#, TypeScript, Dart, Python ve Sentry DSN için hazır örnekler gösterir.
3. Hızlı bir deneme için:

```bash
./bin/izci event send --url http://localhost:5300 --key izk_… --count 5
```

Olaylar birkaç saniye içinde **Hatalar** sayfasında görünür. Gönderim yolları için [Hata gönderme](ingest.md) rehberine bakın.

## Sağlık uçları

| Yol | Anlamı |
|---|---|
| `GET /healthz` | Süreç ayakta (yük dengeleyici canlılık denetimi). |
| `GET /readyz` | Veritabanına ulaşılıyor; trafik alabilir. |
| `grpc.health.v1.Health/Check` | Standart gRPC sağlık servisi. |

## Sonraki adımlar

- [Telegram bildirimleri](telegram.md)
- [GitHub ve yapay zekâ analizi](yapay-zeka.md)
- [MCP ile yapay zekâ asistanları](mcp.md)
- [Organizasyonlar ve roller](cok-kiracilik.md)
