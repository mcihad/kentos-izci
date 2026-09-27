// Package sourcemap reads JavaScript source maps (revision 3, including
// index maps with sections) and maps positions in generated code back to the
// original sources.
package sourcemap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Token is one mapping: a position in the generated file and, when Source is
// not negative, the original position it came from. Lines and columns are
// zero-based.
type Token struct {
	GenLine, GenCol uint32
	Source          int32
	SrcLine, SrcCol uint32
	Name            int32
}

// Map is a parsed source map.
type Map struct {
	// File is the generated file the map describes, if recorded.
	File string
	// DebugID is the map's debug id ("debug_id" or "debugId"), if any.
	DebugID string
	// Sources are the original file names with sourceRoot applied.
	Sources []string
	// Names are the original identifiers referenced by tokens.
	Names []string

	contents []*string
	ignored  []bool
	tokens   []Token

	mu    sync.Mutex
	lines map[int32][]string
}

type rawMap struct {
	Version        int          `json:"version"`
	File           string       `json:"file"`
	SourceRoot     string       `json:"sourceRoot"`
	Sources        []*string    `json:"sources"`
	SourcesContent []*string    `json:"sourcesContent"`
	Names          []string     `json:"names"`
	Mappings       string       `json:"mappings"`
	IgnoreList     []int        `json:"ignoreList"`
	GoogleIgnore   []int        `json:"x_google_ignoreList"`
	DebugID        string       `json:"debug_id"`
	DebugIDCamel   string       `json:"debugId"`
	Sections       []rawSection `json:"sections"`
}

type rawSection struct {
	Offset struct {
		Line   uint32 `json:"line"`
		Column uint32 `json:"column"`
	} `json:"offset"`
	URL string  `json:"url"`
	Map *rawMap `json:"map"`
}

// ErrNotSourceMap means the data is not a revision 3 source map.
var ErrNotSourceMap = errors.New("not a source map")

// Parse reads a source map. Index maps are flattened; sections that refer to
// external maps by URL are not supported.
func Parse(data []byte) (*Map, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	// Some servers prefix JSON with an XSSI guard.
	if bytes.HasPrefix(data, []byte(")]}'")) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	var raw rawMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSourceMap, err)
	}
	if raw.Version != 3 {
		return nil, fmt.Errorf("%w: version %d", ErrNotSourceMap, raw.Version)
	}
	m := &Map{File: raw.File, DebugID: firstNonEmpty(raw.DebugID, raw.DebugIDCamel)}
	if len(raw.Sections) > 0 {
		for _, s := range raw.Sections {
			if s.Map == nil {
				return nil, errors.New("index map sections with a url are not supported")
			}
			if err := m.add(s.Map, s.Offset.Line, s.Offset.Column); err != nil {
				return nil, err
			}
		}
	} else if err := m.add(&raw, 0, 0); err != nil {
		return nil, err
	}
	// Tokens of a regular map are already sorted; sections may overlap in
	// odd inputs, so sort defensively (stable, cheap when sorted).
	if !sort.SliceIsSorted(m.tokens, m.less) {
		sort.SliceStable(m.tokens, m.less)
	}
	return m, nil
}

func (m *Map) less(i, j int) bool {
	a, b := m.tokens[i], m.tokens[j]
	return a.GenLine < b.GenLine || a.GenLine == b.GenLine && a.GenCol < b.GenCol
}

// add appends a (section) map whose generated positions start at line/col.
func (m *Map) add(raw *rawMap, line, col uint32) error {
	if raw.Version != 0 && raw.Version != 3 {
		return fmt.Errorf("%w: version %d", ErrNotSourceMap, raw.Version)
	}
	srcBase, nameBase := int32(len(m.Sources)), int32(len(m.Names))
	root := raw.SourceRoot
	if root != "" && !strings.HasSuffix(root, "/") {
		root += "/"
	}
	for i, s := range raw.Sources {
		name := ""
		if s != nil {
			name = *s
			if root != "" && !isAbsolute(name) {
				name = root + name
			}
		}
		m.Sources = append(m.Sources, name)
		var content *string
		if i < len(raw.SourcesContent) {
			content = raw.SourcesContent[i]
		}
		m.contents = append(m.contents, content)
		m.ignored = append(m.ignored, false)
	}
	for _, list := range [][]int{raw.IgnoreList, raw.GoogleIgnore} {
		for _, i := range list {
			if i >= 0 && i < len(raw.Sources) {
				m.ignored[int(srcBase)+i] = true
			}
		}
	}
	m.Names = append(m.Names, raw.Names...)
	tokens, err := decodeMappings(raw.Mappings, len(raw.Sources), len(raw.Names))
	if err != nil {
		return err
	}
	for _, t := range tokens {
		if t.GenLine == 0 {
			t.GenCol += col
		}
		t.GenLine += line
		if t.Source >= 0 {
			t.Source += srcBase
		}
		if t.Name >= 0 {
			t.Name += nameBase
		}
		m.tokens = append(m.tokens, t)
	}
	return nil
}

// Tokens returns the number of mappings.
func (m *Map) Tokens() int { return len(m.tokens) }

// Lookup returns the mapping of a zero-based generated position: the last
// token at or before it. ok is false before the first token or when that
// token has no original position.
func (m *Map) Lookup(line, col uint32) (Token, bool) {
	i := sort.Search(len(m.tokens), func(i int) bool {
		t := m.tokens[i]
		return t.GenLine > line || t.GenLine == line && t.GenCol > col
	})
	if i == 0 {
		return Token{}, false
	}
	t := m.tokens[i-1]
	return t, t.Source >= 0
}

// Name returns the original identifier of a token, or "".
func (m *Map) Name(t Token) string {
	if t.Name < 0 || int(t.Name) >= len(m.Names) {
		return ""
	}
	return m.Names[t.Name]
}

// Source returns the original file name of a token, or "".
func (m *Map) Source(t Token) string {
	if t.Source < 0 || int(t.Source) >= len(m.Sources) {
		return ""
	}
	return m.Sources[t.Source]
}

// Ignored reports whether a token's source is on the map's ignore list
// (third-party code, per the bundler).
func (m *Map) Ignored(t Token) bool {
	return t.Source >= 0 && int(t.Source) < len(m.ignored) && m.ignored[t.Source]
}

// HasContent reports whether the map embeds the text of a token's source.
func (m *Map) HasContent(t Token) bool {
	return t.Source >= 0 && int(t.Source) < len(m.contents) && m.contents[t.Source] != nil
}

// Context returns the source line of a token and up to n lines before and
// after it, when the map embeds the source text.
func (m *Map) Context(t Token, n int) (pre []string, line string, post []string, ok bool) {
	lines := m.sourceLines(t.Source)
	if lines == nil || int(t.SrcLine) >= len(lines) {
		return nil, "", nil, false
	}
	i := int(t.SrcLine)
	pre = append([]string(nil), lines[max(0, i-n):i]...)
	post = append([]string(nil), lines[i+1:min(len(lines), i+1+n)]...)
	return pre, lines[i], post, true
}

func (m *Map) sourceLines(src int32) []string {
	if src < 0 || int(src) >= len(m.contents) || m.contents[src] == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.lines[src]; ok {
		return l
	}
	if m.lines == nil {
		m.lines = map[int32][]string{}
	}
	text := strings.ReplaceAll(*m.contents[src], "\r\n", "\n")
	l := strings.Split(text, "\n")
	m.lines[src] = l
	return l
}

// Size estimates the memory a parsed map holds, for cache accounting.
func (m *Map) Size() int {
	n := len(m.tokens)*20 + 256
	for i, s := range m.Sources {
		n += len(s) + 16
		if c := m.contents[i]; c != nil {
			n += len(*c)
		}
	}
	for _, s := range m.Names {
		n += len(s) + 16
	}
	return n
}

func isAbsolute(s string) bool {
	return strings.HasPrefix(s, "/") || strings.Contains(s, "://")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// decodeMappings decodes the "mappings" string: lines separated by ';',
// segments by ',', each segment one to five base64 VLQ fields relative to
// the previous segment (the generated column only within a line).
func decodeMappings(s string, nSources, nNames int) ([]Token, error) {
	tokens := make([]Token, 0, strings.Count(s, ",")+strings.Count(s, ";")/2+1)
	var line, col uint32
	var src, srcLine, srcCol, name int64
	var fields [5]int64
	for i := 0; i < len(s); {
		switch s[i] {
		case ';':
			line++
			col = 0
			i++
			continue
		case ',':
			i++
			continue
		}
		n := 0
		for i < len(s) && s[i] != ',' && s[i] != ';' {
			if n == 5 {
				return nil, fmt.Errorf("mappings: segment with more than 5 fields at line %d", line)
			}
			v, next, err := vlq(s, i)
			if err != nil {
				return nil, err
			}
			fields[n] = v
			n++
			i = next
		}
		c := int64(col) + fields[0]
		if c < 0 || c > 1<<31 {
			return nil, fmt.Errorf("mappings: invalid column at line %d", line)
		}
		col = uint32(c)
		t := Token{GenLine: line, GenCol: col, Source: -1, Name: -1}
		switch n {
		case 1:
		case 4, 5:
			src += fields[1]
			srcLine += fields[2]
			srcCol += fields[3]
			if src < 0 || int(src) >= nSources || srcLine < 0 || srcCol < 0 {
				return nil, fmt.Errorf("mappings: segment out of range at line %d", line)
			}
			t.Source, t.SrcLine, t.SrcCol = int32(src), uint32(srcLine), uint32(srcCol)
			if n == 5 {
				name += fields[4]
				if name < 0 || int(name) >= nNames {
					return nil, fmt.Errorf("mappings: name out of range at line %d", line)
				}
				t.Name = int32(name)
			}
		default:
			return nil, fmt.Errorf("mappings: segment with %d fields at line %d", n, line)
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

var base64Value = func() (t [256]int8) {
	for i := range t {
		t[i] = -1
	}
	for i, c := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/" {
		t[c] = int8(i)
	}
	return t
}()

// vlq decodes one base64 VLQ value starting at s[i].
func vlq(s string, i int) (int64, int, error) {
	var v int64
	shift := uint(0)
	for {
		if i >= len(s) {
			return 0, i, errors.New("mappings: truncated value")
		}
		d := base64Value[s[i]]
		if d < 0 {
			return 0, i, fmt.Errorf("mappings: invalid character %q", s[i])
		}
		i++
		v |= int64(d&31) << shift
		if d&32 == 0 {
			break
		}
		shift += 5
		if shift > 60 {
			return 0, i, errors.New("mappings: value too large")
		}
	}
	if v&1 == 1 {
		return -(v >> 1), i, nil
	}
	return v >> 1, i, nil
}
