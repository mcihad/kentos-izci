package symbols

import (
	"errors"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols/sourcemap"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// contextLines is how many source lines surround a resolved line.
const contextLines = 5

// scriptMap is the source map of one script of an event.
type scriptMap struct {
	m *sourcemap.Map
	// mapURL is where the map lives next to the script; sources are
	// relative to it.
	mapURL string
}

// frameURL is the script a JavaScript frame ran in (browser SDKs put the
// URL into filename).
func frameURL(f *ingestv1.Frame) string {
	if f.GetAbsPath() != "" {
		return f.GetAbsPath()
	}
	return f.GetFilename()
}

// isScriptFrame reports a JavaScript frame with a position to resolve.
func isScriptFrame(ev *ingestv1.Event, f *ingestv1.Frame) bool {
	if f.GetLineno() == 0 || f.GetColno() == 0 {
		return false
	}
	url := frameURL(f)
	if url == "" || url == "<anonymous>" || strings.HasPrefix(url, "node:") || strings.HasPrefix(url, "native") {
		return false
	}
	p := f.GetPlatform()
	if p == "" {
		p = ev.GetPlatform()
	}
	return p == "javascript" || p == "node"
}

// javascript resolves the script frames of a stack trace; it reports
// whether any frame changed.
func (p *processor) javascript(st *ingestv1.Stacktrace) bool {
	frames := st.GetFrames()
	type hit struct {
		sm  *scriptMap
		tok sourcemap.Token
	}
	hits := make([]*hit, len(frames))
	found := false
	for i, f := range frames {
		if !isScriptFrame(p.ev, f) {
			continue
		}
		sm := p.scriptMap(frameURL(f))
		if sm == nil {
			metrics.FramesMissingSourceMap.Inc()
			continue
		}
		tok, ok := sm.m.Lookup(f.GetLineno()-1, f.GetColno()-1)
		if !ok {
			metrics.FramesMissingSourceMap.Inc()
			continue
		}
		hits[i] = &hit{sm: sm, tok: tok}
		found = true
		metrics.FramesResolvedSourceMap.Inc()
	}
	if !found {
		return false
	}
	for i, h := range hits {
		if h == nil {
			continue
		}
		f := frames[i]
		abs := sourcemap.Resolve(h.sm.mapURL, h.sm.m.Source(h.tok))
		f.AbsPath = abs
		f.Filename = sourcemap.DisplayPath(abs)
		f.Lineno, f.Colno = h.tok.SrcLine+1, h.tok.SrcCol+1
		// The name a caller used at the call site is the original name of
		// the function this frame runs; minified names are meaningless.
		if i > 0 && hits[i-1] != nil {
			if name := hits[i-1].sm.m.Name(hits[i-1].tok); name != "" {
				f.Function = name
			}
		}
		if pre, line, post, ok := h.sm.m.Context(h.tok, contextLines); ok {
			f.PreContext, f.ContextLine, f.PostContext = pre, line, post
		} else {
			// The SDK's context shows minified code.
			f.PreContext, f.ContextLine, f.PostContext = nil, "", nil
		}
		inApp := !h.sm.m.Ignored(h.tok) && !isLibrarySource(abs)
		f.InApp = &inApp
	}
	return true
}

func isLibrarySource(s string) bool {
	return strings.Contains(s, "node_modules/") || strings.Contains(s, "webpack/bootstrap") ||
		strings.Contains(s, "(webpack)") || strings.Contains(s, "/@vite/") || strings.HasPrefix(s, "webpack/runtime")
}

// scriptMap finds and parses the source map of a script, once per event.
func (p *processor) scriptMap(url string) *scriptMap {
	if sm, ok := p.scripts[url]; ok {
		return sm
	}
	sm := p.findScriptMap(url)
	p.scripts[url] = sm
	return sm
}

func (p *processor) findScriptMap(url string) *scriptMap {
	debugID := p.scriptDebugID(url)
	if debugID != uuid.Nil {
		if ref := p.lookup("d:"+debugID.String(), func(tx pgx.Tx) (*fileRef, error) {
			return findByDebugID(p, tx, debugID)
		}); ref != nil {
			return p.open(ref, url)
		}
	}
	release, dist := p.ev.GetRelease(), p.ev.GetDist()
	if release != "" {
		if ref := p.lookup("u:"+release+"\x00"+dist+"\x00"+url, func(tx pgx.Tx) (*fileRef, error) {
			return findByURL(p, tx, url, release, dist)
		}); ref != nil {
			return p.open(ref, url)
		}
	}
	switch {
	case debugID != uuid.Nil:
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_SOURCE_MAP_MISSING, url, "no source map with debug id %s", debugID)
	case release != "":
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_SOURCE_MAP_MISSING, url, "no source map for release %s", release)
	default:
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_SOURCE_MAP_MISSING, url,
			"the event has neither a debug id for the script nor a release")
	}
	return nil
}

// scriptDebugID returns the debug id the SDK reported for a script.
func (p *processor) scriptDebugID(url string) uuid.UUID {
	bare := sourcemap.StripQuery(url)
	for _, im := range p.ev.GetDebugImages() {
		if lower(im.GetType()) != "sourcemap" {
			continue
		}
		if im.GetCodeFile() == url || sourcemap.StripQuery(im.GetCodeFile()) == bare {
			if id, err := uuid.Parse(im.GetDebugId()); err == nil {
				return id
			}
		}
	}
	return uuid.Nil
}

func (p *processor) open(ref *fileRef, url string) *scriptMap {
	v, err := p.load(p.ctx, p.org, ref, func(b []byte) (any, int, error) {
		m, err := sourcemap.Parse(b)
		if err != nil {
			return nil, 0, err
		}
		return m, m.Size(), nil
	})
	if err != nil {
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_SOURCE_MAP_INVALID, url, "%v", err)
		return nil
	}
	p.touch(p.org, ref)
	mapURL := url + ".map"
	if ref.sourceMap != "" && !strings.HasPrefix(ref.sourceMap, "data:") {
		mapURL = sourcemap.Resolve(url, ref.sourceMap)
	}
	return &scriptMap{m: v.(*sourcemap.Map), mapURL: mapURL}
}

// findByDebugID finds the source map with a debug id and the reference of
// its minified script.
func findByDebugID(p *processor, tx pgx.Tx, id uuid.UUID) (*fileRef, error) {
	m, err := store.ArtifactFileByDebugID(p.ctx, tx, p.project, id, store.ArtifactSourceMap)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ref := &fileRef{id: m.ID, parent: m.BundleID}
	script, err := store.ArtifactFileByDebugID(p.ctx, tx, p.project, id, store.ArtifactMinified)
	switch {
	case err == nil:
		ref.sourceMap = script.SourceMap
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	return ref, nil
}

// findByURL finds the source map of a script uploaded for a release: via
// the minified file stored under the script's URL (with or without host),
// or a map stored next to it.
func findByURL(p *processor, tx pgx.Tx, url, release, dist string) (*fileRef, error) {
	names := candidates(url)
	if len(names) == 0 {
		return nil, nil
	}
	script, err := store.ArtifactFileByURL(p.ctx, tx, p.project, release, dist, names, store.ArtifactMinified)
	switch {
	case errors.Is(err, store.ErrNotFound):
		maps := make([]string, len(names))
		for i, n := range names {
			maps[i] = n + ".map"
		}
		m, err := store.ArtifactFileByURL(p.ctx, tx, p.project, release, dist, maps, store.ArtifactSourceMap)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &fileRef{id: m.ID, parent: m.BundleID}, nil
	case err != nil:
		return nil, err
	}
	if script.DebugID != nil {
		if ref, err := findByDebugID(p, tx, *script.DebugID); err != nil || ref != nil {
			return ref, err
		}
	}
	ref := script.SourceMap
	if ref == "" || strings.HasPrefix(ref, "data:") {
		ref = path.Base(script.URL) + ".map"
	}
	name := sourcemap.Resolve(script.URL, ref)
	m, err := store.ArtifactFileByURL(p.ctx, tx, p.project, release, dist, candidates(name), store.ArtifactSourceMap)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &fileRef{id: m.ID, parent: m.BundleID, sourceMap: script.SourceMap}, nil
}

// candidates are the names a script may be stored under: its URL and its
// host-less "~/path" form (for an absolute file path of a server-side
// script, "~" followed by the path).
func candidates(url string) []string {
	hostless := sourcemap.HostlessName(url)
	if hostless == "" && strings.HasPrefix(url, "/") {
		hostless = "~" + sourcemap.StripQuery(url)
	}
	var out []string
	for _, n := range []string{sourcemap.StripQuery(url), hostless} {
		if n != "" && (len(out) == 0 || out[0] != n) {
			out = append(out, n)
		}
	}
	return out
}
