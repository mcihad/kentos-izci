# İzci belgeleri

İzci, KentOS uygulamalarının hatalarını toplayan, gruplayan, müşteri bazında analiz eden, Telegram ile bildiren ve yapay zekâyla inceleyen bir hata izleme platformudur. Bu belgeler çalışan her İzci sunucusunda da **Belgeler** bölümünden okunabilir; yapay zekâ araçları için `/llms.txt` ve `/llms-full.txt` adreslerinde sunulur.

## Başlarken

- [Kurulum ve ilk çalıştırma](kurulum.md)
- [Yapılandırma](yapilandirma.md): bütün ortam değişkenleri
- [Mimari](mimari.md)

## Veri gönderme

- [Hata gönderme](ingest.md): gRPC, Connect, JSON; C#, TypeScript, Dart, Python örnekleri
- [Go SDK](go-sdk.md)
- [Sentry SDK'larıyla kullanım](sentry.md)

## Kullanım

- [Arama dili](arama.md)
- [Gruplama](gruplama.md)
- [Telegram bildirimleri](telegram.md)
- [GitHub ve yapay zekâ analizi](yapay-zeka.md)
- [MCP ile yapay zekâ asistanları](mcp.md)

## Yönetim

- [Organizasyonlar ve roller](cok-kiracilik.md)
- [Güvenlik ve KVKK](guvenlik.md)
- [İşletim](operasyon.md)
- [Veri modeli](veri-modeli.md)
- [Komut satırı](cli.md)

## Başvuru (İngilizce)

- [Ingest API](api/izci.ingest.v1.md): olay modeli ve `IngestService`
- [Yönetim API'si](api/izci.api.v1.md)
