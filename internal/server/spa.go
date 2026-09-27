package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/mcihad/kentos-izci/internal/config"
)

// contentSecurityPolicy is sent with every HTML document. The built SPA has
// no inline scripts; inline styles are allowed because Radix positions
// popovers with style attributes.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"worker-src 'self' blob:; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// placeholderHTML is served when the binary was built without the web UI.
const placeholderHTML = `<!doctype html>
<html lang="tr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>İzci</title></head>
<body style="margin:0;min-height:100vh;display:grid;place-items:center;background:#0b0d10;color:#e6e8eb;font-family:system-ui,sans-serif">
<main style="max-width:32rem;padding:2rem;text-align:center">
<h1 style="font-weight:600;letter-spacing:-0.02em">İzci</h1>
<p style="color:#9aa3ad;line-height:1.6">Arayüz bu derlemeye eklenmemiş. Arayüzü derlemek için <code>make web</code>,
geliştirme sunucusu için <code>make dev</code> komutunu çalıştırın.</p>
</main></body></html>`

type spaFile struct {
	name        string
	etag        string
	contentType string
	encodings   map[string]string // "br"/"gzip" → precompressed file name
}

// spaHandler serves the embedded single-page app: static files with strong
// caching (hashed assets are immutable), precompressed variants when the
// client accepts them, and index.html for every client-side route.
type spaHandler struct {
	fsys        fs.FS
	files       map[string]*spaFile
	index       *spaFile
	hsts        bool
	log         *slog.Logger
	placeholder bool
}

// NewSPAHandler indexes fsys once at startup.
func NewSPAHandler(fsys fs.FS, cfg *config.Config, log *slog.Logger) http.Handler {
	h := &spaHandler{
		fsys:  fsys,
		files: map[string]*spaFile{},
		hsts:  strings.HasPrefix(cfg.PublicURL, "https://"),
		log:   log,
	}
	if fsys == nil {
		h.placeholder = true
		return h
	}

	names := map[string]bool{}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names[p] = true
		}
		return nil
	})
	for name := range names {
		if strings.HasSuffix(name, ".br") || strings.HasSuffix(name, ".gz") || path.Base(name) == ".gitkeep" {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			log.Warn("spa: cannot read embedded file", slog.String("file", name), slog.Any("err", err))
			continue
		}
		sum := sha256.Sum256(data)
		ct := mime.TypeByExtension(path.Ext(name))
		if ct == "" {
			ct = http.DetectContentType(data)
		}
		f := &spaFile{
			name:        name,
			etag:        `"` + hex.EncodeToString(sum[:8]) + `"`,
			contentType: ct,
			encodings:   map[string]string{},
		}
		if names[name+".br"] {
			f.encodings["br"] = name + ".br"
		}
		if names[name+".gz"] {
			f.encodings["gzip"] = name + ".gz"
		}
		h.files[name] = f
	}
	h.index = h.files["index.html"]
	if h.index == nil {
		h.placeholder = true
	}
	return h
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Unknown API paths (Sentry clients probe endpoints izci lacks) get a
	// JSON 404 instead of the app's HTML.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"detail":"not found"}`)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.placeholder {
		h.setDocumentHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, placeholderHTML)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" || rel == "index.html" {
		h.serveFile(w, r, h.index)
		return
	}
	if f, ok := h.files[rel]; ok {
		h.serveFile(w, r, f)
		return
	}
	// Unknown files (anything with an extension, or under /assets/) are real
	// 404s; everything else is a client-side route rendered by index.html.
	if path.Ext(rel) != "" || strings.HasPrefix(rel, "assets/") || !isBrowserNavigation(r) {
		http.NotFound(w, r)
		return
	}
	h.serveFile(w, r, h.index)
}

func (h *spaHandler) serveFile(w http.ResponseWriter, r *http.Request, f *spaFile) {
	hdr := w.Header()
	hdr.Set("Content-Type", f.contentType)
	hdr.Set("ETag", f.etag)
	switch {
	case f == h.index:
		hdr.Set("Cache-Control", "no-cache")
		h.setDocumentHeaders(w)
	case strings.HasPrefix(f.name, "assets/"):
		// Vite fingerprints everything under assets/, so it never changes.
		hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		hdr.Set("Cache-Control", "public, max-age=3600")
	}

	name := f.name
	if len(f.encodings) > 0 {
		hdr.Add("Vary", "Accept-Encoding")
		if enc := pickEncoding(r.Header.Get("Accept-Encoding"), f.encodings); enc != "" {
			name = f.encodings[enc]
			hdr.Set("Content-Encoding", enc)
		}
	}

	file, err := h.fsys.Open(name)
	if err != nil {
		h.log.Error("spa: open embedded file", slog.String("file", name), slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	if rs, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, rs)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (h *spaHandler) setDocumentHeaders(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", contentSecurityPolicy)
	hdr.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("Cross-Origin-Opener-Policy", "same-origin")
	hdr.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	if h.hsts {
		hdr.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

// pickEncoding returns the best precompressed encoding the client accepts,
// preferring brotli. Encodings listed with q=0 are refused.
func pickEncoding(acceptEncoding string, available map[string]string) string {
	if acceptEncoding == "" {
		return ""
	}
	accepted := map[string]bool{}
	for _, part := range strings.Split(acceptEncoding, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		name := strings.ToLower(strings.TrimSpace(fields[0]))
		ok := true
		for _, param := range fields[1:] {
			param = strings.ReplaceAll(strings.TrimSpace(param), " ", "")
			if param == "q=0" || param == "q=0.0" || param == "q=0.00" || param == "q=0.000" {
				ok = false
			}
		}
		accepted[name] = ok
	}
	for _, enc := range []string{"br", "gzip"} {
		if _, has := available[enc]; has && accepted[enc] {
			return enc
		}
	}
	return ""
}
