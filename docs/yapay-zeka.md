# GitHub ve yapay zekâ analizi

İzci bir hatayı yapay zekâya inceletebilir. Ajan (opencode), hatanın oluştuğu sürümün kodunu ve hatanın bağlamını okur, kök nedeni açıklar ve isterseniz en küçük güvenli düzeltmeyi önerir. Önerilen değişikliği inceledikten sonra tek tıkla GitHub'da bir dala gönderebilir ya da çekme isteği açabilirsiniz.

Yapay zekâ **hiçbir şeyi kendiliğinden birleştirmez**. Değişiklik, siz (ya da açıkça ayarladığınız otomasyon) göndermeden depoya dokunmaz.

## 1. GitHub'ı bağlayın

Organizasyon yöneticisi **Organizasyon ayarları › Entegrasyonlar** bölümünden iki yoldan birini seçer.

### Erişim anahtarı (en kolay)

GitHub'da [ince ayarlı bir erişim anahtarı](https://github.com/settings/personal-access-tokens/new) oluşturun:

- **Repository access:** yalnız İzci'nin kullanacağı depolar.
- **Permissions:** Contents: Read and write · Pull requests: Read and write · Metadata: Read.

Anahtarı **Erişim anahtarı ekle** ile girin. İzci anahtarı GitHub'a doğrulatır, `IZCI_SECRET_KEY` ile şifreleyerek saklar ve bir daha göstermez.

### GitHub App (kurumlar için önerilen)

App'in erişimi kişiye değil kuruma bağlıdır ve kısa ömürlü anahtarlar kullanılır. Sunucu yöneticisi App'i bir kez oluşturur:

1. GitHub'da **Settings › Developer settings › GitHub Apps › New GitHub App**.
2. **Callback URL:** `{IZCI_PUBLIC_URL}/integrations/github/callback`. **Request user authorization (OAuth) during installation** kutusunu işaretleyin.
3. **Permissions:** Contents: Read and write · Pull requests: Read and write · Metadata: Read. Webhook gerekmez.
4. App'in kimliğini, kısa adını, OAuth istemci bilgilerini ve özel anahtarını sunucuya verin: `GITHUB_APP_ID`, `GITHUB_APP_SLUG`, `GITHUB_APP_CLIENT_ID`, `GITHUB_APP_CLIENT_SECRET`, `GITHUB_APP_PRIVATE_KEY_FILE` (bkz. [Yapılandırma](yapilandirma.md#github)).

Bundan sonra organizasyon yöneticileri **GitHub App'i kur** düğmesini kullanır. Kurulumdan dönüşte İzci iki şeyi doğrular: kurulumu başlatan kişinin oturumu, ve GitHub'da onaylayan kişinin o kuruluma gerçekten erişebildiği. Bir kurulum yalnız bir organizasyona bağlanabilir.

GitHub Enterprise Server için `GITHUB_API_URL` ve `GITHUB_WEB_URL` değerlerini ayarlayın.

## 2. Projeye depo bağlayın

**Projeler › proje › Depo** bölümünde:

- **GitHub bağlantısı:** yapay zekânın kullanacağı bağlantı. Bağlantısız bir depo da seçilebilir; o durumda yığın izindeki satırlar GitHub'a bağlanır ama yapay zekâ kodu okuyamaz, yalnız hata kaydıyla analiz yapar.
- **Depo:** `sahip/depo` yazın ya da listeden seçin. Varsayılan dal GitHub'dan otomatik alınır.
- **Yol eşlemeleri:** uygulamanın bildirdiği yolu depodaki yola çevirir. Örneğin konteynerde `/app/src/Kalem.Web/…` olan dosya depoda `src/Kalem.Web/…` ise `/app/` → boş.

Aynı bağlantı, CODEOWNERS'ı içe aktarmak ve hatanın [şüpheli commit](sahiplik.md#şüpheli-commit)'ini bulmak için de kullanılır.

CI'da `RegisterRelease` ile sürüm-commit eşleşmesini kaydederseniz yapay zekâ, hatanın oluştuğu **sürümün** kodunu inceler (bkz. [Hata gönderme](ingest.md#sürüm-kaydetme)). Kayıt yoksa varsayılan dalın son hâli kullanılır.

## 3. Sunucuda opencode'u hazırlayın

Sunucu yöneticisi, İzci'yi çalıştıran kullanıcıyla:

```bash
curl -fsSL https://opencode.ai/install | bash   # ya da paket yöneticiniz
opencode auth login                              # model sağlayıcısında oturum açın
```

Ardından:

```bash
AI_ENABLED=true
AI_OPENCODE_MODEL=anthropic/claude-sonnet-5      # isteğe bağlı
```

Kurulumu doğrulayın (modele istek göndermez):

```bash
izci ai doctor
# ok    AI analysis     enabled (AI_ENABLED=true)
# ok    git             git version 2.54.0
# ok    opencode        version 1.18.31
# ok    model           anthropic/claude-sonnet-5
# ok    data directory  /var/lib/izci/ai
# ok    GitHub          tokens and GitHub App izci-kentos
```

## 4. Analiz başlatın

Bir organizasyonun hataları, organizasyon izin verene kadar yapay zekâ sağlayıcısına gönderilmez. Bir yönetici **Organizasyon ayarları › Genel › Yapay zekâ ile analize izin ver** seçeneğini açar (bkz. [Sağlayıcıya giden veri](#sağlayıcıya-giden-veri-kvkk)).

- **Arayüz:** hata sayfası › **Yapay zekâ** › **Analizi başlat**. Kip seçin: *kök nedeni bul ve düzeltme öner* (depo bağlıysa) ya da *yalnız analiz et*. İsterseniz ajana ek bilgi verin.
- **Telegram:** bildirimdeki **🤖 Yapay zekâ ile analiz et** düğmesi.
- **MCP:** `start_ai_analysis` aracı.
- **Komut satırı:** `izci ai analyze KALEM-1A --wait`.

Analiz sırasında log canlı akar: deponun hazırlanması, ajanın okuduğu ve aradığı dosyalar, yaptığı düzenlemeler ve açıklamaları. Sonuçta şunlar görünür:

- **Özet**, **kök neden**, **güven** (0–100) ve **risk** (düşük, orta, yüksek);
- **Analiz**: geliştiriciler için Türkçe rapor; ne oluyor, neden (dosya:satır), düzeltme, nasıl doğrulanır;
- **Değişiklik**: renklendirilmiş diff ve değişen dosyalar;
- **Günlük**: bütün adımlar ve kullanılan token/maliyet.

## 5. Değişikliği gönderin

İnceledikten sonra **Taslak çekme isteği aç**, **İncelemeye hazır çekme isteği aç** ya da **Dal oluştur**:

- Commit, `AI_GIT_AUTHOR_NAME` adıyla atılır ve **tam olarak ekranda gördüğünüz diff'i** içerir.
- Dal adı `izci/fix/<kısa-kimlik>-<başlık>-<koşu>` biçimindedir; istenirse değiştirilebilir.
- Hiçbir zaman zorla (`--force`) push yapılmaz. Aynı adlı dal başka commit'ler içeriyorsa işlem durur.
- Çekme isteğinin açıklaması Türkçe analizi, hatanın bağlantısını ve etkisini (olay, kullanıcı, müşteri) içerir.
- Hata sayfasının **Etkinlik** akışına dal ve çekme isteği yazılır.
- Değişiklik CI'ın, derleme araçlarının ya da git'in çalıştırdığı dosyalara dokunuyorsa (`.github/`, `.gitlab-ci.yml`, `Jenkinsfile`, `Makefile`, `Dockerfile`, `package.json` ve kilit dosyaları, `go.mod`, `*.csproj`, kabuk betikleri, `.gitmodules`, `.env` …) bu dosyalar ayrıca listelenir ve **"Bu dosyalardaki değişiklikleri satır satır inceledim"** onayı verilmeden gönderilemez. Hata kayıtları dışarıdan gelir ve yapay zekâyı yanıltmaya çalışan metin içerebilir; böyle bir değişiklik dal derlenir derlenmez deponun sırlarıyla çalışır. API'de bu onay `allow_sensitive_changes`, komut satırında `--allow-sensitive`'dir; MCP üzerinden bu tür değişiklikler gönderilemez.

## Otomasyon

**Projeler › proje › Yapay zekâ** bölümünde:

| Ayar | Açıklama |
|---|---|
| Otomatik analiz | Yeni hatalarda analizi kendiliğinden başlatır. Varsayılan: kapalı. |
| En düşük seviye | Bu seviyenin altındaki yeni hatalar analiz edilmez. |
| Analizden sonra | Yalnız analiz, düzeltmeyi bir dala gönder ya da taslak çekme isteği aç. Otomatik değişiklikler `izci-auto/` ile başlayan dallara gider, çekme istekleri hep taslaktır ve CI ya da derleme dosyalarına dokunan değişiklikler otomatik gönderilmez (koşunun günlüğünde nedeni yazar; inceleyip el ile gönderebilirsiniz). |
| Günlük analiz sınırı | Projenin günde başlatabileceği otomatik analiz; 0 ise otomatik analiz başlamaz. |
| Model | Projeye özel model; boşsa sunucu varsayılanı. |
| Çalıştırılabilecek komutlar | Ajanın düzeltmeyi doğrulamak için çalıştırabileceği komutlar (ör. `dotnet test`). Yalnız sunucunun izin verdikleri (`AI_ALLOWED_COMMANDS`) ya da onlara argüman eklenmiş hâlleri seçilebilir. Aşağıdaki uyarıya bakın. |
| Proje talimatları | Her analize eklenen kurallar: kod stili, dokunulmaması gereken yerler… |

Organizasyonun aylık analiz sınırı (plan) aşıldığında yeni analizler başlamaz.

**Otomatik gönderimde CI.** Otomatik analiz, uygulamanın dışından gelen bir hata kaydıyla başlar; bu kayıt yapay zekâyı yanıltmaya çalışan metin içerebilir. Otomatik dallar bu yüzden `izci-auto/` ile başlar: CI'ınızı bu dallarda sır kullanmayacak şekilde ayarlayın. GitHub Actions'ta örneğin:

```yaml
on:
  push:
    branches-ignore: ["izci-auto/**"]
```

Çekme isteklerinde çalışan işlerde sırları, `github.head_ref` `izci-auto/` ile başlıyorsa vermeyin ya da onay isteyen bir ortam (environment) kullanın.

## Sağlayıcıya giden veri (KVKK)

Analiz, hata kayıtlarını ve kodu model sağlayıcısına gönderir; sağlayıcı çoğu zaman yurt dışındadır. Bu yüzden karar organizasyonundur:

- **İzin organizasyon ayarıdır.** **Organizasyon ayarları › Genel › Yapay zekâ ile analize izin ver** varsayılan olarak kapalıdır. Kapalıyken arayüz, otomatik analiz, Telegram düğmesi, MCP ve komut satırı analiz başlatamaz (API `ERROR_REASON_AI_NOT_ALLOWED` döner); izin sıradaki bir analiz başlamadan geri çekilirse o analiz de çalışmaz. İzni kimin, ne zaman değiştirdiği denetim kaydına yazılır. Bu ayardan önce analiz çalıştırmış ya da otomatik analizi açmış organizasyonlarda izin açık başlar.
- **Giden:** ajana verilen talimatlar; hatanın özeti (mesaj, yığın izi ve kod satırları, hatadan önceki adımlar, istek yolu ve gövdesi, çalışma ortamı, etiketler); olay kaydı; depo bağlıysa ajanın okuduğu kod dosyaları.
- **Gitmeyen:** kullanıcının e-posta adresi, adı, IP adresi, çerezler, yetki ve yönlendirme başlıkları. Olaylar zaten [maskelenerek](guvenlik.md#maskeleme) saklanır.
- **Kayıt:** her analizin **Gönderilen bağlam** sekmesi, sağlayıcıya kod dışında tam olarak ne gönderildiğini gösterir; ajanın okuduğu dosyalar **Günlük** sekmesindedir.

### Kurum içi model

Verinin kurum dışına çıkmasını istemiyorsanız opencode'u kendi sunucunuzda çalışan bir modele bağlayın. Örneğin [Ollama](https://ollama.com)'nın OpenAI uyumlu ucuyla bir ayar dosyası:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama",
      "options": { "baseURL": "http://127.0.0.1:11434/v1" },
      "models": { "qwen2.5-coder:32b": { "name": "Qwen2.5 Coder 32B" } }
    }
  }
}
```

```bash
AI_OPENCODE_CONFIG=/etc/izci/opencode.json
AI_OPENCODE_MODEL=ollama/qwen2.5-coder:32b
```

Model araç çağırmayı (tool calling) desteklemelidir; analizin kalitesi modele bağlıdır. Projeler kendi ayarlarında başka bir model seçebilir. `izci ai doctor` ayar dosyasını ve modeli denetler.

## Güvenlik

Ajan, son kullanıcıların etkileyebildiği hata verileriyle (mesajlar, istek gövdeleri) çalışır. İzci bu yüzden ajanı sıkı sınırlar içinde çalıştırır:

- **Kabuk yok.** Varsayılan olarak ajan hiçbir komut çalıştıramaz; kodu yalnızca dosya araçlarıyla okur ve düzenler. Web erişimi, çalışma dizini dışındaki dosyalar, dil sunucuları ve biçimlendiriciler kapalıdır.
- **Sır yok.** Ajanın ortamında İzci'nin veritabanı adresi, ana anahtarı, bot anahtarı ya da GitHub anahtarı bulunmaz. Git işlemlerini ajan değil, İzci yapar.
- **Depo ayarları yok sayılır.** Depodaki `opencode.json`, eklentiler ve `.opencode` klasörü ajana yüklenmez. Sunucu kullanıcısının kişisel opencode ayarları da kullanılmaz.
- **Sonuçlar güvenle okunur.** Ajanın klasörü, ajan çalıştıktan sonra güvenilmez sayılır. Sembolik bağlantılar klasör dışına çıkamaz; git hiçbir zaman ajanın yazabileceği bir yapılandırmayı okumaz.
- **Kişisel veri gönderilmez.** Olaydaki e-posta, IP, ad, çerez ve yönlendirme başlıkları model sağlayıcısına gitmeden çıkarılır.

**Çalıştırılabilecek komutlar kod çalıştırma yetkisidir.** `dotnet test` ya da `npm test`, deponun kodunu İzci sunucusunda çalıştırır; ajan da bu kodu değiştirebilir ve hata kayıtlarındaki metin onu buna yönlendirmeye çalışabilir. Bu yüzden:

- Hangi komutların çalışabileceğine sunucu yöneticisi karar verir (`AI_ALLOWED_COMMANDS`, varsayılan: hiçbiri). Proje yöneticileri yalnız bu listeden seçer; listede olmayan bir komutu kaydetmek reddedilir, eski ayarlarda kalanlar çalıştırılmaz ve koşunun günlüğünde yazılır.
- Linux'ta `AI_SANDBOX=bwrap` ile ajan [bubblewrap](https://github.com/containers/bubblewrap) yalıtımında çalışır: kendi süreç ad alanı, salt okunur sistem klasörleri, özel `/tmp`; yalnız koşunun klasörü ve projenin deposu yazılabilir. Başka projelerin depoları, İzci'nin veri klasörü, `.env` dosyası ve süreci görünmez. Model sağlayıcısına ulaşmak için ağ açık kalır. Konteynerde bubblewrap'ın ad alanı açabilmesi gerekir (ör. Docker'da `--security-opt seccomp=unconfined --security-opt apparmor=unconfined`); `izci ai doctor` yalıtımın çalıştığını denetler.
- Linux'ta İzci süreci, aynı kullanıcıyla çalışan başka süreçlerin ortam değişkenlerini ve belleğini okumasını engeller (`/proc/<pid>/environ`).
- Yalıtım yoksa komut izni vermeyin ya da analizleri ayrı bir sunucuda çalıştırın (bkz. [İşletim](operasyon.md)).

Analizler model sağlayıcısında maliyet oluşturur. Her koşunun token ve maliyet bilgisi **Günlük** sekmesinde, toplamlar **Yapay zekâ** sayfasındadır.

## Komut satırı

```bash
izci ai analyze KALEM-1A --mode fix --wait     # başlat ve logu izle
izci ai list --status failed                   # son koşular
izci ai show 01a0e0b4 --diff                     # sonuç, rapor ve diff
izci ai logs 01a0e0b4 -f                         # canlı log
izci ai pr 01a0e0b4                              # taslak çekme isteği
izci ai branch 01a0e0b4 --name izci/fix/odeme    # yalnız dal
izci ai cancel 01a0e0b4
izci ai doctor
```

Koşu kimliğinin ilk 8 karakteri yeterlidir. Koşuları çalışan `izci serve` yürütür; komut satırı onları kuyruğa ekler, izler ve yayımlar.

## Sorun giderme

**"Yapay zekâ analizi kapalı."** `AI_ENABLED=true` verip sunucuyu yeniden başlatın; `izci ai doctor` eksikleri gösterir.

**Analiz "prepare repository" hatasıyla bitiyor.** Bağlantının depoya erişimini (anahtarın izinleri ya da App'in kurulduğu depolar) ve deponun `https` adresini kontrol edin.

**Değişiklik önerilmiyor.** Proje depoyla bağlı değilse ya da kip *yalnız analiz* ise ajan kod değiştirmez. Depo bağlıyken de ajan güvenli bir düzeltme bulamadığını raporlayabilir.

**Çekme isteği açılamıyor: "branch already exists".** Aynı adlı dal başka commit'lerle var. Farklı bir dal adı verin ya da dalı GitHub'da silin.
