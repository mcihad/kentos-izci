# Komut satırı

`izci` binary'si hem sunucudur hem yönetim aracıdır. Komutlar ve yardım metinleri İngilizcedir; `izci <komut> --help` her komutun bayraklarını gösterir. Bütün komutlar yapılandırmayı sunucuyla aynı yerden okur (ortam değişkenleri ve `.env`; `--env-file` ile başka dosya).

Veritabanına bağlanan komutlar (`setup`, `user`, `org`, `project`, `key`, `token`, `ai`, `demo`, `migrate`) sunucunun kurulu olduğu makinede, onun yapılandırmasıyla çalıştırılır. `event`, `bench` ve `mcp --url` ise ağ üzerinden bir sunucuya bağlanır.

## Sunucu ve veritabanı

| Komut | Açıklama |
|---|---|
| `izci serve` | Sunucuyu başlatır: ingest, API, arayüz, MCP; tek port. |
| `izci migrate up` / `down` / `status` | Şema göçleri. `serve`, `DB_AUTO_MIGRATE=true` iken `up` işlemini kendisi yapar. |
| `izci db bootstrap` | `DB_ADMIN_URL` ile İzci rolünü ve veritabanlarını oluşturur (bkz. [Kurulum](kurulum.md)). |
| `izci db reset` | Bütün veriyi siler ve şemayı yeniden kurar. **Yalnız geliştirme için.** |
| `izci version [--json]` | Sürüm, commit ve derleme bilgisi. |

## İlk kurulum ve kullanıcılar

```bash
IZCI_PASSWORD='…' izci setup --email admin@kentos.com.tr --name "Ayşe Yılmaz" \
  --org-name KentOS --project Kalem --platform dotnet
```

| Komut | Bayraklar |
|---|---|
| `izci setup` | `--email`, `--name`, `--password`, `--org-name`, `--org`, `--project`, `--platform` |
| `izci user create` | `--email`, `--name`, `--password`, `--org`, `--role` (`viewer`, `member`, `admin`, `owner`), `--superadmin` |
| `izci user list` | — |
| `izci user reset-password EMAIL` | `--password`; kullanıcının açık oturumlarını kapatır. |
| `izci user superadmin EMAIL` | `--revoke` ile yetkiyi geri alır. |

Parolalar `--password`, `IZCI_PASSWORD` ya da standart girdiden okunur. Komut geçmişinde kalmaması için `IZCI_PASSWORD` ya da standart girdiyi tercih edin.

## Organizasyonlar, projeler ve anahtarlar

| Komut | Bayraklar |
|---|---|
| `izci org create` | `--name`, `--slug` |
| `izci org list` | — |
| `izci project create` | `--org`, `--name`, `--slug`, `--platform`; ilk ingest anahtarını da oluşturur. |
| `izci key create` | `--org`, `--project`, `--name`, `--origin` (tarayıcı kökeni, tekrarlanabilir), `--rate-limit`, `--releases` |
| `izci token create` | `--user`, `--name`, `--org`, `--scopes` (`read,write,ai`), `--expires` (`90d`, `12h`, `never`) |
| `izci token list` / `revoke ID` | `--user` |

`token create`, anahtarı standart çıktıya **yalnız başına** yazar; bilgi mesajı standart hataya gider. Böylece anahtar doğrudan bir sır deposuna aktarılabilir:

```bash
izci token create --user bot@kentos.com.tr --name ci --org kentos --scopes read > izci-token.txt
```

## Olaylar ve demo verisi

| Komut | Açıklama |
|---|---|
| `izci event send` | Gerçekçi örnek hata (ya da `--message` ile mesaj) gönderir. `--url`, `--key`, `--count`, `--template`, `--release`, `--protocol` (`connect`, `grpc`, `grpcweb`). |
| `izci bench ingest` | Yük testi; saniyedeki olay ve gecikme yüzdeliklerini raporlar. `--url`, `--key`, `--rate`, `--duration`, `--concurrency`, `--batch`, `--protocol`. |
| `izci demo seed` | Demo projeleri ve geçmiş günlere yayılmış gerçekçi olaylar oluşturur. `--org`, `--events`, `--days`, `--seed`. |

## Yapay zekâ

| Komut | Açıklama |
|---|---|
| `izci ai analyze ISSUE` | Analiz başlatır. `--mode fix|analyze`, `--model`, `--instructions`, `--wait` (logu izler). |
| `izci ai list` | Son koşular. `--issue`, `--status`, `--limit`. |
| `izci ai show RUN` | Sonuç ve rapor; `--diff` önerilen değişikliği de yazar. |
| `izci ai logs RUN` | Koşu günlüğü; `-f` bitene kadar izler. |
| `izci ai branch RUN` | Değişikliği yeni bir dala gönderir (`--name`). |
| `izci ai pr RUN` | Çekme isteği açar (`--draft`, `--title`, `--base`). |
| `izci ai cancel RUN` | Kuyruktaki ya da süren koşuyu durdurur. |
| `izci ai doctor` | opencode, model, git, veri klasörü ve GitHub ayarlarını denetler; modele istek göndermez. |

Birden fazla organizasyon varsa `--org` verin. Koşu kimliği yerine ilk 8 karakteri yeterlidir. Ayrıntılar: [GitHub ve yapay zekâ analizi](yapay-zeka.md).

## MCP

```bash
IZCI_TOKEN=izp_… izci mcp --url https://izci.example.com   # uzak sunucu
IZCI_TOKEN=izp_… izci mcp                                   # yerel veritabanı
```

Ayrıntılar: [MCP ile yapay zekâ asistanları](mcp.md).

## Belgeler

```bash
izci docs llms           # llms.txt: belge dizini (IZCI_PUBLIC_URL bağlantılarıyla)
izci docs llms --full    # llms-full.txt: bütün belgeler tek dosyada
```

Çalışan sunucu aynı içeriği `/llms.txt` ve `/llms-full.txt` adreslerinde sunar.

## Kabuk tamamlama

```bash
izci completion zsh > "${fpath[1]}/_izci"     # bash, fish ve powershell de desteklenir
```
