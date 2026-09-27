// Reports a caught exception to izci with the Connect protocol (JSON over
// HTTP), using only the .NET base library.
//
//   IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… dotnet run --project examples/csharp/IzciOrnek
//
// For generated gRPC clients use the protos in proto/izci/ingest/v1 with
// Grpc.Tools or buf; the JSON shape below is the same message.

using System.Diagnostics;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json.Nodes;

var url = Environment.GetEnvironmentVariable("IZCI_URL") ?? "http://localhost:5300";
var key = Environment.GetEnvironmentVariable("IZCI_KEY")
    ?? throw new InvalidOperationException("Set IZCI_KEY to an ingest key of the project (izk_…).");

try
{
    new BasvuruService().Onayla(77);
}
catch (Exception ex)
{
    var result = await Report(ex);
    Console.WriteLine($"{result?["outcome"]} {result?["issueShortId"]} {result?["issueUrl"]}");
}

async Task<JsonNode?> Report(Exception ex)
{
    var evt = new JsonObject
    {
        ["level"] = "LEVEL_ERROR",
        ["platform"] = "dotnet",
        ["environment"] = "production",
        ["release"] = "kalem@1.4.2",
        ["customer"] = new JsonObject { ["id"] = "sivas-belediyesi", ["name"] = "Sivas Belediyesi" },
        ["user"] = new JsonObject { ["id"] = "3310" },
        ["contexts"] = new JsonObject
        {
            ["runtime"] = new JsonObject { ["name"] = ".NET", ["version"] = Environment.Version.ToString() },
            ["os"] = new JsonObject { ["name"] = Environment.OSVersion.Platform.ToString(), ["version"] = Environment.OSVersion.VersionString },
        },
        ["exceptions"] = Exceptions(ex),
    };
    using var http = new HttpClient();
    using var request = new HttpRequestMessage(HttpMethod.Post, $"{url}/izci.ingest.v1.IngestService/SendEvent")
    {
        Content = JsonContent.Create(new JsonObject { ["event"] = evt }),
    };
    request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", key);
    using var response = await http.SendAsync(request);
    var body = await response.Content.ReadFromJsonAsync<JsonNode>();
    if (!response.IsSuccessStatusCode)
        throw new HttpRequestException($"izci answered {(int)response.StatusCode}: {body?["message"]}");
    return body?["result"];
}

// izci wants the root cause first and the raised exception last, and frames
// from the oldest call to the newest; .NET lists frames newest first.
static JsonArray Exceptions(Exception ex)
{
    var chain = new List<Exception>();
    for (var e = ex; e is not null; e = e.InnerException) chain.Insert(0, e);
    var list = new JsonArray();
    foreach (var e in chain)
    {
        var frames = new JsonArray();
        foreach (var f in new StackTrace(e, fNeedFileInfo: true).GetFrames().Reverse())
        {
            var method = f.GetMethod();
            var type = method?.DeclaringType?.FullName ?? "";
            frames.Add(new JsonObject
            {
                ["function"] = $"{method?.DeclaringType?.Name}.{method?.Name}",
                ["module"] = method?.DeclaringType?.Namespace ?? "",
                ["filename"] = f.GetFileName() ?? "",
                ["lineno"] = f.GetFileLineNumber(),
                ["inApp"] = !type.StartsWith("System.") && !type.StartsWith("Microsoft."),
            });
        }
        list.Add(new JsonObject
        {
            ["type"] = e.GetType().FullName,
            ["value"] = e.Message,
            ["mechanism"] = new JsonObject { ["type"] = "generic", ["handled"] = true },
            ["stacktrace"] = new JsonObject { ["frames"] = frames },
        });
    }
    return list;
}

class BasvuruService
{
    public void Onayla(int id)
    {
        try
        {
            Kaydet(id);
        }
        catch (TimeoutException e)
        {
            throw new InvalidOperationException($"Başvuru {id} onaylanamadı", e);
        }
    }

    static void Kaydet(int id) => throw new TimeoutException("Veritabanı 5 sn içinde yanıt vermedi");
}
