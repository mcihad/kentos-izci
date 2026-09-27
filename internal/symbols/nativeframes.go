package symbols

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols/native"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// image is a native image of an event with a parsed load address.
type image struct {
	addr    uint64
	size    uint64
	debugID uuid.UUID
	file    string
}

// nativeImages returns the ELF and Mach-O images of an event.
func nativeImages(ev *ingestv1.Event) []image {
	var out []image
	for _, im := range ev.GetDebugImages() {
		switch lower(im.GetType()) {
		case "elf", "macho":
		default:
			continue
		}
		addr, err := parseAddr(im.GetImageAddr())
		if err != nil {
			continue
		}
		id, err := uuid.Parse(im.GetDebugId())
		if err != nil {
			continue
		}
		out = append(out, image{addr: addr, size: im.GetImageSize(), debugID: id, file: im.GetCodeFile()})
	}
	return out
}

func parseAddr(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "0x"); ok {
		return strconv.ParseUint(rest, 16, 64)
	}
	return strconv.ParseUint(s, 10, 64)
}

// isNativeFrame reports a frame that has only an instruction address, like
// the frames of obfuscated or split-debug-info Flutter builds.
func isNativeFrame(f *ingestv1.Frame) bool {
	return f.GetInstructionAddr() != "" && f.GetLineno() == 0 && f.GetFilename() == "" && f.GetAbsPath() == ""
}

// native resolves the native frames of a stack trace; inlined calls expand
// into several frames. It reports whether any frame changed.
func (p *processor) native(st *ingestv1.Stacktrace) bool {
	images := nativeImages(p.ev)
	var out []*ingestv1.Frame
	changed := false
	for _, f := range st.GetFrames() {
		if !isNativeFrame(f) {
			out = append(out, f)
			continue
		}
		addr, err := parseAddr(f.GetInstructionAddr())
		im := imageAt(images, addr)
		if err != nil || im == nil {
			out = append(out, f)
			continue
		}
		sym := p.symbolizer(im)
		if sym == nil {
			metrics.FramesMissingNative.Inc()
			out = append(out, f)
			continue
		}
		rel := addr - im.addr
		// Stack frames hold return addresses; the call is the instruction
		// before.
		if rel > 0 {
			rel--
		}
		resolved := sym.Lookup(rel)
		if len(resolved) == 0 {
			metrics.FramesMissingNative.Inc()
			out = append(out, f)
			continue
		}
		metrics.FramesResolvedNative.Inc()
		for _, r := range resolved {
			nf := &ingestv1.Frame{Function: r.Function, AbsPath: r.File, Filename: fileName(r.File), Lineno: r.Line,
				Colno: r.Column, InstructionAddr: f.GetInstructionAddr(), Platform: framePlatform(r.File)}
			if nf.Platform == "dart" {
				nf.Module = dartModule(r.File)
			}
			out = append(out, nf)
		}
		changed = true
	}
	if changed {
		st.Frames = out
	}
	return changed
}

// imageAt returns the image an address belongs to: the one loaded closest
// below it (and within its size, when known).
func imageAt(images []image, addr uint64) *image {
	var best *image
	for i := range images {
		im := &images[i]
		if im.addr > addr || (im.size > 0 && addr >= im.addr+im.size) {
			continue
		}
		if best == nil || im.addr > best.addr {
			best = im
		}
	}
	return best
}

// symbolizer returns the debug information index of an image.
func (p *processor) symbolizer(im *image) *native.Symbolizer {
	ref := p.lookup("n:"+im.debugID.String(), func(tx pgx.Tx) (*fileRef, error) {
		f, err := store.DebugFileByDebugID(p.ctx, tx, p.project, im.debugID, []string{string(native.KindELF), string(native.KindMachO)})
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &fileRef{id: f.ID, parent: f.ID, debug: true}, nil
	})
	target := im.debugID.String()
	if ref == nil {
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_DEBUG_FILE_MISSING, target,
			"no debug information file with this debug id (image %s)", im.file)
		return nil
	}
	v, err := p.load(p.ctx, p.org, ref, func(b []byte) (any, int, error) {
		o, err := native.Open(b)
		if err != nil {
			return nil, 0, err
		}
		s, err := o.Symbolizer()
		if err != nil {
			return nil, 0, err
		}
		return s, s.Size(), nil
	})
	if err != nil {
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_DEBUG_FILE_INVALID, target, "%v", err)
		return nil
	}
	p.touch(p.org, ref)
	return v.(*native.Symbolizer)
}

// fileName is the short name of a source file: its last path segment
// ("package:kalem/src/fatura.dart" → "fatura.dart").
func fileName(uri string) string {
	if i := strings.LastIndexByte(uri, '/'); i >= 0 {
		return uri[i+1:]
	}
	if _, rest, ok := strings.Cut(uri, ":"); ok && !strings.Contains(uri, "://") {
		return rest
	}
	return uri
}

func framePlatform(file string) string {
	if strings.HasSuffix(file, ".dart") || strings.HasPrefix(file, "dart:") || strings.HasPrefix(file, "package:") {
		return "dart"
	}
	return "native"
}

// dartModule is the library path of a Dart source ("package:flutter/src/
// widgets/framework.dart" → "package:flutter/src/widgets"), which in-app
// rules match against; local files have none.
func dartModule(file string) string {
	if !strings.HasPrefix(file, "package:") && !strings.HasPrefix(file, "dart:") {
		return ""
	}
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		return file[:i]
	}
	return file
}

// instanceOf matches the obfuscated type in "Instance of 'Ki'".
var instanceOf = regexp.MustCompile(`Instance of '([A-Za-z0-9_$]+)'`)

// deobfuscate restores the exception types of obfuscated Dart builds from
// the uploaded obfuscation map of the app image.
func (p *processor) deobfuscate() {
	var names map[string]string
	for _, im := range nativeImages(p.ev) {
		if names = p.dartMap(im.debugID); names != nil {
			break
		}
	}
	if names == nil {
		return
	}
	for _, ex := range p.ev.GetExceptions() {
		if orig, ok := names[ex.GetType()]; ok {
			ex.Type = orig
		}
		ex.Value = instanceOf.ReplaceAllStringFunc(ex.GetValue(), func(m string) string {
			sub := instanceOf.FindStringSubmatch(m)
			if orig, ok := names[sub[1]]; ok {
				return "Instance of '" + orig + "'"
			}
			return m
		})
	}
}

func (p *processor) dartMap(debugID uuid.UUID) map[string]string {
	ref := p.lookup("dm:"+debugID.String(), func(tx pgx.Tx) (*fileRef, error) {
		f, err := store.DebugFileByDebugID(p.ctx, tx, p.project, debugID, []string{KindDartSymbolMap})
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &fileRef{id: f.ID, parent: f.ID, debug: true}, nil
	})
	if ref == nil {
		return nil
	}
	v, err := p.load(p.ctx, p.org, ref, func(b []byte) (any, int, error) {
		m, err := native.ParseDartSymbolMap(b)
		if err != nil {
			return nil, 0, err
		}
		size := 256
		for k, v := range m {
			size += len(k) + len(v) + 48
		}
		return m, size, nil
	})
	if err != nil {
		p.fail(ingestv1.ProcessingErrorKind_PROCESSING_ERROR_KIND_DEBUG_FILE_INVALID, debugID.String(), "obfuscation map: %v", err)
		return nil
	}
	p.touch(p.org, ref)
	return v.(map[string]string)
}
