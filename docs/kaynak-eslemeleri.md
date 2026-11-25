# Kaynak eşlemeleri ve semboller

Web uygulamaları yayına küçültülmüş (minified) JavaScript ile çıkar; Flutter uygulamaları `--obfuscate` ile karıştırılır ve hata ayıklama bilgisi uygulamadan ayrılır (`--split-debug-info`). Bu uygulamalardan gelen yığın izleri okunamaz: `Pc (index-BTpphgj6.js:15:29179)` ya da yalnız bellek adresleri.

İzci, yüklediğiniz **kaynak eşlemeleri** (source map) ve **sembol dosyalarıyla** bu yığın izlerini özgün dosya, satır ve fonksiyon adlarına çevirir: `totalWithTax (src/invoice.ts:15)`. Çeviri, olay kaydedilmeden ve gruplanmadan önce yapılır; böylece aynı hata her sürümde aynı kayda düşer.

Yükleme için Sentry'nin araçları olduğu gibi kullanılır: `@sentry/vite-plugin`, `@sentry/webpack-plugin`, `sentry-cli` ve Flutter için `sentry_dart_plugin`. Bunlar yalnız İzci'nin adresine yönlendirilir.

## Nasıl çalışır

**Web (JavaScript):** Her yığın çerçevesinin betik adresi ve satır/sütunu, betiğin kaynak eşlemesinde aranır. Eşleme iki yoldan bulunur:

1. **Debug kimliği** (önerilen): Derleme eklentileri ve `sentry-cli sourcemaps inject` her betiğe ve eşlemesine aynı kimliği yazar. SDK, hata olduğunda betiğin kimliğini olayla gönderir (`debug_meta`). Sürüm adı gerekmez; betik hangi adresten sunulursa sunulsun eşleşir.
2. **Sürüm ve adres:** Olayın `release` değeriyle yüklenmiş dosyalar arasında betiğin adresi aranır. Adres alan adı olmadan karşılaştırılır: `https://kalem.example.com/assets/app.js` betiği `~/assets/app.js` olarak yüklenen dosyayla eşleşir.

Çevrilen çerçevede dosya, satır, sütun ve eşlemenin içerdiği kaynak koddan satırlar gösterilir. Fonksiyon adı, çağıran çerçevedeki çağrının özgün adından alınır. `node_modules` altındaki ve paketleyicinin üçüncü taraf olarak işaretlediği dosyalar kütüphane kodu sayılır.

**Flutter ve Dart:** Karıştırılmış derlemelerin yığın izi yalnız adres taşır. Sentry Dart SDK'sı olayla birlikte uygulama görüntüsünün derleme kimliğini ve yüklendiği adresi gönderir. İzci adresleri, aynı derlemenin sembol dosyasındaki DWARF bilgisiyle fonksiyon, dosya ve satıra çevirir; satır içine alınmış (inline) çağrılar ayrı çerçeveler olarak görünür. Karıştırma haritası (`--save-obfuscation-map`) yüklendiyse karıştırılmış istisna türleri de düzelir: `Ki` → `InvoiceException`.

| Platform | Sembol dosyası | Biçim |
|---|---|---|
| Android | `app.android-arm64.symbols` (ve diğer mimariler) | ELF |
| iOS, macOS | `app.ios-arm64.symbols`, `app.darwin-arm64.symbols` | Mach-O (dSYM) |
| Hepsi | `obfuscation.map.json` | Dart karıştırma haritası |

## Anahtar

Yükleme için şunlardan biri kullanılır:

- **Sürüm ve dosya yükleme** izni olan bir proje API anahtarı (`izk_…`). **Projeler › proje › Anahtarlar › Yeni anahtar** penceresinde "CI hattından sürüm kaydına, kaynak eşlemesi ve sembol yüklemeye de izin ver" seçeneğini işaretleyin. Anahtar yalnız kendi projesine yükleyebilir.
- `write` kapsamlı bir kişisel erişim anahtarı (`izp_…`); sahibinin organizasyonda en az üye rolü olmalıdır. Birden çok projeye yükleyen hatlar için uygundur.

Bu anahtarı yalnız CI'da, gizli değişken olarak kullanın. Tarayıcıya ya da mobil uygulamaya konan anahtar herkesçe okunabilir; ona bu izni vermeyin, uygulamalar için ayrı bir anahtar oluşturun. Projeyle birlikte oluşan ilk anahtar yalnız olay gönderir. Yükleme izinli bir anahtarla bir tarayıcıdan olay gelirse (`Origin` başlıklı istek), olaylar kabul edilmeye devam eder ama anahtarın sürüm kaydı ve dosya yükleme izni kalıcı olarak kapanır; **Anahtarlar** sayfası bunu gösterir.

`sentry-cli` İzci anahtarları için "Unrecognized auth token format" uyarısı yazar; zararsızdır.

## Web uygulamaları

Önce kaynak eşlemesi üretimini açın. `hidden` eşlemeler üretilir ama betiklerde adları geçmez; böylece tarayıcılar indirmez, kaynak kodunuz yayında görünmez.

### Vite

```sh
npm install --save-dev @sentry/vite-plugin
```

```typescript
// vite.config.ts
import { sentryVitePlugin } from "@sentry/vite-plugin";
import { defineConfig } from "vite";

export default defineConfig({
  build: { sourcemap: "hidden" },
  plugins: [
    sentryVitePlugin({
      url: "https://izci.example.com",
      org: "kentos",
      project: "kalem-web",
      authToken: process.env.SENTRY_AUTH_TOKEN,
      // Yüklenen eşlemeler yayına çıkmasın:
      sourcemaps: { filesToDeleteAfterUpload: ["dist/**/*.map"] },
    }),
  ],
});
```

Eklenti her derlemede betiklere debug kimliği ekler, eşlemeleri yükler ve bir sürüm oluşturur. Sürüm adı verilmezse git commit'inden türetilir; `release: { name: "kalem-web@5.1.0" }` ile belirleyebilirsiniz. Uygulamadaki Sentry SDK'sı sürümü eklentinin enjekte ettiği değerden alır.

### webpack

```javascript
// webpack.config.js
const { sentryWebpackPlugin } = require("@sentry/webpack-plugin");

module.exports = {
  devtool: "hidden-source-map",
  plugins: [
    sentryWebpackPlugin({
      url: "https://izci.example.com",
      org: "kentos",
      project: "kalem-web",
      authToken: process.env.SENTRY_AUTH_TOKEN,
    }),
  ],
};
```

Rollup (`@sentry/rollup-plugin`) ve esbuild (`@sentry/esbuild-plugin`) aynı ayarları alır.

### sentry-cli

Derleme eklentisi kullanmayan projelerde, derlemeden sonra ve yayından önce:

```sh
export SENTRY_URL=https://izci.example.com
export SENTRY_AUTH_TOKEN=izk_…
sentry-cli sourcemaps inject ./dist
sentry-cli sourcemaps upload --org kentos --project kalem-web ./dist
```

`inject` betiklerin ve eşlemelerin içine debug kimliğini yazar; yayına bu dosyalar çıkmalıdır. Debug kimliği kullanmadan sürüme göre yüklemek için `inject` adımını atlayıp `upload` komutuna `--release kalem-web@5.1.0` verin; betikler sunucuda `/assets/` altındaysa `--url-prefix '~/assets'` ekleyin. Bu yolda uygulamanın SDK'sı da aynı `release` değerini göndermelidir.

`sentry-cli` 2.17 ve üstü gerekir (artifact bundle yüklemesi).

## Flutter ve Dart

Uygulamayı karıştırarak ve hata ayıklama bilgisini ayırarak derleyin:

```sh
flutter build apk --obfuscate --split-debug-info=build/debug-info \
  --extra-gen-snapshot-options=--save-obfuscation-map=build/app/obfuscation.map.json
```

### sentry_dart_plugin

```yaml
# pubspec.yaml
dev_dependencies:
  sentry_dart_plugin: ^3.4.0

sentry:
  url: https://izci.example.com
  org: kentos
  project: kalem-mobil
  upload_debug_symbols: true
  symbols_path: build/debug-info
  dart_symbol_map_path: build/app/obfuscation.map.json
```

```sh
SENTRY_AUTH_TOKEN=izk_… dart run sentry_dart_plugin
```

Eklenti sembol dosyalarını ve karıştırma haritasını yükler, sürümü oluşturup tamamlar. Commit ilişkilendirmesi istemiyorsanız `commits: false` ekleyin.

### sentry-cli

```sh
export SENTRY_URL=https://izci.example.com SENTRY_AUTH_TOKEN=izk_…
sentry-cli debug-files upload --org kentos --project kalem-mobil build/debug-info
sentry-cli dart-symbol-map upload --org kentos --project kalem-mobil \
  build/app/obfuscation.map.json build/debug-info/app.android-arm64.symbols
```

Karıştırma haritası, ait olduğu derlemenin sembol dosyasıyla birlikte verilir; İzci onu o dosyanın debug kimliğiyle saklar. Her mimarinin sembol dosyası ve debug kimliği ayrıdır: haritayı her mimarinin sembol dosyasıyla ayrı ayrı yükleyin.

Her derlemenin sembolleri o derlemeye aittir. Yayınlanan her sürüm için yükleyin; yeniden derlenen bir uygulamanın sembolleri öncekiyle eşleşmez.

## Sürümler ve dağıtımlar

`sentry-cli releases new`, `releases finalize`, `releases set-commits` ve `deploys new` komutları İzci'nin **Projeler › proje › Sürümler** listesine yazar. `set-commits --auto`, organizasyonun projelerine bağlı GitHub depolarını görür; commit bilgisi yapay zekâ analizinde sürümün kodunu bulmak için kullanılır.

## Yüklenenleri yönetme

**Projeler › proje › Kaynak eşlemeleri** sekmesi yüklenen paketleri (sürüm, dosyalar, boyut, son kullanım) ve sembol dosyalarını listeler. Yöneticiler silebilir; silme yalnız bundan sonra gelen olayları etkiler, kayıtlı olaylar değişmez.

Dosyalar veritabanında saklanır ve sıkıştırılır. Bir yükleme (paket ya da sembol dosyası) ve paketteki her dosya en çok `INGEST_UPLOAD_MAX_MB` (varsayılan 128 MiB), bir paketin açılmış içeriği en çok 1 GiB (ya da bu sınırın iki katı, hangisi büyükse), bir projenin toplamı en çok 10 GiB olabilir. Yığın izi çözmeyen dosyalar, yüklendikten ya da son kullanıldıktan 90 gün sonra (`RETENTION_DAYS` daha uzunsa o kadar) silinir. Aynı dosya ikinci kez yüklenirse yeniden saklanmaz.

## Hata sayfasında

Çevrilen yığın izlerinin üstünde "Yığın izi yüklenen kaynak eşlemeleri ve sembol dosyalarıyla özgün koda çevrildi" notu görünür. **Ham yığın izi** anahtarı, SDK'nın gönderdiği küçültülmüş ya da adresli hâli gösterir.

Bir betiğin eşlemesi ya da bir derlemenin sembol dosyası bulunamazsa olayın başında nedeniyle birlikte uyarı çıkar. Uyarılar yalnız projeye en az bir kez yükleme yapıldıysa gösterilir; hiç yükleme yapmayan projeler etkilenmez.

## Sorun giderme

**"Kaynak eşlemesi bulunamadı":**
- Debug kimliğiyle: yayındaki betik, yüklenenle aynı derlemeden mi? Eşlemeler yüklendikten sonra yeniden derleme yapıldıysa kimlikler değişir.
- Sürümle: olaydaki `release` ile yüklemedeki sürüm aynı olmalı. Betiğin adresi yüklenen adla eşleşmeli; sentry-cli çıktısındaki `~/…` adlarını olaydaki betik adresiyle karşılaştırın, gerekirse `--url-prefix` kullanın.

**Satırlar tutmuyor:** Yüklenen eşleme yayındaki betiğe ait değildir. Eşlemeyi yayına çıkan dosyalardan, derlemeden hemen sonra yükleyin.

**Flutter'da "sembol dosyası yüklenmemiş":** Uyarıdaki debug kimliği hangi derlemeye aitse onun `--split-debug-info` klasörünü yükleyin. `sentry-cli debug-files check dosya.symbols` bir dosyanın debug kimliğini gösterir.

**İstisna türü hâlâ karıştırılmış (`Ki`):** Karıştırma haritası o derlemenin debug kimliğiyle yüklenmemiştir; `dart-symbol-map upload` komutuna aynı derlemenin sembol dosyasını verin.

## Uç noktalar

İzci, Sentry web API'sinin yükleme ve sürüm için gereken kısmını sunar:

| Uç nokta | Ne için |
|---|---|
| `GET /api/0/` | `sentry-cli info` |
| `GET`, `POST /api/0/organizations/{org}/chunk-upload/` | Yükleme ayarları ve dosya parçaları |
| `POST /api/0/organizations/{org}/artifactbundle/assemble/` | Kaynak eşleme paketi |
| `POST /api/0/projects/{org}/{proje}/files/difs/assemble/` | Sembol dosyası ve karıştırma haritası |
| `POST /api/0/projects/{org}/{proje}/releases/`, `POST /api/0/organizations/{org}/releases/` | Sürüm oluşturma |
| `GET`, `PUT /api/0/…/releases/{sürüm}/` | Sürüm bilgisi, tamamlama, commit'ler |
| `POST /api/0/organizations/{org}/releases/{sürüm}/deploys/` | Dağıtım kaydı |
| `GET /api/0/organizations/{org}/repos/` | `set-commits --auto` için depolar |

Diğer Sentry web API uçları (hatalar, olaylar, projeler) yoktur.

## Sınırlamalar

- .NET PDB, Android ProGuard/R8 eşlemeleri, Windows ve Breakpad sembolleri, minidump ve React Native Hermes eşlemeleri işlenmez.
- İzci ELF ve Mach-O dosyalarındaki DWARF bilgisini okur; Flutter ve Dart derlemeleriyle denenmiştir.
- `sentry-cli` 1.x'in sürüme bağlı eski dosya yüklemesi desteklenmez.
