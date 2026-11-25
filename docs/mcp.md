# MCP ile yapay zekâ asistanları

İzci bir [Model Context Protocol](https://modelcontextprotocol.io) sunucusudur. Claude Code, opencode, Cursor ve MCP destekleyen diğer asistanlar İzci'deki hataları arayabilir, olayları okuyabilir, hataları triage edebilir ve yapay zekâ analizi başlatabilir. Örneğin editörünüzde "Sivas Belediyesi'ni en çok etkileyen hatayı bul ve düzelt" demek yeter.

## Erişim anahtarı

Asistan sizin adınıza **kişisel erişim anahtarıyla** (`izp_…`) çalışır. **Hesabım › Erişim anahtarları › Anahtar oluştur** ile oluşturun ve yalnız gereken kapsamları verin (son 10 dakika içinde giriş yapmadıysanız önce kimliğinizi yeniden doğrulamanız istenir):

| Kapsam | İzin verdiği araçlar |
|---|---|
| `read` | Arama ve okuma araçlarının hepsi. |
| `write` | `update_issue`, `add_comment`. |
| `ai` | `start_ai_analysis`, `create_fix_pull_request`. |

Anahtar tek bir organizasyonla sınırlanabilir ve süreli olmalıdır. Asistan hiçbir zaman sizin rolünüzden fazlasını yapamaz: bir `viewer` anahtarı `write` kapsamı taşısa bile hata durumunu değiştiremez.

Sunucu yöneticisi, botlar için komut satırından da anahtar üretebilir: `izci token create --user … --scopes read` (bkz. [Komut satırı](cli.md)).

## Bağlantı

MCP adresi `{IZCI_PUBLIC_URL}/mcp` adresidir (streamable HTTP). Anahtar oluşturma penceresi, aşağıdaki komutları anahtarınızla birlikte hazır verir.

**Claude Code**

```bash
claude mcp add --transport http izci https://izci.example.com/mcp \
  --header "Authorization: Bearer izp_…"
```

**opencode** (`opencode.json`)

```json
{
  "mcp": {
    "izci": {
      "type": "remote",
      "url": "https://izci.example.com/mcp",
      "headers": { "Authorization": "Bearer izp_…" }
    }
  }
}
```

**Cursor** (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "izci": {
      "url": "https://izci.example.com/mcp",
      "headers": { "Authorization": "Bearer izp_…" }
    }
  }
}
```

**Yerel süreç (stdio).** Asistan MCP sunucularını yalnız süreç olarak başlatabiliyorsa `izci` binary'sini kullanın. Komut, araçları `--url` ile verilen sunucuya HTTPS üzerinden iletir:

```bash
claude mcp add izci --env IZCI_TOKEN=izp_… -- izci mcp --url https://izci.example.com
```

Birden fazla organizasyonunuz varsa varsayılanı `Izci-Org: <kısa-ad>` başlığıyla (stdio'da `--org` ile) seçin; her araç ayrıca `organization` parametresi alır.

## Araçlar

| Araç | Ne yapar | Kapsam |
|---|---|---|
| `list_organizations` | Erişilebilen organizasyonlar ve rolünüz. | read |
| `list_projects` | Projeler, açık hata sayıları, son 24 saatteki olaylar, bağlı depo. | read |
| `get_project_setup` | Olay gönderme adresleri ve Sentry DSN şablonu. | read |
| `search_issues` | [Arama dili](arama.md) ile hata arama; sıralama, dönem, sayfalama. | read |
| `get_issue` | Hata ayrıntısı, etiket dağılımları, çekme istekleri, [sahipler ve şüpheli commit](sahiplik.md) ve en bilgilendirici olayın yığın izi. | read |
| `list_issue_events` | Bir hatanın olayları (filtrelenebilir). | read |
| `get_event` | Olay ayrıntısı; `raw=true` saklanan JSON'u döner. | read |
| `get_issue_stats` | Olay sayılarının zaman içindeki dağılımı. | read |
| `list_customers` | Müşteriler ve sağlık durumu. | read |
| `get_customer_overview` | Bir müşterinin projeleri ve onu en çok etkileyen hatalar. | read |
| `list_releases` | Sürümler, commit'ler ve getirdikleri yeni hatalar. | read |
| `list_monitors` | [Zamanlanmış görevler](cron.md): zamanlama, ortam başına son çalışmanın durumu, açık hata kaydı; `failing` ile yalnız sorunlular. | read |
| `get_ai_run` | Yapay zekâ analizinin sonucu, raporu ve diff'i. | read |
| `update_issue` | Durum, atanan kişi ve öncelik değiştirme. | write |
| `add_comment` | Hataya yorum ekleme. | write |
| `start_ai_analysis` | Yapay zekâ analizini başlatma. | ai |
| `create_fix_pull_request` | Önerilen düzeltmeyi çekme isteği olarak açma (varsayılan: taslak). | ai |

Hatalar kısa kimlikle (`KALEM-1A`) ya da UUID ile verilir. Sonuçlar arayüz bağlantıları içerir.

## Kaynaklar ve istemler

Kaynaklar: `izci://issues/{kısa-kimlik}`, `izci://events/{kimlik}`, `izci://docs/{rehber}` (bu belgeler).

İstemler (asistanınızın komut menüsünde görünür):

- **triage_issue**: bir hatanın etkisini değerlendirip durum, öncelik ve sahip önerir; onaylarsanız uygular.
- **investigate_issue**: olayları, sürümleri ve kodu karşılaştırarak kök nedeni arar.
- **customer_health_report**: müşterilerin durumunu özetleyen rapor.

## Güvenlik

- `/mcp` yalnızca `Authorization: Bearer izp_…` kabul eder; tarayıcı oturum çerezleri yok sayılır.
- Her araç yönetim API'sini aynı anahtarla çağırır. Rol, kapsam ve organizasyon sınırları arayüzdekiyle birebir aynıdır.
- Hata verileri son kullanıcı metni içerebilir. İzci'nin araç açıklamaları asistana bu metinleri talimat olarak değil veri olarak ele almasını söyler; yine de yazma yetkili anahtarları gerektiğinde verin.
- `MCP_ENABLED=false` uç noktayı kapatır.
