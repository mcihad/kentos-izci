# Sahiplik ve otomatik atama

Bir hata oluştuğunda ilk soru "bu kimin?" olur. İzci bunu üç yoldan yanıtlar:

- **Sahiplik kuralları:** dosya yolu, modül, adres ya da etikete göre sahip belirten satırlar.
- **CODEOWNERS:** depodaki CODEOWNERS dosyası; ekipler yolların sahiplerini zaten orada tutar.
- **Şüpheli commit:** hatanın oluştuğu satırları en son değiştiren commit ve yazarı.

Yeni hatalar bunlara göre **kendiliğinden atanabilir**. Ayarlar proje başınadır: **Projeler › proje › Sahiplik** (yönetici rolü).

## Sahiplik kuralları

Her satırda bir kural ve ardından sahipleri yazılır; `#` ile başlayan satırlar açıklamadır:

```
# Tahsilat ekibi
path:src/Tahsilat/*          ayse@kentos.com.tr
module:Kentos.Tahsilat.*     @tahsilat-ekibi
url:*/odeme/*                @kentos/odeme
tags.customer:sivas          mehmet@kentos.com.tr
codeowners:/src/Basvuru/     #basvuru
```

| Kural | Neye bakar |
|---|---|
| `path:` | Yığın izindeki çerçevelerin dosya yolları (hem `abs_path` hem `filename`). `/` ya da `*` ile başlamayan desen her klasörde eşleşir: `src/Tahsilat/*`, `/app/src/Tahsilat/OdemeService.cs` ile eşleşir. Windows yolları, `webpack:///` ve `app:///` önekleri düzeltilerek karşılaştırılır. |
| `module:` | Çerçevelerin modülü ya da ad alanı (.NET, Java, Python). |
| `url:` | İsteğin adresinin tamamı; baştan ve sondan `*` kullanın. |
| `tags.<anahtar>:` | Olayın etiketi. Gönderilen etiketlerin yanında `level`, `environment`, `release`, `transaction`, `server_name`, `logger`, `customer` ve `tenant` da kullanılabilir. |
| `codeowners:` | Depodaki yol, [CODEOWNERS kalıbıyla](#codeowners). |

Desenlerde `*` her şeyle (klasör ayıracı dahil), `?` tek karakterle eşleşir; büyük küçük harf fark etmez.

**Sahipler:** e-posta adresi (organizasyonun üyesi olmalı), `@kullanıcı`, `@kuruluş/ekip` ya da `#ekip`. `@` ve `#` ile başlayanlar [sahip eşlemeleriyle](#sahip-eşlemeleri) üyelere bağlanır.

**Sıra:** Kurallar yukarıdan aşağı okunur; eşleşen **son** kural atamayı belirler ve sahiplerinden organizasyon üyesi olan ilk kişiye atanır. CODEOWNERS satırları projenin kurallarından **önce** okunur, yani proje kuralları üstün gelir.

Kaydederken geçersiz satır reddedilir ve hangi satırda neyin yanlış olduğu gösterilir.

## CODEOWNERS

**GitHub'dan içe aktar**, projeye bağlı deponun varsayılan dalında `.github/CODEOWNERS`, `CODEOWNERS` ve `docs/CODEOWNERS` dosyalarına bu sırayla bakar. Bunun için depo, **Projeler › proje › Depo** bölümünde bir GitHub bağlantısıyla bağlanmış olmalıdır (bkz. [GitHub ve yapay zekâ](yapay-zeka.md#1-githubı-bağlayın)). Dosya kendiliğinden eşitlenmez; değiştirdikten sonra **GitHub'dan yenile**'ye basın. GitHub'a bağlı olmayan depolar (GitLab, Azure DevOps) için dosyanın içeriğini **Yapıştır** ile ekleyin.

Kalıplar GitHub'ın kurallarıyla okunur:

| Kalıp | Neyi kapsar |
|---|---|
| `*` | Bütün dosyalar |
| `*.cs` | Her klasördeki `.cs` dosyaları |
| `/src/Tahsilat/` | Depo kökündeki `src/Tahsilat` klasörü ve altındaki her şey |
| `Tahsilat/` | Herhangi bir yerdeki `Tahsilat` klasörü |
| `docs/*` | `docs` klasörünün doğrudan içindekiler (alt klasörler hariç) |
| `**/logs` | Herhangi bir yerdeki `logs` ve altı |
| `/src/Eski` (sahipsiz) | Sahip yazılmayan satır, o yolu sahipsiz bırakır |

GitLab'ın bölüm başlıkları (`[Bölüm]`) atlanır. Geçersiz satırlar atlanır ve sayfada listelenir.

**Yollar nasıl eşleşir:** Yığın izindeki yol, deponun **yol eşlemeleriyle** depo yoluna çevrilir (örneğin `/app/` → boş). Eşleme yoksa yolun sonundan kısalan parçaları denenir: `/app/src/Tahsilat/OdemeService.cs` için `app/src/…`, `src/Tahsilat/…`, `Tahsilat/…`. CODEOWNERS'ta uygulamanın kendi çerçevelerine (in-app) bakılır; kütüphane çerçeveleri yalnız hiç in-app çerçeve yoksa kullanılır.

## Sahip eşlemeleri

`@ad`, `@kuruluş/ekip` ve `#ekip` sahipleri, sayfanın altındaki **Sahip eşlemeleri** bölümünde organizasyon üyelerine bağlanır. Eşlenmemiş sahipler burada listelenir; bir üye seçmek yeter. Eşlemeler organizasyonun **bütün projelerinde** geçerlidir. Bir eşlemede birden çok üye varsa ilk üye atanır.

GitHub kullanıcı adını (`@ayse-kentos`) bir üyeye eşlemek, commit'lerinde başka bir e-posta adresi kullanan kişinin [şüpheli commit](#şüpheli-commit)'lerini de tanıtır.

Üye olmayan e-posta adresleri atanamaz; sayfa bunları ayrıca gösterir.

## Hata sayfasında sahipler

Hata sayfasının yan panelindeki **Sahipler** bölümü, hatanın son olayına bugünkü kurallarla bakar: hangi üyenin hangi kurallarla sahip olduğunu, **önerilen** kişiyi (atamayı belirleyen kuralın seçtiği) ve eşlenmemiş sahipleri gösterir. **Ata** ile hata o kişiye atanır.

## Şüpheli commit

Şüpheli commit, hatanın oluştuğu satırları en son değiştiren commit'tir:

1. Son olayın, satır numarası olan en içteki üç uygulama çerçevesi alınır.
2. Dosyalar depoda bulunur: yol eşlemesi, yolun sonundan parçalar ya da depoda aynı adda tek dosya varsa en uzun ortak sonu olan.
3. Her dosya için GitHub'dan **git blame** istenir: sürüm kaydedilmişse ([Hata gönderme](ingest.md#sürüm-kaydetme) ya da `sentry-cli releases set-commits`) o sürümün commit'inde, yoksa varsayılan dalda.
4. Satırları değiştiren commit'lerden **en yenisi** şüpheli commit olur.

Hata sayfası ilk açıldığında arama kendiliğinden yapılır ve sonuç saklanır; **Yeniden ara** düğmesi en çok dakikada bir GitHub'a sorar. Commit'in yazarı, e-posta adresiyle ya da GitHub kullanıcı adının eşlemesiyle bir üyeye bağlanırsa sayfada **… kişisine ata** düğmesi çıkar.

**Gerekenler:** Depo bir GitHub bağlantısıyla bağlanmış olmalı; bağlantının depoyu okuma izni (Contents: Read) yeterlidir. GitHub Enterprise Server da desteklenir. 100.000'den fazla dosyası olan depolarda dosya listesi eksik gelebilir; yol eşlemesi ekleyin.

Sonuç şunlardan biri olabilir: commit bulundu, olayda satır numaralı çerçeve yok, dosyalar depoda bulunamadı (yol eşlemelerini kontrol edin) ya da GitHub isteği reddetti (bağlantının izinlerini kontrol edin; GitHub'ın mesajı gösterilir).

## Otomatik atama

| Seçenek | Ne olur |
|---|---|
| Kapalı | Yeni hatalar atanmadan kalır (varsayılan). |
| Sahiplik kurallarına göre | Hata ilk kez görüldüğü anda, eşleşen son kuralın ilk üyesine atanır. Atama olaylarla aynı işlemde yapıldığından bildirimlerde (Telegram, e-posta, Slack, Teams, webhook, Jira) **Atanan** satırı görünür. |
| Şüpheli commit'in yazarına | Hata oluştuktan birkaç saniye sonra şüpheli commit aranır; yazarı üyeyse ona, değilse kurallara göre atanır. Bildirim atamadan önce gidebilir. GitHub bağlantılı depo gerekir. |

Yalnız **yeni** hatalar atanır; atanmış bir hata hiçbir zaman başkasına devredilmez, geri dönen (regresyon) hatalar da yeniden atanmaz. Otomatik atamalar hatanın **Etkinlik** sekmesine hangi kural ya da commit yüzünden yapıldığıyla birlikte yazılır.

## API

`izci.api.v1.OwnershipService` bu sayfanın yaptığı her şeyi sunar:

| RPC | Rol | Ne yapar |
|---|---|---|
| `GetOwnership` | izleyici | Kurallar, CODEOWNERS, otomatik atama, eşlenmemiş sahipler ve geçersiz satırlar |
| `UpdateOwnership` | yönetici | Kuralları, CODEOWNERS metnini ya da otomatik atamayı değiştirir; geçersiz kural satırında `line`, `kind` ve `value` bilgisiyle `ERROR_REASON_INVALID_ARGUMENT` döner |
| `ImportCodeowners` | yönetici | CODEOWNERS'ı GitHub'dan alır |
| `ListOwnerAliases`, `SetOwnerAlias` | izleyici, yönetici | Sahip eşlemeleri |
| `GetIssueOwners` | izleyici | Bir hatanın sahipleri ve saklanan şüpheli commit'i |
| `FindSuspectCommit` | izleyici | Şüpheli commit'i arar ve saklar |

Ayrıntılar [Yönetim API başvurusunda](api/izci.api.v1.md).
