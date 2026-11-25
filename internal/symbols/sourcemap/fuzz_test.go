package sourcemap

import (
	"os"
	"testing"
)

// FuzzParse feeds arbitrary bytes to the source map parser (JSON and VLQ
// mappings) and looks tokens up in what it accepts.
func FuzzParse(f *testing.F) {
	if data, err := os.ReadFile("testdata/index.js.map"); err == nil {
		f.Add(data)
	}
	f.Add([]byte(`{"version":3,"sources":["a.js"],"names":["x"],"mappings":"AAAA,SAASA;;ACCb","sourcesContent":["let x = 1;\n"]}`))
	f.Add([]byte(`{"version":3,"sources":["a.js"],"names":[],"mappings":"gggggggg+/,;;A"}`))
	f.Add([]byte(`{"version":3,"sections":[{"offset":{"line":0,"column":0},"map":{"version":3,"sources":["b.js"],"names":[],"mappings":"AAAA"}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data)
		if err != nil {
			return
		}
		for line := uint32(0); line < 4; line++ {
			for col := uint32(0); col < 80; col += 9 {
				if tok, ok := m.Lookup(line, col); ok {
					_ = m.Name(tok)
					_ = m.Source(tok)
					_ = m.Ignored(tok)
					_, _, _, _ = m.Context(tok, 3)
				}
			}
		}
	})
}
