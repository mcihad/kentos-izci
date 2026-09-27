# Gelen veri filtreleri

Her hata işe yaramaz: botların tetiklediği sayfa hataları, kullanıcıların tarayıcı eklentilerinin fırlattığı istisnalar, artık desteklenmeyen tarayıcılar, geliştiricilerin kendi makinelerinden gönderdikleri. Gelen veri filtreleri bu olayları **saklamadan atar**; kayıtlar, uyarılar ve istatistikler gerçek sorunlarla kalır.

Filtreler proje başınadır: **Projeler › proje › Filtreler** (yönetici rolü).

## Nasıl çalışır

- Filtreler olay alınır alınmaz, hız sınırından ve aylık kotadan **önce** uygulanır. Atılan olay sınırlara ve kotaya sayılmaz.
- Atılan olay için istemci `EVENT_OUTCOME_FILTERED` yanıtı alır; Sentry SDK'ları olayı başarılı gönderilmiş sayar, yeniden denemez.
- Atılan olayların ekleri de saklanmaz.
- **Son 30 günde atılan olaylar** bölümü, filtrelere takılan olayları neden ve güne göre sayar. Bir filtrenin beklenenden fazlasını attığını buradan fark edebilirsiniz.

## Hazır filtreler

| Filtre | Ne atılır |
|---|---|
| Tarayıcı eklentileri | Yığın izinde eklenti betiği bulunan hatalar (`chrome-extension://`, `moz-extension://`, `safari-web-extension://`, Safari'nin gizlediği `webkit-masked-url://` …), eklentilerin bilinen hataları (`top.GLOBALS`, `originalCreateNotification`, `fb_xd_fragment` …) ve başka alan adındaki betiklerden gelen ayrıntısız `Script error.` hataları. Mesaj kontrolleri yalnız tarayıcı olaylarına uygulanır. |
| Botlar | User-Agent'ı arama motoru botu (Googlebot, Bingbot, YandexBot …), bağlantı önizleyicisi (facebookexternalhit, Twitterbot, LinkedInBot), yapay zekâ tarayıcısı (GPTBot, ClaudeBot) ya da çalışma süresi denetleyicisi (Pingdom, UptimeRobot, Lighthouse, başsız Chrome) olan olaylar. |
| Eski tarayıcılar | Internet Explorer; Edge 18 ve öncesi (EdgeHTML); Safari 11, Chrome 79, Firefox 77, Opera 66 ve öncesi; Opera Mini ve eski Android tarayıcısı. |
| Yerel geliştirme | `localhost`, `127.0.0.1`, `::1` ya da `*.localhost` adresindeki sayfalardan gelen ve bu adreslerden gönderilen olaylar. |

Tarayıcıya ve bota bakan filtreler olaydaki User-Agent başlığını kullanır (Sentry'nin tarayıcı SDK'sı bunu gönderir). User-Agent taşımayan olaylar (sunucu ve mobil SDK'ları) bu filtrelerden etkilenmez.

## Kendi filtreleriniz

Her satıra bir değer yazılır.

**Kayıt dışı ortamlar:** Bu ortamlardan gelen olaylar atılır, ör. `development`, `test`. Ortam verilmeyen olaylar `production` sayılır.

**Hata mesajları:** Desenler, olayın mesajıyla bütün olarak ve büyük küçük harf ayırmadan karşılaştırılır. `*` her şeyle (satır sonları dahil), `?` tek karakterle eşleşir. İstisnalarda `Tür: mesaj`, yalnız mesaj ve yalnız tür ayrı ayrı denenir:

| Desen | Neyi atar |
|---|---|
| `*ResizeObserver loop*` | Tarayıcıların zararsız ResizeObserver uyarısı |
| `ChunkLoadError*` | Yeni sürüm yayınlanınca eski sayfaların yükleyemediği parçalar |
| `TypeError: Failed to fetch` | Tam olarak bu tür ve mesaj |
| `*Nesne başvurusu bir nesnenin örneğine ayarlanmadı*` | Türkçe .NET mesajı, her türle |

**IP adresleri:** Tek adres (`203.0.113.7`) ya da aralık (`10.20.0.0/16`). Hem olayın istemcisine (son kullanıcı; tarayıcı olaylarında gönderen adres) hem de olayı gönderen makineye bakılır. Böylece bir test sunucusunun gönderdiği olaylar da atılabilir.

**Sürümler:** Olayın `release` değeriyle karşılaştırılan desenler, ör. `*-dev`, `kalem-web@4.*`.

## Başka yollar

- Hatalar hiç gönderilmesin istiyorsanız SDK'da filtreleyin (`beforeSend`, `ignoreErrors`); ağ trafiği de azalır.
- Bir hatanın bildirimlerini susturmak ama olaylarını saklamak için hatayı **yoksayın**; filtreler olayları hiç saklamaz.
- Organizasyon genelinde hız sınırı ve kota için [Organizasyonlar ve roller](cok-kiracilik.md) rehberine bakın.
