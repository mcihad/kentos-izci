import type { GetIngestEndpointsResponse } from "@/gen/izci/api/v1/project_pb";

export type Snippet = { id: string; label: string; lang: string; intro: string; code: string };

/** Ready-to-paste setup code for each supported way of sending events. */
export function setupSnippets(ep: GetIngestEndpointsResponse): Snippet[] {
  const base = ep.baseUrl;
  const dsn = ep.sentryDsnTemplate.replace("<key>", "<anahtar>");
  const body = JSON.stringify(
    {
      event: {
        level: "LEVEL_ERROR",
        environment: "production",
        release: "1.4.2",
        customer: { id: "sivas-belediyesi", name: "Sivas Belediyesi" },
        user: { id: "1042", email: "ayse.yilmaz@example.com" },
        exceptions: [{ type: "TimeoutException", value: "SMS provider did not answer in 30 s" }],
      },
    },
    null,
    2,
  );
  return [
    {
      id: "go",
      label: "Go",
      lang: "go",
      intro: "İzci Go SDK'sı paniği, hata zincirini, isteği ve sunucu kaynak kullanımını otomatik toplar.",
      code: `// go get github.com/mcihad/kentos-izci/sdk/go
import izci "github.com/mcihad/kentos-izci/sdk/go"

func main() {
	if err := izci.Init(izci.Options{
		Endpoint:     "${base}",
		Key:          os.Getenv("IZCI_KEY"),
		Environment:  "production",
		Release:      "1.4.2",
		CustomerID:   "sivas-belediyesi",
		CustomerName: "Sivas Belediyesi",
	}); err != nil {
		log.Fatal(err)
	}
	defer izci.Close(3 * time.Second)

	// Report panics and 5xx responses of an HTTP server.
	handler := izcihttp.New(izcihttp.Options{}).Handle(mux)
	log.Fatal(http.ListenAndServe(":8080", handler))
}

// Anywhere else:
izci.CaptureError(err)`,
    },
    {
      id: "dotnet",
      label: ".NET",
      lang: "csharp",
      intro:
        "Sentry .NET SDK'sını İzci'ye yönlendirin. Müşteri ve kiracı etiketleri İzci'de ayrı alanlara dönüşür.",
      code: `// dotnet add package Sentry.AspNetCore
builder.WebHost.UseSentry(o =>
{
    o.Dsn = "${dsn}";
    o.Environment = "production";
    o.Release = "1.4.2";
});

// Per request, for example in a middleware:
SentrySdk.ConfigureScope(scope =>
{
    scope.SetTag("customer", "sivas-belediyesi");
    scope.SetTag("customer_name", "Sivas Belediyesi");
    scope.SetTag("tenant", tenantId);
});`,
    },
    {
      id: "node",
      label: "Node.js",
      lang: "typescript",
      intro: "Sentry Node SDK'sı İzci ile olduğu gibi çalışır; yalnızca DSN değişir.",
      code: `// npm install @sentry/node
import * as Sentry from "@sentry/node";

Sentry.init({
  dsn: "${dsn}",
  environment: "production",
  release: "1.4.2",
});

Sentry.setTag("customer", "sivas-belediyesi");
Sentry.setTag("customer_name", "Sivas Belediyesi");`,
    },
    {
      id: "browser",
      label: "Tarayıcı",
      lang: "typescript",
      intro: "Tarayıcıdan gönderim için anahtarın izin verilen kaynaklarına uygulamanızın adresini ekleyin.",
      code: `// npm install @sentry/browser
import * as Sentry from "@sentry/browser";

Sentry.init({
  dsn: "${dsn}",
  environment: "production",
  release: "1.4.2",
});

Sentry.setUser({ id: user.id, email: user.email });
Sentry.setTag("customer", tenant.key);`,
    },
    {
      id: "python",
      label: "Python",
      lang: "python",
      intro: "Sentry Python SDK'sı İzci ile olduğu gibi çalışır.",
      code: `# pip install sentry-sdk
import sentry_sdk

sentry_sdk.init(
    dsn="${dsn}",
    environment="production",
    release="1.4.2",
)
sentry_sdk.set_tag("customer", "sivas-belediyesi")`,
    },
    {
      id: "flutter",
      label: "Flutter",
      lang: "dart",
      intro: "Sentry Flutter SDK'sı mobil ve masaüstü uygulamalardan gönderir.",
      code: `// flutter pub add sentry_flutter
await SentryFlutter.init(
  (options) {
    options.dsn = '${dsn}';
    options.environment = 'production';
    options.release = '1.4.2';
  },
  appRunner: () => runApp(const App()),
);

Sentry.configureScope((scope) => scope.setTag('customer', 'sivas-belediyesi'));`,
    },
    {
      id: "curl",
      label: "HTTP (JSON)",
      lang: "shellscript",
      intro: "SDK'sı olmayan her dil Connect protokolüyle düz JSON gönderebilir.",
      code: `curl ${base}/izci.ingest.v1.IngestService/SendEvent \\
  -H "Authorization: Bearer $IZCI_KEY" \\
  -H "Content-Type: application/json" \\
  -d '${body}'`,
    },
    {
      id: "grpc",
      label: "gRPC",
      lang: "shellscript",
      intro: `Yerel gRPC istemcileri ${ep.grpcTarget} adresine ${ep.grpcTls ? "TLS ile" : "şifresiz HTTP/2 ile"} bağlanır. İstemcileri proto dosyalarından üretebilirsiniz.`,
      code: `buf curl --schema proto --protocol grpc${ep.grpcTls ? "" : " --http2-prior-knowledge"} \\
  -H "authorization: Bearer $IZCI_KEY" \\
  -d '${body}' \\
  ${base}/izci.ingest.v1.IngestService/SendEvent`,
    },
  ];
}
