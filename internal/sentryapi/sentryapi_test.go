package sentryapi_test

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/ingest/sentry"
	"github.com/mcihad/kentos-izci/internal/sentryapi"
	"github.com/mcihad/kentos-izci/internal/store"
)

func newEnv(t *testing.T) *ingesttest.Env {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	e := ingesttest.New(t, ingesttest.Options{Mount: func(mux *http.ServeMux, svc *ingest.Service) {
		sentry.NewHandler(svc, log).Register(mux.Handle)
	}})
	sentryapi.New(e.DB, e.Keys, nil, e.Symbols, log).Register(e.Mux.Handle)
	return e
}

// client speaks Sentry's web API the way sentry-cli does.
type client struct {
	t     *testing.T
	base  string
	token string
}

func (c *client) call(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func (c *client) json(method, path string, body any, want int) map[string]any {
	c.t.Helper()
	status, raw := c.call(method, path, body)
	if status != want {
		c.t.Fatalf("%s %s: %d %s", method, path, status, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func sha(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

// chunks splits a file the way the server asks and returns the checksums.
func (c *client) chunks(data []byte) ([]string, [][]byte) {
	opts := c.json("GET", "/api/0/organizations/kentos/chunk-upload/", nil, 200)
	size := int(opts["chunkSize"].(float64))
	var sums []string
	var parts [][]byte
	for i := 0; i < len(data); i += size {
		p := data[i:min(len(data), i+size)]
		sums, parts = append(sums, sha(p)), append(parts, p)
	}
	return sums, parts
}

// sendChunks uploads chunks gzip-compressed, named by their checksum.
func (c *client) sendChunks(parts [][]byte) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, p := range parts {
		w, _ := mw.CreateFormFile("file_gzip", sha(p))
		zw := gzip.NewWriter(w)
		_, _ = zw.Write(p)
		_ = zw.Close()
	}
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.base+"/api/0/organizations/kentos/chunk-upload/", &body)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		c.t.Fatalf("chunk upload: %d", res.StatusCode)
	}
}

// uploadBundle runs the artifact bundle protocol: assemble (chunks
// missing), upload chunks, assemble again.
func (c *client) uploadBundle(bundle []byte, release string) map[string]any {
	c.t.Helper()
	sums, parts := c.chunks(bundle)
	req := map[string]any{"checksum": sha(bundle), "chunks": sums, "projects": []string{"kalem"}, "version": release}
	first := c.json("POST", "/api/0/organizations/kentos/artifactbundle/assemble/", req, 200)
	if first["state"] != "not_found" || len(first["missingChunks"].([]any)) != len(sums) {
		c.t.Fatalf("first assemble: %v", first)
	}
	c.sendChunks(parts)
	return c.json("POST", "/api/0/organizations/kentos/artifactbundle/assemble/", req, 200)
}

// uploadDebugFile runs the debug file protocol for one file.
func (c *client) uploadDebugFile(data []byte, name, debugID string) map[string]any {
	c.t.Helper()
	sums, parts := c.chunks(data)
	sum := sha(data)
	req := map[string]any{sum: map[string]any{"name": name, "debug_id": debugID, "chunks": sums}}
	path := "/api/0/projects/kentos/kalem/files/difs/assemble/"
	if first := c.json("POST", path, req, 200)[sum].(map[string]any); first["state"] != "not_found" {
		c.t.Fatalf("first assemble: %v", first)
	}
	c.sendChunks(parts)
	return c.json("POST", path, req, 200)[sum].(map[string]any)
}

type bundleFile struct {
	path, typ, url string
	headers        map[string]string
	data           []byte
}

func zipBundle(t *testing.T, release string, files ...bundleFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := map[string]any{"org": "kentos", "project": "kalem", "release": release}
	entries := map[string]any{}
	for _, f := range files {
		w, _ := zw.Create(f.path)
		_, _ = w.Write(f.data)
		entries[f.path] = map[string]any{"type": f.typ, "url": f.url, "headers": f.headers}
	}
	manifest["files"] = entries
	w, _ := zw.Create("manifest.json")
	_ = json.NewEncoder(w).Encode(manifest)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// col is the one-based UTF-16 column of needle in a one-line script, as a
// JavaScript engine reports it.
func col(t *testing.T, src, needle string) int {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("%q not found", needle)
	}
	return len(utf16.Encode([]rune(src[:i]))) + 1
}

// sendEvent posts a Sentry event to the store endpoint and returns the
// stored payload.
func sendEvent(t *testing.T, e *ingesttest.Env, event map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(event)
	url := fmt.Sprintf("%s/api/%d/store/?sentry_key=%s&sentry_version=7", e.Server.URL, e.Project.PublicID, e.Key)
	res, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("store: %d %s", res.StatusCode, out)
	}
	ctx := context.Background()
	var payload []byte
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM events WHERE event_id = $1`, event["event_id"]).Scan(&payload)
	})
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	return m
}

func frames(ev map[string]any, key string) []map[string]any {
	ex := ev["exceptions"].([]any)[0].(map[string]any)
	st := ex["stacktrace"].(map[string]any)
	list, _ := st[key].([]any)
	out := make([]map[string]any, len(list))
	for i, f := range list {
		out[i] = f.(map[string]any)
	}
	return out
}

func scriptEvent(id, release string, fs ...map[string]any) map[string]any {
	ev := map[string]any{
		"event_id": id, "platform": "javascript", "level": "error",
		"exception": map[string]any{"values": []any{map[string]any{
			"type": "InvoiceError", "value": "Fatura satırı yok",
			"stacktrace": map[string]any{"frames": fs},
		}}},
		"sdk": map[string]any{"name": "sentry.javascript.browser", "version": "11.0.0"},
	}
	if release != "" {
		ev["release"] = release
	}
	return ev
}

func scriptFrame(url, fn string, colno int) map[string]any {
	return map[string]any{"filename": url, "function": fn, "lineno": 1, "colno": colno, "in_app": true}
}

func TestSourceMapsByRelease(t *testing.T) {
	e := newEnv(t)
	js, _ := os.ReadFile("../symbols/sourcemap/testdata/index.js")
	sm, _ := os.ReadFile("../symbols/sourcemap/testdata/index.js.map")
	c := &client{t: t, base: e.Server.URL, token: e.Key}
	// sentry-cli learns the file limit (INGEST_UPLOAD_MAX_MB) from here.
	if opts := c.json("GET", "/api/0/organizations/kentos/chunk-upload/", nil, 200); opts["maxFileSize"] != float64(128<<20) {
		t.Fatalf("chunk upload options: %v", opts)
	}
	bundle := zipBundle(t, "kalem-web@5.1.0",
		bundleFile{path: "files/_/_/index.js", typ: "minified_source", url: "~/assets/index.js", data: js},
		bundleFile{path: "files/_/_/index.js.map", typ: "source_map", url: "~/assets/index.js.map", data: sm})
	if res := c.uploadBundle(bundle, ""); res["state"] != "ok" {
		t.Fatalf("assemble: %v", res)
	}
	// The same archive again is a no-op.
	again := c.json("POST", "/api/0/organizations/kentos/artifactbundle/assemble/",
		map[string]any{"checksum": sha(bundle), "chunks": []string{sha(bundle)}, "projects": []string{"kalem"}}, 200)
	if again["state"] != "ok" {
		t.Fatalf("second assemble: %v", again)
	}

	src := string(js)
	url := "https://kalem.example.com/assets/index.js?v=3"
	ev := sendEvent(t, e, scriptEvent("5b3f0e6c0f8a4a0e9c1d2b3a4f5e6d70", "kalem-web@5.1.0",
		scriptFrame(url, "HTMLButtonElement.<anonymous>", col(t, src, "i())")),
		scriptFrame(url, "i", col(t, src, "n([])")),
		scriptFrame(url, "n", col(t, src, "t(e);return")),
		scriptFrame(url, "t", col(t, src, "new e(`Fatura"))))
	fs := frames(ev, "frames")
	top := fs[3]
	if top["filename"] != "src/invoice.ts" || top["abs_path"] != "https://kalem.example.com/src/invoice.ts" ||
		top["lineno"] != float64(15) || top["function"] != "totalWithTax" || top["in_app"] != true ||
		!strings.Contains(top["context_line"].(string), `throw new InvoiceError("Fatura satırı yok")`) {
		t.Fatalf("top frame: %v", top)
	}
	if fs[2]["function"] != "formatTotal" || fs[2]["lineno"] != float64(21) || fs[1]["function"] != "saveInvoice" ||
		fs[1]["filename"] != "src/main.ts" || fs[1]["lineno"] != float64(6) {
		t.Fatalf("callers: %v / %v", fs[2], fs[1])
	}
	raw := frames(ev, "raw_frames")
	if len(raw) != 4 || raw[3]["function"] != "t" || raw[3]["filename"] != url {
		t.Fatalf("raw frames: %v", raw)
	}
	if ev["processing_errors"] != nil {
		t.Fatalf("processing errors: %v", ev["processing_errors"])
	}

	// Another release has no maps: the frames stay and the event says why.
	ev = sendEvent(t, e, scriptEvent("5b3f0e6c0f8a4a0e9c1d2b3a4f5e6d71", "kalem-web@5.2.0",
		scriptFrame(url, "t", col(t, src, "new e(`Fatura"))))
	perr := ev["processing_errors"].([]any)[0].(map[string]any)
	if frames(ev, "frames")[0]["function"] != "t" || perr["kind"] != "PROCESSING_ERROR_KIND_SOURCE_MAP_MISSING" ||
		perr["target"] != url || !strings.Contains(perr["detail"].(string), "kalem-web@5.2.0") {
		t.Fatalf("missing map: %v", ev["processing_errors"])
	}
	// Clients cannot inject what only the server sets.
	forged := scriptEvent("5b3f0e6c0f8a4a0e9c1d2b3a4f5e6d72", "", map[string]any{"function": "x", "filename": "app.py", "lineno": 3})
	forged["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["raw_frames"] = []any{}
	ev = sendEvent(t, e, forged)
	if ev["processing_errors"] != nil || frames(ev, "raw_frames") != nil && len(frames(ev, "raw_frames")) > 0 {
		t.Fatalf("forged: %v", ev)
	}
}

func TestSourceMapsByDebugID(t *testing.T) {
	e := newEnv(t)
	js, _ := os.ReadFile("../symbols/sourcemap/testdata/index.js")
	sm, _ := os.ReadFile("../symbols/sourcemap/testdata/index.js.map")
	c := &client{t: t, base: e.Server.URL, token: e.Key}
	const id = "6afa11e7-3def-4765-a777-e3c1f2bacefc"
	// Files named after the debug id, as sentry-cli uploads them.
	bundle := zipBundle(t, "",
		bundleFile{path: "files/_/_/" + id + "-0.js", typ: "minified_source", url: "~/" + id + "-0.js",
			headers: map[string]string{"debug-id": id, "sourcemap": "index-BQVxLPGT.js.map"}, data: js},
		bundleFile{path: "files/_/_/" + id + "-0.js.map", typ: "source_map", url: "~/" + id + "-0.js.map",
			headers: map[string]string{"debug-id": id}, data: sm})
	if res := c.uploadBundle(bundle, ""); res["state"] != "ok" {
		t.Fatalf("assemble: %v", res)
	}
	src := string(js)
	url := "https://kalem.example.com/assets/index-BQVxLPGT.js"
	event := scriptEvent("7c3f0e6c0f8a4a0e9c1d2b3a4f5e6d70", "",
		scriptFrame(url, "n", col(t, src, "t(e);return")),
		scriptFrame(url, "t", col(t, src, "new e(`Fatura")),
		scriptFrame("https://cdn.example.com/other.js", "z", 10))
	event["debug_meta"] = map[string]any{"images": []any{map[string]any{"type": "sourcemap", "code_file": url, "debug_id": id}}}
	ev := sendEvent(t, e, event)
	fs := frames(ev, "frames")
	if fs[1]["function"] != "totalWithTax" || fs[1]["filename"] != "src/invoice.ts" || fs[1]["lineno"] != float64(15) {
		t.Fatalf("frame: %v", fs[1])
	}
	// The map's sources are relative to its real location next to the
	// script, not to the debug-id name it was uploaded under.
	if fs[1]["abs_path"] != "https://kalem.example.com/src/invoice.ts" {
		t.Fatalf("abs_path: %v", fs[1]["abs_path"])
	}
	errs := ev["processing_errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["target"] != "https://cdn.example.com/other.js" {
		t.Fatalf("processing errors: %v", errs)
	}
}

func TestDartDebugFiles(t *testing.T) {
	e := newEnv(t)
	c := &client{t: t, base: e.Server.URL, token: e.Key}
	dsym := gunzip(t, "../symbols/native/testdata/dart-macos-arm64.symbols.gz")
	const debugID = "b4485c2e-6085-7b09-97e4-66d02ec92702"
	res := c.uploadDebugFile(dsym, "app.symbols", debugID)
	dif := res["dif"].(map[string]any)
	if res["state"] != "ok" || dif["debugId"] != debugID || dif["cpuName"] != "arm64" || dif["symbolType"] != "macho" {
		t.Fatalf("dSYM: %v", res)
	}
	res = c.uploadDebugFile(gunzip(t, "../symbols/native/testdata/obfuscation.map.json.gz"), "obfuscation.map.json", debugID)
	if res["state"] != "ok" || res["dif"].(map[string]any)["symbolType"] != "dartsymbolmap" {
		t.Fatalf("map: %v", res)
	}
	if res := c.uploadDebugFile([]byte("not a debug file at all"), "notes.txt", debugID); res["state"] != "error" ||
		!strings.Contains(res["detail"].(string), "unsupported") {
		t.Fatalf("junk: %v", res)
	}

	// The stack trace of the fixture as the Dart SDK reports it: frames
	// #07..#00 of the run, the image at isolate_dso_base.
	var fs []any
	for _, a := range []string{"0x101137d0f", "0x10116901f", "0x10115d30f", "0x101168d43", "0x10115cf07", "0x10116805f",
		"0x10115ccc7", "0x10115cd3f"} {
		fs = append(fs, map[string]any{"platform": "native", "instruction_addr": a})
	}
	event := map[string]any{
		"event_id": "9d3f0e6c0f8a4a0e9c1d2b3a4f5e6d70", "platform": "other", "level": "error", "release": "kalem-mobil@2.0.0+7",
		"exception": map[string]any{"values": []any{map[string]any{"type": "Ki", "value": "Instance of 'Ki'",
			"stacktrace": map[string]any{"frames": fs}}}},
		"debug_meta": map[string]any{"images": []any{map[string]any{"type": "macho", "debug_id": debugID,
			"code_id": "b4485c2e60857b0997e466d02ec92702", "code_file": "App.Framework/App", "image_addr": "0x101118000"}}},
		"sdk": map[string]any{"name": "sentry.dart", "version": "9.30.1"},
	}
	ev := sendEvent(t, e, event)
	ex := ev["exceptions"].([]any)[0].(map[string]any)
	if ex["type"] != "InvoiceException" || ex["value"] != "Instance of 'InvoiceException'" {
		t.Fatalf("exception: %v %v", ex["type"], ex["value"])
	}
	got := frames(ev, "frames")
	n := len(got)
	want := []struct {
		fn   string
		line float64
	}{{"main", 22}, {"computeTotals", 18}, {"InvoiceService.total", 11}}
	for i, w := range want {
		f := got[n-3+i]
		if f["function"] != w.fn || f["lineno"] != w.line || f["filename"] != "main.dart" || f["platform"] != "dart" || f["in_app"] != true {
			t.Fatalf("frame %d: %v", n-3+i, f)
		}
	}
	if len(frames(ev, "raw_frames")) != 8 {
		t.Fatalf("raw frames: %v", frames(ev, "raw_frames"))
	}

	// An image without uploaded files is reported.
	event["event_id"] = "9d3f0e6c0f8a4a0e9c1d2b3a4f5e6d71"
	event["debug_meta"].(map[string]any)["images"].([]any)[0].(map[string]any)["debug_id"] = "00000000-0000-0000-0000-000000000001"
	ev = sendEvent(t, e, event)
	perr := ev["processing_errors"].([]any)[0].(map[string]any)
	if perr["kind"] != "PROCESSING_ERROR_KIND_DEBUG_FILE_MISSING" || perr["target"] != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("missing debug file: %v", perr)
	}

	var stored int
	e.Scalar(t, &stored, `SELECT count(*) FROM debug_files`)
	if stored != 2 {
		t.Fatalf("debug files: %d", stored)
	}
}

func gunzip(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	return b
}

func TestAuthorization(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var ingestOnly, exposed string
	err := e.DB.InSystem(ctx, func(tx pgx.Tx) error {
		_, secret, err := store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: e.Org.ID, ProjectID: e.Project.ID, Name: "browser",
			Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST}})
		if err != nil {
			return err
		}
		ingestOnly = secret
		// A CI key that a browser application also used.
		k, secret, err := store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{OrgID: e.Org.ID, ProjectID: e.Project.ID, Name: "leaked",
			Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST, apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES}})
		if err != nil {
			return err
		}
		exposed = secret
		_, err = store.MarkAPIKeyBrowserSeen(ctx, tx, k.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		token, path string
		want        int
	}{
		{"", "/api/0/organizations/kentos/chunk-upload/", 401},
		{"izk_nope", "/api/0/organizations/kentos/chunk-upload/", 401},
		{ingestOnly, "/api/0/organizations/kentos/chunk-upload/", 403},
		{exposed, "/api/0/organizations/kentos/chunk-upload/", 403},
		{e.Key, "/api/0/organizations/other/chunk-upload/", 404},
		{e.Key, "/api/0/organizations/kentos/chunk-upload/", 200},
		{e.Key, "/api/0/", 200},
	} {
		cl := &client{t: t, base: e.Server.URL, token: c.token}
		if status, body := cl.call("GET", c.path, nil); status != c.want {
			t.Errorf("%q %s: %d %s", c.token, c.path, status, body)
		}
	}
	cl := &client{t: t, base: e.Server.URL, token: e.Key}
	// A key is bound to its project.
	status, body := cl.call("POST", "/api/0/organizations/kentos/artifactbundle/assemble/",
		map[string]any{"checksum": "x", "chunks": []string{"x"}, "projects": []string{"baska"}})
	if status != 403 || !strings.Contains(string(body), "kalem") {
		t.Errorf("foreign project: %d %s", status, body)
	}
	// Chunks must match their names.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, _ := mw.CreateFormFile("file", strings.Repeat("0", 40))
	_, _ = w.Write([]byte("data"))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.Server.URL+"/api/0/organizations/kentos/chunk-upload/", &buf)
	req.Header.Set("Authorization", "Bearer "+e.Key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("mismatched chunk: %d", res.StatusCode)
	}
}

func TestReleases(t *testing.T) {
	e := newEnv(t)
	c := &client{t: t, base: e.Server.URL, token: e.Key}
	rel := c.json("POST", "/api/0/projects/kentos/kalem/releases/",
		map[string]any{"version": "kalem-web@5.1.0", "projects": []string{"kalem"}, "dateStarted": time.Now().UTC()}, 201)
	projects := rel["projects"].([]any)
	if rel["version"] != "kalem-web@5.1.0" || rel["newGroups"] != float64(0) || projects[0].(map[string]any)["slug"] != "kalem" {
		t.Fatalf("new: %v", rel)
	}
	c.json("PUT", "/api/0/projects/kentos/kalem/releases/kalem-web@5.1.0/",
		map[string]any{"dateReleased": time.Now().UTC(), "refs": []any{map[string]any{"repository": "kentos/kalem",
			"commit": "0123456789ABCDEF0123456789abcdef01234567"}}}, 200)
	dep := c.json("POST", "/api/0/organizations/kentos/releases/kalem-web@5.1.0/deploys/",
		map[string]any{"environment": "Production", "name": "ci"}, 201)
	if dep["environment"] != "production" {
		t.Fatalf("deploy: %v", dep)
	}
	info := c.json("GET", "/api/0/organizations/kentos/releases/kalem-web@5.1.0/", nil, 200)
	if info["deployCount"] != float64(1) || info["lastCommit"].(map[string]any)["id"] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("info: %v", info)
	}
	if status, _ := c.call("GET", "/api/0/organizations/kentos/releases/nope/", nil); status != 404 {
		t.Fatalf("unknown release: %d", status)
	}
	if status, _ := c.call("POST", "/api/0/organizations/kentos/releases/", map[string]any{"version": "", "projects": []string{"kalem"}}); status != 400 {
		t.Fatalf("empty version: %d", status)
	}
	var commit, repo string
	e.Scalar(t, &commit, `SELECT commit_sha || ' ' || repository FROM releases WHERE version = 'kalem-web@5.1.0'`)
	if commit, repo, _ = strings.Cut(commit, " "); commit != "0123456789abcdef0123456789abcdef01234567" || repo != "kentos/kalem" {
		t.Fatalf("stored release: %s %s", commit, repo)
	}
	if status, body := c.call("GET", "/api/0/organizations/kentos/repos/", nil); status != 200 || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("repos: %d %s", status, body)
	}
}
