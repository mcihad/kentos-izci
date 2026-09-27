# Gruplama

İzci her olay için bir **parmak izi** hesaplar. Aynı parmak izine sahip olaylar aynı **hataya** (issue) toplanır. İyi bir gruplama, aynı kusurun binlerce olayını tek satırda gösterir; farklı kusurları da birbirinden ayırır.

Hata sayfasındaki **Neden bu grupta?** kutusu, parmak izinin hangi bileşenlerden hesaplandığını gösterir.

## Kurallar (gruplama v1)

İlk uyan kural kullanılır:

### 1. Özel parmak izi

Olay `fingerprint` taşıyorsa grubu o belirler:

```json
"fingerprint": ["odeme-saglayici-zaman-asimi"]
```

`{{ default }}` yazılırsa varsayılan bileşenler de eklenir. Böylece varsayılan grup, örneğin müşteriye göre bölünebilir:

```json
"fingerprint": ["{{ default }}", "sivas-belediyesi"]
```

Go SDK'da: `izci.WithFingerprint(...)` ya da `scope.SetFingerprint(...)`.

### 2. Yığın izi

İstisnalarda yığın izi varsa:

- Fırlatılan istisna ve en çok iki nedeni (zincirin son üç halkası) kullanılır.
- Her istisnanın **türü** alınır; SDK'nın ürettiği yapay sarmalayıcıların (`mechanism.synthetic`) türü alınmaz.
- **Uygulama kodu** (`inApp`) çerçeveleri varsa yalnız onlar, yoksa bütün çerçeveler kullanılır. En yeni 30 çerçeve hesaba katılır.
- Her çerçeveden **modül**, **normalize edilmiş fonksiyon adı** ve **normalize edilmiş dosya adı** alınır.
- **Satır numarası kullanılmaz.** Böylece ilgisiz satırlar eklenip silindiğinde grup bozulmaz.
- Fonksiyon adı yoksa (küçültülmüş ya da isimsiz kod) kaynak satırı (`contextLine`), o da yoksa son çare olarak satır numarası kullanılır.

### 3. Yığın izi olmayan istisnalar

İstisna türü ve **normalize edilmiş mesaj** (ilk satır) kullanılır.

### 4. Mesajlar

Mesaj şablonu (`message.template`, ör. `Sipariş {id} bulunamadı`), yoksa normalize edilmiş ilk satır kullanılır. Şablon göndermek, değişken değerli mesajların doğru gruplanmasının en sağlam yoludur.

Parmak izi, bileşenlerin SHA-256 özetinin ilk 128 bitidir. Algoritmanın sürümü özete dahildir; algoritma değişirse eski hatalar kendi parmak izlerini korur.

## Normalizasyon

Derlemeden derlemeye değişen parçalar yer tutucuyla değiştirilir, böylece aynı kusur her dağıtımda aynı gruba düşer:

| Nerede | Neler |
|---|---|
| Mesajlar | Sayılar, UUID'ler, onaltılık değerler, IP adresleri, e-postalar, URL'ler, tarihler. |
| Go | Closure ve sarmalayıcı son ekleri: `.func1`, `.gowrap2`, `-fm`. |
| .NET | Async durum makineleri (`<OnaylaAsync>d__12.MoveNext` → `OnaylaAsync`), lambda ve display sınıfları, `lambda_method12`. |
| Java, Kotlin | `lambda$foo$3`, `$$Lambda$…/0x…`, `access$000`. |
| JavaScript | `async ` öneki, `Object.<anonymous>`, küçültücünün tek harfli adları. |
| Dosya adları | Mutlak yollar ve URL'ler dosya adına indirgenir; paketleyici özetleri çıkarılır (`index-CeX-bC8z.js` → `index.js`). |
| Hepsi | Bellek adresleri ve 3 haneden uzun sayılar. |

## Uygulama kodu (in-app)

Çerçevenin uygulama koduna ait olup olmadığı gruplamayı ve hata sayfasındaki yığın izini doğrudan etkiler. SDK `inApp` gönderirse o değer kullanılır. Göndermezse İzci platforma göre tahmin eder:

- çalışma zamanı ve yaygın kütüphane modülleri (`System.*`, `Microsoft.*`, `Npgsql`, `Polly`, `java.*`, `org.springframework.*`, `dart:`, `package:flutter/`…) ve bağımlılık dizinleri (`node_modules`, `site-packages`, `/go/pkg/mod/`, `.pub-cache`, `.cargo/registry`…) uygulama kodu değildir;
- geri kalanlar uygulama kodudur.

Proje ayarlarının **Gizlilik › Uygulama kodu** bölümünde modül, dosya adı ya da yol önekleri zorla dahil edilebilir ya da hariç tutulabilir. Sıra: SDK'nın bildirdiği değer, sonra proje kuralları, en son platform tahmini.

## Sık sorulanlar

**Aynı kusur birden fazla hataya bölünüyor.** Genellikle mesajdaki değişken bir değer (sipariş numarası vb.) ya da yığın izi olmayan bir istisnadır. Mesaj şablonu gönderin ya da `fingerprint` kullanın.

**Farklı kusurlar aynı hatada toplanıyor.** Genellikle yığın izi gönderilmiyordur ya da çerçeveler uygulama kodu olarak işaretlenmemiştir. SDK ayarlarını ve uygulama kodu kurallarını kontrol edin.

**Sürüm yükseltmeden sonra yeni hatalar açıldı.** Fonksiyon ya da dosya adı değiştiyse bu beklenen bir durumdur: kod değişmiştir. Eski hata sessizleşir, yenisi aynı kusuru temsil eder.
