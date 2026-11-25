# Gruplama

İzci her olay için bir **parmak izi** hesaplar. Aynı parmak izine sahip olaylar aynı **hataya** (issue) toplanır. İyi bir gruplama, aynı kusurun binlerce olayını tek satırda gösterir; farklı kusurları da birbirinden ayırır.

Hata sayfasındaki **Neden bu grupta?** kutusu, parmak izinin hangi bileşenlerden hesaplandığını gösterir.

## Kurallar (gruplama v1)

İlk uyan kural kullanılır. Projenin [parmak izi kuralları](#proje-kuralları) hepsinden önce gelir.

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

## Proje kuralları

**Proje ayarları › Gruplama** bölümünde yöneticiler iki tür kural yazar. Kurallar yalnız **yeni olaylara** uygulanır; mevcut hatalar parmak izlerini korur. Yeni kuralla açılan bir hatayı eskisiyle [birleştirebilirsiniz](#birleştirme-ve-ayırma). Kaydetmeden önce **Dene** ile kuralları bir hatanın son olayına uygulayıp hangi parmak izini alacağını ve hangi hataya düşeceğini görebilirsiniz; hatalı satırlar satır numarasıyla gösterilir ve kaydedilmez.

Değerler büyük/küçük harf ayırmaz; `*` her şeyle, `?` tek karakterle eşleşir. Boşluk içeren değerler çift tırnağa alınır. Başına `!` konan eşleştirici tersine döner. Bir satırdaki bütün eşleştiriciler uymalıdır; `#` ile başlayan satırlar açıklamadır.

### Parmak izi kuralları

Her satır `eşleştiriciler -> parmak izi` biçimindedir. **Eşleşen ilk kural**, uygulamanın gönderdiği `fingerprint` dahil her şeyden önce gelir:

```text
error.type:*TimeoutException transaction:/api/odeme* -> odeme-zaman-asimi
error.value:"*bağlantı havuzu*" -> baglanti-havuzu
message:"Rapor * oluşturulamadı" -> rapor-olusturma
tags.modul:imar !level:warning -> {{ default }}, imar
logger:Kalem.Mail* -> mail, {{ error.type }}
```

| Eşleştirici | Neye bakar |
|---|---|
| `error.type`, `error.value` | Zincirdeki herhangi bir istisnanın türü ya da mesajı |
| `message` | Günlük mesajı (biçimlenmiş hâli ya da şablonu) |
| `logger`, `level`, `transaction`, `environment`, `release` | Olayın aynı adlı alanları (`level`: `error`, `warning`…) |
| `tags.<ad>` | Bir etiketin değeri |
| `stack.module`, `stack.function`, `stack.filename`, `stack.package` | Yığın izindeki herhangi bir çerçeve |

Parmak izi virgülle ayrılmış parçalardan oluşur. Parçalarda değişkenler kullanılabilir: `{{ default }}` (varsayılan gruplamanın bileşenleri; tek başına bir parça olmalı), `{{ error.type }}`, `{{ error.value }}` ve `{{ message }}` (normalize edilmiş ilk satır), `{{ transaction }}`, `{{ level }}`, `{{ logger }}`, `{{ environment }}`, `{{ release }}`, `{{ stack.function }}` ve `{{ stack.module }}` (en yeni uygulama çerçevesi), `{{ tags.<ad> }}`. Değeri olmayan değişken `<none>` olur. Hata sayfasındaki **Neden bu grupta?** kutusu eşleşen kuralı gösterir.

### Yığın izi kuralları

Her satır çerçeve eşleştiricileri (`stack.module`, `stack.function`, `stack.filename`, `stack.package`) ve eylemlerden oluşur:

```text
stack.function:*Logger.* -group
stack.module:Kalem.Ortak.* -app
stack.module:Polly* +group
```

| Eylem | Etkisi |
|---|---|
| `-group` | Çerçeve gruplamaya hiç katılmaz (günlük yazıcıları, sarmalayıcılar). |
| `+group` | Çerçeve, uygulama kodu olmasa da gruplamaya katılır. |
| `+app`, `-app` | Çerçeve uygulama kodu sayılır ya da sayılmaz; SDK'nın işaretini ve [uygulama kodu](#uygulama-kodu-in-app) kurallarını geçersiz kılar. Hata sayfasındaki yığın izini, sahiplik kurallarını ve yapay zekâya giden bağlamı da etkiler. |

Bir çerçeveye birden çok kural uyarsa sonuncusu geçerlidir. Projede en çok 500 kural olabilir.

## Birleştirme ve ayırma

Yanlış ayrılmış hataları **birleştirin**: hata listesinde aynı projenin hatalarını seçip **Birleştir**'e basın ve hangisinin kalacağını seçin (varsayılan, en çok olayı olan). Diğerlerinin olay sayıları, saatlik grafikleri, etiket dağılımları, etkilenen kullanıcıları, yorumları, geri bildirimleri, takipçileri, yapay zekâ analizleri ve iş kayıtları kalan hataya geçer; olayları birkaç dakika içinde arka planda taşınır. Birleştirilen hataların parmak izleriyle gelen **yeni olaylar da** kalan hataya düşer. Eski kısa kimlikler (`KALEM-1B`) ve bağlantılar kalan hataya götürür. Üyeler (`member`) birleştirebilir; işlem denetim kaydına yazılır.

Birleştirmeyi geri almak için kalan hatanın sayfasındaki **Birleştirilen hatalar** listesinden **Ayır**'a basın. Parmak izi yeniden kendi hatasına çıkar ve, başka bir hata almadıysa, eski kısa kimliğini geri alır. O parmak iziyle gelmiş olaylar ona taşınır; iki hatanın sayaçları, grafikleri, etiketleri ve etkilenen kullanıcıları saklanan olaylardan yeniden hesaplanır (saklama süresini aşmış olaylar artık sayılmaz). Yorumlar ve diğer kayıtlar kalan hatada kalır.

Olay bir kez birleştirilmiş bir hatada saklandığında kendi parmak izini (`grouping_hash`) da taşır; ayırma bu yüzden olay olay doğru çalışır.

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

**Aynı kusur birden fazla hataya bölünüyor.** Genellikle mesajdaki değişken bir değer (sipariş numarası vb.) ya da yığın izi olmayan bir istisnadır. Mesaj şablonu gönderin, `fingerprint` ya da bir [parmak izi kuralı](#parmak-izi-kuralları) kullanın; açılmış hataları [birleştirin](#birleştirme-ve-ayırma).

**Farklı kusurlar aynı hatada toplanıyor.** Genellikle yığın izi gönderilmiyordur ya da çerçeveler uygulama kodu olarak işaretlenmemiştir. SDK ayarlarını, uygulama kodu kurallarını ve [yığın izi kurallarını](#yığın-izi-kuralları) (ör. ortak bir günlük yazıcısı için `-group`) kontrol edin. Birleştirmeyle karışan hataları **Ayır** ile geri alın.

**Sürüm yükseltmeden sonra yeni hatalar açıldı.** Fonksiyon ya da dosya adı değiştiyse bu beklenen bir durumdur: kod değişmiştir. Eski hata sessizleşir, yenisi aynı kusuru temsil eder.
