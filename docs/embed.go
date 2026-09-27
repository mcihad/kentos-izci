// Package docs embeds izci's user guides (Turkish) and the API reference
// generated from the proto files (English), so that a running server can
// show them in the web UI, serve /llms.txt and offer them to MCP clients.
package docs

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed *.md api/*.md
var files embed.FS

// Guide describes one document.
type Guide struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Section     string `json:"section"`
}

// Guides lists the documents in reading order.
var Guides = []Guide{
	{"kurulum", "Kurulum ve ilk çalıştırma", "Gereksinimler, veritabanı, ilk yönetici ve ilk proje.", "Başlarken"},
	{"yapilandirma", "Yapılandırma", "Bütün ortam değişkenleri, varsayılanları ve doğrulama kuralları.", "Başlarken"},
	{"mimari", "Mimari", "Tek port, ingest boru hattı, veritabanı ve arka plan işleri.", "Başlarken"},
	{"ingest", "Hata gönderme", "gRPC, Connect ve JSON ile olay gönderme; C#, TypeScript, Dart, Python ve Go örnekleri.", "Veri gönderme"},
	{"go-sdk", "Go SDK", "Go uygulamalarında hataları, panikleri ve bağlamı yakalama.", "Veri gönderme"},
	{"sentry", "Sentry SDK'larıyla kullanım", "Mevcut Sentry SDK'larını DSN değiştirerek İzci'ye bağlama.", "Veri gönderme"},
	{"arama", "Arama dili", "is:, level:, customer:, release: ve diğer anahtarlarla hata arama.", "Kullanım"},
	{"gruplama", "Gruplama", "Olayların hangi kurallarla aynı hataya toplandığı.", "Kullanım"},
	{"cron", "Zamanlanmış görev izleme", "Cron işlerinin zamanında, hatasız ve süresinde çalıştığını izleme; Sentry check-in'leri, HTTP ve Go SDK.", "Kullanım"},
	{"telegram", "Telegram bildirimleri", "Botu bağlama, uyarı kuralları ve Telegram'dan triage.", "Kullanım"},
	{"yapay-zeka", "GitHub ve yapay zekâ analizi", "GitHub bağlantısı, opencode ile analiz, dal ve çekme isteği.", "Kullanım"},
	{"mcp", "MCP ile yapay zekâ asistanları", "Claude Code, opencode ve Cursor'dan İzci'yi kullanma.", "Kullanım"},
	{"cok-kiracilik", "Organizasyonlar ve roller", "Çoklu kiracılık, roller, erişim anahtarları ve kapsamlar.", "Yönetim"},
	{"guvenlik", "Güvenlik ve KVKK", "Maskeleme, IP saklama, şifreleme ve yapay zekâ güvenliği.", "Yönetim"},
	{"operasyon", "İşletim", "Dağıtım, ters vekil, yedekleme, saklama süresi ve ölçekleme.", "Yönetim"},
	{"veri-modeli", "Veri modeli", "Tablolar, bölümleme ve satır düzeyi güvenlik.", "Yönetim"},
	{"cli", "Komut satırı", "izci komutları ve bayrakları.", "Yönetim"},
	{"api/izci.ingest.v1", "Ingest API başvurusu", "izci.ingest.v1: olay modeli ve IngestService (İngilizce).", "Başvuru"},
	{"api/izci.api.v1", "Yönetim API başvurusu", "izci.api.v1: arayüzün ve dış araçların kullandığı API (İngilizce).", "Başvuru"},
}

// Read returns a document's Markdown.
func Read(slug string) (string, bool) {
	if !known(slug) {
		return "", false
	}
	b, err := files.ReadFile(slug + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}

func known(slug string) bool {
	for _, g := range Guides {
		if g.Slug == slug {
			return true
		}
	}
	return false
}

// LLMs renders llms.txt (an index with links under base) or, with full,
// llms-full.txt (every document inline).
func LLMs(base string, full bool) string {
	base = strings.TrimRight(base, "/")
	var b strings.Builder
	b.WriteString("# izci\n\n")
	b.WriteString("> izci is a self-hosted, Sentry-style error tracking platform for the KentOS applications: " +
		"applications send errors over gRPC/Connect (izci.ingest.v1) or with Sentry SDKs; izci groups them into issues, " +
		"tracks customers (tenants), notifies on Telegram, analyzes issues with AI (opencode) and serves an MCP endpoint. " +
		"The guides are in Turkish; the API reference is in English.\n\n")
	if !full {
		b.WriteString("The complete documentation in one file: " + base + "/llms-full.txt\n")
		section := ""
		for _, g := range Guides {
			if g.Section != section {
				section = g.Section
				b.WriteString("\n## " + section + "\n\n")
			}
			b.WriteString("- [" + g.Title + "](" + base + "/docs/raw/" + g.Slug + "): " + g.Description + "\n")
		}
		b.WriteString("\n## MCP\n\n- Endpoint: " + base + "/mcp (streamable HTTP, header \"Authorization: Bearer izp_…\")\n")
		return b.String()
	}
	for _, g := range Guides {
		text, _ := Read(g.Slug)
		b.WriteString("\n\n---\n\n<!-- " + g.Slug + " -->\n\n")
		b.WriteString(strings.TrimSpace(text))
		b.WriteString("\n")
	}
	return b.String()
}

// Register serves the documents: the index as JSON, each document as
// Markdown, and llms.txt / llms-full.txt for AI tools.
func Register(handle func(pattern string, h http.Handler), publicURL string) {
	cache := func(w http.ResponseWriter) { w.Header().Set("Cache-Control", "public, max-age=300") }
	handle("GET /docs/index.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cache(w)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Guides)
	}))
	handle("GET /docs/raw/{slug...}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text, ok := Read(strings.TrimSuffix(r.PathValue("slug"), ".md"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		cache(w)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(text))
	}))
	for path, full := range map[string]bool{"GET /llms.txt": false, "GET /llms-full.txt": true} {
		handle(path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			cache(w)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(LLMs(publicURL, full)))
		}))
	}
}
