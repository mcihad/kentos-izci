package native

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"
)

// The fixtures come from testdata/main.dart.txt compiled with Dart 3.12:
//
//	dart compile exe bin/main.dart -S app.symbols \
//	  --extra-gen-snapshot-options=--obfuscate,--save-obfuscation-map=map.json
//
// once for macOS (a Mach-O dSYM companion) and once with
// --target-os=linux --target-arch=arm64 (an ELF file).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMachODart(t *testing.T) {
	o, err := Open(fixture(t, "dart-macos-arm64.symbols.gz"))
	if err != nil {
		t.Fatal(err)
	}
	// The macOS run printed build_id 'b4485c2e60857b0997e466d02ec92702';
	// the Dart SDK reports it as the debug id of a "macho" image.
	if o.Kind != KindMachO || o.DebugID != "b4485c2e-6085-7b09-97e4-66d02ec92702" || o.CodeID != "b4485c2e60857b0997e466d02ec92702" ||
		o.Arch != "arm64" || !o.Debug {
		t.Fatalf("object: %+v", o)
	}
	s, err := o.Symbolizer()
	if err != nil {
		t.Fatal(err)
	}
	// Frames #00-#02 of that run ("virt" = instruction address minus
	// isolate_dso_base); return addresses, so look up the call instruction.
	for _, c := range []struct {
		addr uint64
		fn   string
		line uint32
	}{
		{0x44d3f, "InvoiceService.total", 11},
		{0x44cc7, "computeTotals", 18},
		{0x5005f, "main", 22},
	} {
		frames := s.Lookup(c.addr - 1)
		if len(frames) == 0 {
			t.Fatalf("%#x: not found", c.addr)
		}
		f := frames[len(frames)-1]
		if f.Function != c.fn || f.Line != c.line || !strings.HasSuffix(f.File, "/bin/main.dart") {
			t.Errorf("%#x: %+v, want %s line %d", c.addr, frames, c.fn, c.line)
		}
	}
	if frames := s.Lookup(0xfffffff); len(frames) != 0 {
		t.Errorf("address outside the object resolved: %+v", frames)
	}
	if s.Size() <= 0 {
		t.Error("size")
	}
}

func TestELFDart(t *testing.T) {
	o, err := Open(fixture(t, "dart-linux-arm64.symbols.gz"))
	if err != nil {
		t.Fatal(err)
	}
	// `file` reports BuildID[md5/uuid]=4d0d11e086612bf7cc804c1bc6edf832; the
	// debug id flips the first three fields (little endian object).
	if o.Kind != KindELF || o.CodeID != "4d0d11e086612bf7cc804c1bc6edf832" || o.DebugID != "e0110d4d-6186-f72b-cc80-4c1bc6edf832" ||
		o.Arch != "arm64" || !o.Debug {
		t.Fatalf("object: %+v", o)
	}
	if id, _ := DebugIDFromBuildID(o.CodeID, true); id != o.DebugID {
		t.Fatalf("DebugIDFromBuildID = %s", id)
	}
	s, err := o.Symbolizer()
	if err != nil {
		t.Fatal(err)
	}
	// Without a stack trace from Linux, look up an address inside the
	// function the fixture throws from.
	found := false
	for _, sub := range s.subs {
		if s.name(sub.fn) != "InvoiceService.total" {
			continue
		}
		frames := s.Lookup(sub.low + (sub.high-sub.low)/2)
		f := frames[len(frames)-1]
		if frames[0].Function != "InvoiceService.total" || !strings.HasSuffix(f.File, "/bin/main.dart") || f.Line < 9 || f.Line > 14 {
			t.Fatalf("lookup inside InvoiceService.total: %+v", frames)
		}
		found = true
	}
	if !found {
		t.Fatal("InvoiceService.total not indexed")
	}
}

func TestInlinedFrames(t *testing.T) {
	o, _ := Open(fixture(t, "dart-macos-arm64.symbols.gz"))
	s, _ := o.Symbolizer()
	// Find any address inside an inlined call and check the chain: every
	// outer frame is located at the call site of the next one.
	for _, sub := range s.subs {
		for _, r := range *sub.inlined {
			if r.depth != 1 {
				continue
			}
			frames := s.Lookup(r.low)
			if len(frames) < 2 {
				t.Fatalf("inlined range %#x: %+v", r.low, frames)
			}
			callee := s.funcs[r.fn]
			if frames[0].Line != callee.callLine || frames[1].Function != s.name(r.fn) {
				t.Fatalf("chain %+v, call line %d", frames, callee.callLine)
			}
			return
		}
	}
	t.Fatal("no inlined calls in the fixture")
}

func TestOpenRejects(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("hello"), []byte("\x7fELF garbage"), {0xca, 0xfe, 0xba, 0xbe, 0, 0, 0, 2}} {
		if _, err := Open(in); err == nil {
			t.Errorf("Open(%q) accepted", in)
		}
	}
}

func TestDartSymbolMap(t *testing.T) {
	data := fixture(t, "obfuscation.map.json.gz")
	if !IsDartSymbolMap(data) || IsDartSymbolMap([]byte(`{"version":3}`)) || IsDartSymbolMap([]byte("\x7fELF")) {
		t.Fatal("IsDartSymbolMap")
	}
	m, err := ParseDartSymbolMap(data)
	if err != nil {
		t.Fatal(err)
	}
	// The macOS run printed the runtime type "Ki".
	if m["Ki"] != "InvoiceException" || m["Li"] != "InvoiceService" {
		t.Fatalf("Ki=%q Li=%q", m["Ki"], m["Li"])
	}
	for _, bad := range []string{`["a"]`, `{"a":"b"}`, `[1,2]`} {
		if _, err := ParseDartSymbolMap([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		t.Fatal("fixture")
	}
}
