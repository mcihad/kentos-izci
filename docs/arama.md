# Arama dili

Hatalar sayfası, MCP aracı `search_issues` ve API aynı arama dilini kullanır:

```
is:unresolved level:>=error customer:sivas-belediyesi release:1.4.2 "bağlantı zaman aşımı"
```

Arama boşken `is:unresolved` uygulanır. Arama kutusu yazarken anahtar ve değer önerir.

## Sözdizimi

- Terimler boşlukla ayrılır ve **hepsi birlikte** sağlanmalıdır (VE).
- `anahtar:değer` bir filtredir. Tırnaksız kelimeler ya da `"tırnaklı metin"` başlıkta ve culprit'te arama yapar (büyük/küçük harf fark etmez).
- Virgül **alternatifler** demektir: `level:error,fatal` → error **ya da** fatal.
- Başa `!` konursa terim tersine döner: `!environment:staging`.
- Değerde boşluk varsa tırnak kullanın: `customer:"sivas belediyesi"`.
- `*` joker karakterdir: `release:1.4.*`, `type:*Timeout*`.
- Karşılaştırmalar: `>`, `>=`, `<`, `<=` (ör. `times_seen:>100`, `level:>=error`).
- Göreli zaman: `-24h` "son 24 saat içinde", `+7d` "7 günden eski". Birimler: `m`, `h`, `d`, `w`.

## Anahtarlar

### Durum

| Terim | Anlamı |
|---|---|
| `is:unresolved`, `is:resolved`, `is:ignored` | Durum. |
| `is:new` | Son 72 saatte ilk kez görülenler. |
| `is:regressed` | Çözülmüşken geri dönenler. |
| `is:escalating` | Yoksayılmışken olağan dışı artanlar. |
| `is:assigned`, `is:unassigned` | Atanmış ya da atanmamış. |
| `is:unhandled` | Yakalanmamış istisnalar (çökme, işlenmemiş promise…). |
| `assigned:me`, `assigned:none`, `assigned:ayse@kentos.com.tr` | Atanan kişi. |
| `has:ai`, `has:user`, `has:customer`, `has:assignee` | Yapay zekâ analizi yapılmış, kullanıcı ya da müşteri bilgisi olan, atanmış. |

### Hatanın kendisi

| Terim | Anlamı |
|---|---|
| `level:error`, `level:>=warning` | Seviye: `debug`, `info`, `warning`, `error`, `fatal`. |
| `priority:high`, `priority:>=high` | Öncelik: `low`, `medium`, `high`, `critical`. |
| `project:kalem,bildirim` | Proje kısa adı. |
| `type:*NullReference*` | İstisna türü (`error.type` da yazılabilir). |
| `file:OrderService.cs` | Hatanın oluştuğu dosya (`filename` da yazılabilir). |
| `first_seen:-24h`, `last_seen:>2026-09-01` | İlk ya da son görülme. Tarih biçimi `YYYY-MM-DD` ya da RFC 3339. |
| `times_seen:>1000`, `users:>10`, `customers:>=3` | Olay, kullanıcı ve müşteri sayıları. |

### Olayların özellikleri

Bu anahtarlar, hatanın **herhangi bir olayında** değerin görülüp görülmediğine bakar:

| Terim | Anlamı |
|---|---|
| `environment:production` (`env:`) | Ortam. |
| `release:kalem@1.4.2` | Sürüm. |
| `customer:sivas-belediyesi` | Müşteri anahtarı (küçük harf). |
| `tenant:sivas` | Tenant. |
| `user:ayse@example.com` | Kullanıcı: olayda e-posta varsa e-posta, yoksa kullanıcı adı, o da yoksa kimlik. |
| `browser:Chrome*`, `os:Windows*`, `device:mobile` | İstemci. Tarayıcı ve işletim sistemi sürümle birlikte saklanır (`Chrome 128`, `Windows 11`), bu yüzden `*` kullanın. Cihaz; model, cihaz türü ya da `desktop`, `mobile`, `tablet`. |
| `server:kalem-api-1` (`server_name:`) | Sunucu adı. |
| `runtime:".NET 10*"`, `transaction:"POST /api/basvurular"`, `logger:KentOS.Kalem.Data` | Diğer olay alanları. |
| `handled:no`, `handled:yes` | Mekanizması yakalanmamış (ya da yakalanmış) olarak bildirilen olaylar. |
| Diğer her anahtar | Aynı adlı etiket: `region:ic-anadolu`, `modul:tahsilat`. |

## Örnekler

```
is:unresolved level:>=error first_seen:-24h
```
Son 24 saatte ortaya çıkan, çözülmemiş hatalar.

```
customer:sivas-belediyesi,tokat-belediyesi !environment:staging
```
İki belediyeyi etkileyen, staging dışındaki hatalar.

```
is:regressed release:1.4.*
```
1.4 sürümleriyle geri dönen hatalar.

```
times_seen:>500 users:>20 is:unassigned
```
Çok kişiyi etkileyen ama henüz kimsenin üstlenmediği hatalar.

```
has:ai is:unresolved
```
Yapay zekâ analizi bekleyen ya da tamamlanmış açık hatalar.

## Hatalı arama

Arama anlaşılamazsa sonuç yerine hatanın konumu ve nedeni gösterilir. Örneğin `level:kritik` için: "unknown level" (konum 0, `kritik`). API bu durumda `ERROR_REASON_INVALID_QUERY` ile `position` ve `token` bilgisini döner.

## Hata özeti

Hata listesinde bir satıra tıklamak hatanın özetini sağdan açılan bir çekmecede gösterir: başlık, durum, çöz/yoksay/ata düğmeleri, son 24 saat ve 30 günün grafikleri, ilk ve son görülme, etkilenen müşteriler ve etiketler. Liste arkada kullanılabilir kalır; başka bir satıra tıklamak özeti o hataya geçirir, **Esc** kapatır. Çekmecenin kenarını sürükleyerek genişliği ayarlayabilirsiniz; tarayıcı bunu hatırlar. **Tam sayfayı aç** hatanın kendi sayfasına geçer. ⌘ ya da Ctrl ile tıklamak (veya orta tık) hatayı doğrudan yeni sekmede açar. Telefonda özet alttan açılır.

Klavyeyle: **J** / **K** listede gezinir, **Enter** odaktaki hatanın özetini açar (özet açıkken J ve K özeti de değiştirir), **O** hatayı tam sayfada açar.

## Kayıtlı aramalar

Hata listesindeki **Kayıtlı aramalar › Bu aramayı kaydet** arama ifadesini, seçili projeleri, sıralamayı ve dönemi bir adla saklar; aynı menüden seçilince hepsi geri gelir. Aramalar kişiseldir; üyeler (`member` ve üstü) **Organizasyonla paylaş** ile bütün organizasyona açabilir. Paylaşılan bir aramayı yalnız sahibi ve yöneticiler değiştirip silebilir. Kaydederken arama denetlenir; anlaşılmayan ifade kaydedilmez.

## Panolar

**Panolar** sayfasında ekipler izledikleri aramaları tek ekranda toplar. Bir pano şu bileşenlerden oluşur:

- **Grafik:** aramanın dönemdeki hata ve olay sayısı ile olayların zaman içindeki dağılımı (son 24 saat ile son 90 gün arası).
- **Hata listesi:** aramanın seçilen sıralamaya göre ilk 1-20 hatası.

Bileşenler bir arama ifadesiyle ya da bir kayıtlı aramadan doldurulur, yukarı aşağı taşınır ve kaldırılır; **Hatalarda aç** bileşenin aramasını hata listesinde açar. Panolar da kişiseldir ya da paylaşılır; kurallar kayıtlı aramalardaki gibidir. Örnek: bir belediye için `customer:sivas-belediyesi is:unresolved` grafiği ve `customer:sivas-belediyesi level:fatal` listesi.

Grafiğin sayıları `IssueService.GetQueryStats` ile, kayıtlı aramalar ve panolar `ViewService` ile [Yönetim API'sinden](api/izci.api.v1.md) de okunup yazılabilir.

## CSV olarak dışa aktarma

Hata listesindeki **CSV**, o anki aramanın (projeler, sıralama ve dönemle) en çok 1000 hatasını indirir: kısa kimlik, başlık, proje, durum, dönemdeki ve toplam olay, kullanıcı ve müşteri sayısı, atanan kişi, ilk ve son görülme, bağlantı. Dosya noktalı virgülle ayrılır ve Excel'in Türkçe ayarlarıyla doğrudan açılır.
