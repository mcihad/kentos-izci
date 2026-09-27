// Package native reads debug information files of natively compiled code —
// ELF and Mach-O objects with DWARF, such as the split debug info of Flutter
// and Dart AOT builds — and resolves instruction addresses to functions,
// files and lines, including inlined calls.
package native

import (
	"bytes"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Kind is the object file format.
type Kind string

// Object file formats.
const (
	KindELF   Kind = "elf"
	KindMachO Kind = "macho"
)

// ErrUnsupported means the data is not an ELF or Mach-O object.
var ErrUnsupported = errors.New("not an ELF or Mach-O object file")

// Object is a parsed debug information file.
type Object struct {
	Kind Kind
	// DebugID identifies the object like Sentry does: the Mach-O UUID, or
	// the GNU build id of an ELF file with its first three fields in big
	// endian order.
	DebugID string
	// CodeID is the build id (ELF) or UUID (Mach-O) in hex.
	CodeID string
	// Arch is the CPU architecture, e.g. "arm64" or "x86_64".
	Arch string
	// Debug reports DWARF debug information (functions, files, lines).
	Debug bool
	// Symbols reports a symbol table.
	Symbols bool

	base  uint64
	dwarf *dwarf.Data
}

// Open parses an object file held in memory.
func Open(data []byte) (*Object, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\x7fELF")):
		return openELF(data)
	case len(data) >= 4 && isMachO(binary.LittleEndian.Uint32(data)):
		return openMachO(data)
	case len(data) >= 4 && (binary.BigEndian.Uint32(data) == macho.MagicFat):
		return nil, errors.New("universal (fat) Mach-O files are not supported; upload the file of each architecture")
	}
	return nil, ErrUnsupported
}

func isMachO(magic uint32) bool {
	switch magic {
	case macho.Magic32, macho.Magic64, 0xcefaedfe, 0xcffaedfe:
		return true
	}
	return false
}

func openELF(data []byte) (*Object, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ELF: %w", err)
	}
	o := &Object{Kind: KindELF, Arch: elfArch(f.Machine)}
	if s := f.Section(".note.gnu.build-id"); s != nil {
		if b, err := s.Data(); err == nil {
			if id := noteDesc(b, f.ByteOrder, 3, "GNU"); len(id) > 0 {
				o.CodeID = hex.EncodeToString(id)
				o.DebugID = elfDebugID(id, f.ByteOrder == binary.LittleEndian)
			}
		}
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD {
			o.base = p.Vaddr
			break
		}
	}
	if syms, err := f.Symbols(); err == nil && len(syms) > 0 {
		o.Symbols = true
	}
	if f.Section(".debug_info") != nil || f.Section(".zdebug_info") != nil {
		if d, err := f.DWARF(); err == nil {
			o.dwarf, o.Debug = d, true
		}
	}
	return o, nil
}

func openMachO(data []byte) (*Object, error) {
	f, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("Mach-O: %w", err)
	}
	o := &Object{Kind: KindMachO, Arch: machoArch(f.Cpu, f.SubCpu)}
	for _, l := range f.Loads {
		raw := l.Raw()
		if len(raw) >= 24 && f.ByteOrder.Uint32(raw) == 0x1b { // LC_UUID
			u := raw[8:24]
			o.CodeID = hex.EncodeToString(u)
			o.DebugID = formatUUID(u)
		}
	}
	if seg := f.Segment("__TEXT"); seg != nil {
		o.base = seg.Addr
	}
	if f.Symtab != nil && len(f.Symtab.Syms) > 0 {
		o.Symbols = true
	}
	if f.Section("__debug_info") != nil {
		if d, err := f.DWARF(); err == nil {
			o.dwarf, o.Debug = d, true
		}
	}
	return o, nil
}

// noteDesc returns the descriptor of the first note of the given type and
// owner in an ELF note section.
func noteDesc(b []byte, order binary.ByteOrder, typ uint32, owner string) []byte {
	for len(b) >= 12 {
		namesz, descsz, t := order.Uint32(b), order.Uint32(b[4:]), order.Uint32(b[8:])
		b = b[12:]
		nameEnd := align4(namesz)
		descEnd := nameEnd + align4(descsz)
		if uint64(len(b)) < uint64(descEnd) || descsz > uint32(len(b)) {
			return nil
		}
		name := strings.TrimRight(string(b[:namesz]), "\x00")
		if t == typ && name == owner {
			return b[nameEnd : nameEnd+descsz]
		}
		b = b[descEnd:]
	}
	return nil
}

func align4(n uint32) uint32 { return (n + 3) &^ 3 }

// elfDebugID converts a GNU build id to a debug id: the first 16 bytes
// (zero padded), with the first three UUID fields flipped to big endian
// for little endian objects. This matches Sentry's symbolic and the Dart
// SDK.
func elfDebugID(buildID []byte, littleEndian bool) string {
	var u [16]byte
	copy(u[:], buildID)
	if littleEndian {
		u[0], u[1], u[2], u[3] = u[3], u[2], u[1], u[0]
		u[4], u[5] = u[5], u[4]
		u[6], u[7] = u[7], u[6]
	}
	return formatUUID(u[:])
}

func formatUUID(b []byte) string {
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// DebugIDFromBuildID converts a hex build id, as printed in Dart stack
// traces ("build_id: '…'"), to the debug id of an ELF object.
func DebugIDFromBuildID(buildID string, littleEndian bool) (string, error) {
	b, err := hex.DecodeString(buildID)
	if err != nil {
		return "", err
	}
	return elfDebugID(b, littleEndian), nil
}

func elfArch(m elf.Machine) string {
	switch m {
	case elf.EM_AARCH64:
		return "arm64"
	case elf.EM_ARM:
		return "arm"
	case elf.EM_X86_64:
		return "x86_64"
	case elf.EM_386:
		return "x86"
	case elf.EM_RISCV:
		return "riscv64"
	}
	return strings.ToLower(strings.TrimPrefix(m.String(), "EM_"))
}

func machoArch(c macho.Cpu, sub uint32) string {
	switch c {
	case macho.CpuArm64:
		if sub&0xff == 2 {
			return "arm64e"
		}
		return "arm64"
	case macho.CpuAmd64:
		return "x86_64"
	case macho.CpuArm:
		return "arm"
	case macho.Cpu386:
		return "x86"
	}
	return strings.ToLower(c.String())
}

// Frame is a resolved location.
type Frame struct {
	Function string
	File     string
	Line     uint32
	Column   uint32
}

// Symbolizer resolves addresses of one object.
type Symbolizer struct {
	base  uint64
	files []string
	names []string
	funcs []funcRec
	subs  []subRange
	rows  []lineRow
}

type funcRec struct {
	name     int32 // index into names; -1 unknown
	callFile int32
	callLine uint32
	callCol  uint32
}

type subRange struct {
	low, high uint64
	fn        int32
	inlined   *[]inlRange
}

type inlRange struct {
	low, high uint64
	fn        int32
	depth     int32
}

type lineRow struct {
	addr uint64
	file int32
	line uint32
	col  uint32
	end  bool
}

// Symbolizer indexes the object's DWARF data. It fails without debug
// information.
func (o *Object) Symbolizer() (*Symbolizer, error) {
	if o.dwarf == nil {
		return nil, errors.New("the file has no DWARF debug information")
	}
	s := &Symbolizer{base: o.base}
	nameIdx := map[string]int32{}
	internName := func(n string) int32 {
		if i, ok := nameIdx[n]; ok {
			return i
		}
		i := int32(len(s.names))
		s.names = append(s.names, n)
		nameIdx[n] = i
		return i
	}
	fileIdx := map[string]int32{}
	internFile := func(n string) int32 {
		if i, ok := fileIdx[n]; ok {
			return i
		}
		i := int32(len(s.files))
		s.files = append(s.files, n)
		fileIdx[n] = i
		return i
	}

	// Names of every subprogram DIE by offset, and references to resolve
	// once the whole unit is read (abstract origins may point forward).
	names := map[dwarf.Offset]string{}
	refs := map[dwarf.Offset]dwarf.Offset{}
	type pending struct {
		fn     int32
		origin dwarf.Offset
		name   string
	}
	var unresolved []pending

	r := o.dwarf.Reader()
	for {
		cu, err := r.Next()
		if err != nil {
			return nil, fmt.Errorf("DWARF: %w", err)
		}
		if cu == nil {
			break
		}
		if cu.Tag != dwarf.TagCompileUnit && cu.Tag != dwarf.TagPartialUnit {
			r.SkipChildren()
			continue
		}
		// Line table of the unit.
		var cuFiles []int32
		if lr, err := o.dwarf.LineReader(cu); err == nil && lr != nil {
			var le dwarf.LineEntry
			for {
				if err := lr.Next(&le); err != nil {
					if err != io.EOF {
						return nil, fmt.Errorf("DWARF line table: %w", err)
					}
					break
				}
				file := int32(-1)
				if le.File != nil {
					file = internFile(le.File.Name)
				}
				s.rows = append(s.rows, lineRow{addr: le.Address, file: file, line: uint32(max(le.Line, 0)),
					col: uint32(max(le.Column, 0)), end: le.EndSequence})
			}
			for _, f := range lr.Files() {
				if f == nil {
					cuFiles = append(cuFiles, -1)
				} else {
					cuFiles = append(cuFiles, internFile(f.Name))
				}
			}
		}
		if !cu.Children {
			continue
		}
		// Walk the unit's DIEs; stack holds the enclosing function records.
		type level struct {
			fn    int32
			isFn  bool
			inl   *[]inlRange
			depth int32
		}
		stack := []level{{fn: -1}}
		for {
			e, err := r.Next()
			if err != nil {
				return nil, fmt.Errorf("DWARF: %w", err)
			}
			if e == nil {
				break
			}
			if e.Tag == 0 {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					break
				}
				continue
			}
			top := stack[len(stack)-1]
			cur := level{fn: top.fn, inl: top.inl, depth: top.depth}
			switch e.Tag {
			case dwarf.TagSubprogram, dwarf.TagInlinedSubroutine:
				if n, ok := e.Val(dwarf.AttrName).(string); ok {
					names[e.Offset] = n
				}
				origin, _ := e.Val(dwarf.AttrAbstractOrigin).(dwarf.Offset)
				if origin == 0 {
					origin, _ = e.Val(dwarf.AttrSpecification).(dwarf.Offset)
				}
				if origin != 0 {
					refs[e.Offset] = origin
				}
				ranges, _ := o.dwarf.Ranges(e)
				if len(ranges) == 0 {
					break
				}
				fr := funcRec{name: -1, callFile: -1}
				if e.Tag == dwarf.TagInlinedSubroutine {
					if v, ok := e.Val(dwarf.AttrCallFile).(int64); ok && v >= 0 && int(v) < len(cuFiles) {
						fr.callFile = cuFiles[v]
					}
					if v, ok := e.Val(dwarf.AttrCallLine).(int64); ok && v > 0 {
						fr.callLine = uint32(v)
					}
					if v, ok := e.Val(dwarf.AttrCallColumn).(int64); ok && v > 0 {
						fr.callCol = uint32(v)
					}
				}
				idx := int32(len(s.funcs))
				s.funcs = append(s.funcs, fr)
				name, _ := e.Val(dwarf.AttrName).(string)
				unresolved = append(unresolved, pending{fn: idx, origin: origin, name: name})
				cur.fn, cur.isFn = idx, true
				if e.Tag == dwarf.TagSubprogram || top.inl == nil {
					list := &[]inlRange{}
					for _, rg := range ranges {
						s.subs = append(s.subs, subRange{low: rg[0], high: rg[1], fn: idx, inlined: list})
					}
					cur.inl, cur.depth = list, 0
				} else {
					cur.depth = top.depth + 1
					for _, rg := range ranges {
						*top.inl = append(*top.inl, inlRange{low: rg[0], high: rg[1], fn: idx, depth: cur.depth})
					}
				}
			}
			if e.Children {
				stack = append(stack, cur)
			}
		}
	}

	// Resolve names through abstract origins and specifications.
	resolve := func(off dwarf.Offset) string {
		for range 8 {
			if n, ok := names[off]; ok {
				return n
			}
			next, ok := refs[off]
			if !ok {
				return ""
			}
			off = next
		}
		return ""
	}
	for _, p := range unresolved {
		name := p.name
		if name == "" && p.origin != 0 {
			name = resolve(p.origin)
		}
		if name != "" {
			s.funcs[p.fn].name = internName(name)
		}
	}
	sort.Slice(s.subs, func(i, j int) bool { return s.subs[i].low < s.subs[j].low })
	sort.SliceStable(s.rows, func(i, j int) bool { return s.rows[i].addr < s.rows[j].addr })
	return s, nil
}

// Lookup resolves an address relative to the object's load address (the
// instruction address minus the image address). The result lists the
// function and its inlined callees from the outermost to the innermost, the
// order frames have in a stack trace (oldest first). It is empty when the
// address is not covered by the debug information.
func (s *Symbolizer) Lookup(rel uint64) []Frame {
	pc := rel + s.base
	i := sort.Search(len(s.subs), func(i int) bool { return s.subs[i].low > pc }) - 1
	// Ranges of top-level functions do not overlap, but tolerate producers
	// that emit a few nested ones.
	var sub *subRange
	for j := i; j >= 0 && j >= i-8; j-- {
		if s.subs[j].low <= pc && pc < s.subs[j].high {
			sub = &s.subs[j]
			break
		}
	}
	file, line, col, hasLine := s.line(pc)
	if sub == nil {
		if !hasLine {
			return nil
		}
		return []Frame{{File: s.file(file), Line: line, Column: col}}
	}
	chain := []int32{sub.fn}
	for _, r := range *sub.inlined {
		if r.low <= pc && pc < r.high && int(r.depth) == len(chain) {
			chain = append(chain, r.fn)
		}
	}
	frames := make([]Frame, len(chain))
	for k := len(chain) - 1; k >= 0; k-- {
		f := Frame{Function: s.name(chain[k])}
		if k == len(chain)-1 {
			if hasLine {
				f.File, f.Line, f.Column = s.file(file), line, col
			}
		} else {
			callee := s.funcs[chain[k+1]]
			f.File, f.Line, f.Column = s.file(callee.callFile), callee.callLine, callee.callCol
		}
		frames[k] = f
	}
	return frames
}

func (s *Symbolizer) line(pc uint64) (file int32, line, col uint32, ok bool) {
	i := sort.Search(len(s.rows), func(i int) bool { return s.rows[i].addr > pc }) - 1
	if i < 0 || s.rows[i].end {
		return 0, 0, 0, false
	}
	r := s.rows[i]
	return r.file, r.line, r.col, r.line > 0
}

func (s *Symbolizer) name(fn int32) string {
	if fn < 0 || s.funcs[fn].name < 0 {
		return ""
	}
	return s.names[s.funcs[fn].name]
}

func (s *Symbolizer) file(i int32) string {
	if i < 0 || int(i) >= len(s.files) {
		return ""
	}
	return s.files[i]
}

// Size estimates the memory the index holds, for cache accounting.
func (s *Symbolizer) Size() int {
	n := len(s.rows)*32 + len(s.funcs)*24 + len(s.subs)*40 + 256
	for _, f := range s.files {
		n += len(f) + 16
	}
	for _, f := range s.names {
		n += len(f) + 16
	}
	for _, sub := range s.subs {
		n += len(*sub.inlined) * 8 // shared lists, counted roughly
	}
	return n
}

// ParseDartSymbolMap reads the obfuscation map of a Dart build
// (--save-obfuscation-map, uploaded as "dartsymbolmap"): a JSON array of
// original and obfuscated names in pairs. It returns obfuscated → original.
func ParseDartSymbolMap(data []byte) (map[string]string, error) {
	var list []string
	if err := json.Unmarshal(bytes.TrimSpace(data), &list); err != nil {
		return nil, fmt.Errorf("not a Dart obfuscation map: %w", err)
	}
	if len(list)%2 != 0 {
		return nil, errors.New("not a Dart obfuscation map: odd number of names")
	}
	out := make(map[string]string, len(list)/2)
	for i := 0; i < len(list); i += 2 {
		if list[i+1] != "" && list[i] != list[i+1] {
			out[list[i+1]] = list[i]
		}
	}
	return out, nil
}

// IsDartSymbolMap reports whether data looks like a Dart obfuscation map.
func IsDartSymbolMap(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) >= 2 && data[0] == '[' && (data[1] == '"' || data[1] == ']' || data[1] == ' ' || data[1] == '\n')
}
