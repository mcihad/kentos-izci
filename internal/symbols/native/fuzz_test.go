package native

import (
	"compress/gzip"
	"io"
	"os"
	"testing"
)

// prefix reads up to n bytes of a gzipped fixture: real debug files are
// megabytes, their beginning is a seed that keeps fuzzing fast.
func prefix(name string, n int) []byte {
	f, err := os.Open("testdata/" + name)
	if err != nil {
		return nil
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(zr, int64(n)))
	return b
}

// FuzzOpen feeds arbitrary bytes to the ELF, Mach-O and DWARF readers and
// symbolizes addresses in what they accept.
func FuzzOpen(f *testing.F) {
	for _, name := range []string{"dart-linux-arm64.symbols.gz", "dart-macos-arm64.symbols.gz"} {
		if b := prefix(name, 64<<10); len(b) > 0 {
			f.Add(b)
		}
	}
	f.Add([]byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte("\xcf\xfa\xed\xfe\x0c\x00\x00\x01\x00\x00\x00\x00\x0a\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		o, err := Open(data)
		if err != nil {
			return
		}
		s, err := o.Symbolizer()
		if err != nil {
			return
		}
		for _, pc := range []uint64{0, 1, 0x1000, 0x7fffffff, ^uint64(0)} {
			_ = s.Lookup(pc)
		}
	})
}

// FuzzDartSymbolMap feeds arbitrary bytes to the obfuscation map parser.
func FuzzDartSymbolMap(f *testing.F) {
	if b := prefix("obfuscation.map.json.gz", 4<<10); len(b) > 0 {
		f.Add(b)
	}
	f.Add([]byte(`["MyClass","a","myMethod","b"]`))
	f.Add([]byte(`["odd"]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseDartSymbolMap(data)
	})
}
