// Command protoc-gen-izcidoc is a protoc plugin that renders the izci
// protobuf contracts as Markdown API reference pages (docs/api).
//
// It writes one page per proto package plus an index (README.md). Leading
// comments become the documentation, protovalidate rules become a
// "Constraints" column, and every type reference links to its definition.
// Run it through buf with `strategy: all` so that it sees every file at once.
package main

import (
	"fmt"
	"slices"
	"strings"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		return generate(gen)
	})
}

type pkgDoc struct {
	name     string
	comment  string
	files    []*protogen.File
	services []*protogen.Service
	messages []*protogen.Message
	enums    []*protogen.Enum
}

func generate(gen *protogen.Plugin) error {
	pkgs := map[string]*pkgDoc{}
	var order []string
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		name := string(f.Desc.Package())
		p, ok := pkgs[name]
		if !ok {
			p = &pkgDoc{name: name}
			pkgs[name] = p
			order = append(order, name)
		}
		p.files = append(p.files, f)
		if c := clean(f.Desc.SourceLocations().ByPath(protoreflect.SourcePath{12}).LeadingComments); c != "" && p.comment == "" {
			// Path 12 is the syntax statement; package docs sit above it or
			// above the package statement (path 2).
			p.comment = c
		}
		if c := clean(f.Desc.SourceLocations().ByPath(protoreflect.SourcePath{2}).LeadingComments); c != "" {
			p.comment = c
		}
		p.services = append(p.services, f.Services...)
		for _, m := range f.Messages {
			p.messages = append(p.messages, flattenMessages(m)...)
		}
		p.enums = append(p.enums, f.Enums...)
		for _, m := range f.Messages {
			p.enums = append(p.enums, nestedEnums(m)...)
		}
	}
	slices.Sort(order)

	for _, name := range order {
		p := pkgs[name]
		slices.SortFunc(p.messages, func(a, b *protogen.Message) int { return strings.Compare(a.GoIdent.GoName, b.GoIdent.GoName) })
		slices.SortFunc(p.enums, func(a, b *protogen.Enum) int { return strings.Compare(a.GoIdent.GoName, b.GoIdent.GoName) })
		out := gen.NewGeneratedFile(name+".md", "")
		renderPackage(out, p)
	}

	idx := gen.NewGeneratedFile("README.md", "")
	idx.P("# izci API reference")
	idx.P()
	idx.P("> Generated from `proto/` by `make gen` (tools/protoc-gen-izcidoc). Do not edit by hand.")
	idx.P()
	idx.P("| Package | Services | Summary |")
	idx.P("|---|---|---|")
	for _, name := range order {
		p := pkgs[name]
		var svcs []string
		for _, s := range p.services {
			svcs = append(svcs, fmt.Sprintf("[%s](%s.md#%s)", s.Desc.Name(), name, anchor(s.Desc.FullName())))
		}
		idx.P(fmt.Sprintf("| [`%s`](%s.md) | %s | %s |", name, name, strings.Join(svcs, ", "), cell(firstSentence(p.comment))))
	}
	return nil
}

func flattenMessages(m *protogen.Message) []*protogen.Message {
	if m.Desc.IsMapEntry() {
		return nil
	}
	out := []*protogen.Message{m}
	for _, n := range m.Messages {
		out = append(out, flattenMessages(n)...)
	}
	return out
}

func nestedEnums(m *protogen.Message) []*protogen.Enum {
	out := slices.Clone(m.Enums)
	for _, n := range m.Messages {
		out = append(out, nestedEnums(n)...)
	}
	return out
}

func renderPackage(g *protogen.GeneratedFile, p *pkgDoc) {
	g.P("# ", p.name)
	g.P()
	var files []string
	for _, f := range p.files {
		files = append(files, "`"+f.Desc.Path()+"`")
	}
	g.P("> Generated from ", strings.Join(files, ", "), " — do not edit by hand.")
	g.P()
	if p.comment != "" {
		g.P(p.comment)
		g.P()
	}

	g.P("## Contents")
	g.P()
	if len(p.services) > 0 {
		g.P("**Services:** ", joinLinks(len(p.services), func(i int) protoreflect.FullName { return p.services[i].Desc.FullName() }))
		g.P()
	}
	if len(p.messages) > 0 {
		g.P("**Messages:** ", joinLinks(len(p.messages), func(i int) protoreflect.FullName { return p.messages[i].Desc.FullName() }))
		g.P()
	}
	if len(p.enums) > 0 {
		g.P("**Enums:** ", joinLinks(len(p.enums), func(i int) protoreflect.FullName { return p.enums[i].Desc.FullName() }))
		g.P()
	}

	if len(p.services) > 0 {
		g.P("## Services")
		g.P()
		for _, s := range p.services {
			renderService(g, p.name, s)
		}
	}
	if len(p.messages) > 0 {
		g.P("## Messages")
		g.P()
		for _, m := range p.messages {
			renderMessage(g, p.name, m)
		}
	}
	if len(p.enums) > 0 {
		g.P("## Enums")
		g.P()
		for _, e := range p.enums {
			renderEnum(g, e)
		}
	}
}

func joinLinks(n int, name func(int) protoreflect.FullName) string {
	parts := make([]string, n)
	for i := range n {
		parts[i] = fmt.Sprintf("[%s](#%s)", shortName(name(i)), anchor(name(i)))
	}
	return strings.Join(parts, " · ")
}

func renderService(g *protogen.GeneratedFile, pkg string, s *protogen.Service) {
	heading(g, 3, s.Desc.FullName(), string(s.Desc.Name()))
	if c := clean(string(s.Comments.Leading)); c != "" {
		g.P(c)
		g.P()
	}
	g.P("| Method | Request | Response | Summary |")
	g.P("|---|---|---|---|")
	for _, m := range s.Methods {
		resp := typeLink(pkg, m.Output.Desc.FullName())
		if m.Desc.IsStreamingServer() {
			resp = "stream " + resp
		}
		req := typeLink(pkg, m.Input.Desc.FullName())
		if m.Desc.IsStreamingClient() {
			req = "stream " + req
		}
		g.P(fmt.Sprintf("| [%s](#%s) | %s | %s | %s |", m.Desc.Name(), anchor(m.Desc.FullName()), req, resp, cell(firstSentence(clean(string(m.Comments.Leading))))))
	}
	g.P()
	for _, m := range s.Methods {
		heading(g, 4, m.Desc.FullName(), string(s.Desc.Name())+"."+string(m.Desc.Name()))
		g.P(fmt.Sprintf("`rpc %s(%s) returns (%s%s)`", m.Desc.Name(), m.Input.Desc.Name(), streamPrefix(m.Desc.IsStreamingServer()), m.Output.Desc.Name()))
		g.P()
		if c := clean(string(m.Comments.Leading)); c != "" {
			g.P(c)
			g.P()
		}
		if opts, ok := m.Desc.Options().(*descriptorpb.MethodOptions); ok && opts.GetIdempotencyLevel() == descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
			g.P("_Side-effect free: may be called with HTTP GET over the Connect protocol._")
			g.P()
		}
	}
}

func streamPrefix(stream bool) string {
	if stream {
		return "stream "
	}
	return ""
}

func renderMessage(g *protogen.GeneratedFile, pkg string, m *protogen.Message) {
	heading(g, 3, m.Desc.FullName(), string(m.Desc.Name()))
	if c := clean(string(m.Comments.Leading)); c != "" {
		g.P(c)
		g.P()
	}
	if len(m.Fields) == 0 {
		g.P("_No fields._")
		g.P()
		return
	}
	hasRules := false
	for _, f := range m.Fields {
		if rules(f) != "" {
			hasRules = true
			break
		}
	}
	if hasRules {
		g.P("| Field | Type | Description | Constraints |")
		g.P("|---|---|---|---|")
	} else {
		g.P("| Field | Type | Description |")
		g.P("|---|---|---|")
	}
	for _, f := range m.Fields {
		desc := cell(clean(string(f.Comments.Leading)))
		if f.Oneof != nil && !f.Desc.HasOptionalKeyword() {
			desc = fmt.Sprintf("_oneof `%s`._ %s", f.Oneof.Desc.Name(), desc)
		}
		row := fmt.Sprintf("| `%s` | %s | %s |", f.Desc.Name(), fieldType(pkg, f), desc)
		if hasRules {
			row = fmt.Sprintf("%s %s |", row, cell(rules(f)))
		}
		g.P(row)
	}
	g.P()
}

func renderEnum(g *protogen.GeneratedFile, e *protogen.Enum) {
	heading(g, 3, e.Desc.FullName(), string(e.Desc.Name()))
	if c := clean(string(e.Comments.Leading)); c != "" {
		g.P(c)
		g.P()
	}
	g.P("| Name | Number | Description |")
	g.P("|---|---|---|")
	for _, v := range e.Values {
		g.P(fmt.Sprintf("| `%s` | %d | %s |", v.Desc.Name(), v.Desc.Number(), cell(clean(string(v.Comments.Leading)))))
	}
	g.P()
}

func heading(g *protogen.GeneratedFile, level int, full protoreflect.FullName, title string) {
	g.P(fmt.Sprintf(`<a id="%s"></a>`, anchor(full)))
	g.P()
	g.P(strings.Repeat("#", level), " ", title)
	g.P()
}

func fieldType(pkg string, f *protogen.Field) string {
	if f.Desc.IsMap() {
		k := scalarOrLink(pkg, f.Message.Fields[0])
		v := scalarOrLink(pkg, f.Message.Fields[1])
		return fmt.Sprintf("map&lt;%s, %s&gt;", k, v)
	}
	t := scalarOrLink(pkg, f)
	switch {
	case f.Desc.IsList():
		return "repeated " + t
	case f.Desc.HasOptionalKeyword():
		return "optional " + t
	}
	return t
}

func scalarOrLink(pkg string, f *protogen.Field) string {
	switch f.Desc.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return typeLink(pkg, f.Message.Desc.FullName())
	case protoreflect.EnumKind:
		return typeLink(pkg, f.Enum.Desc.FullName())
	default:
		return "`" + f.Desc.Kind().String() + "`"
	}
}

var wellKnown = map[string]string{
	"google.protobuf.Timestamp": "https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp",
	"google.protobuf.Duration":  "https://protobuf.dev/reference/protobuf/google.protobuf/#duration",
	"google.protobuf.Struct":    "https://protobuf.dev/reference/protobuf/google.protobuf/#struct",
	"google.protobuf.Value":     "https://protobuf.dev/reference/protobuf/google.protobuf/#value",
}

func typeLink(pkg string, full protoreflect.FullName) string {
	s := string(full)
	if url, ok := wellKnown[s]; ok {
		return fmt.Sprintf("[%s](%s)", s, url)
	}
	owner := string(full.Parent())
	for strings.Count(owner, ".") > 2 && !strings.HasPrefix(owner, "izci.") {
		owner = string(protoreflect.FullName(owner).Parent())
	}
	name := shortName(full)
	if strings.HasPrefix(s, pkg+".") {
		return fmt.Sprintf("[%s](#%s)", name, anchor(full))
	}
	if strings.HasPrefix(s, "izci.") {
		target := s[:strings.LastIndex(s, ".")]
		return fmt.Sprintf("[%s](%s.md#%s)", s, target, anchor(full))
	}
	return "`" + s + "`"
}

func shortName(full protoreflect.FullName) string {
	s := string(full)
	// Drop the package (three segments for izci packages, e.g. izci.api.v1).
	parts := strings.Split(s, ".")
	if len(parts) > 3 && parts[0] == "izci" {
		return strings.Join(parts[3:], ".")
	}
	return string(full.Name())
}

func anchor(full protoreflect.FullName) string {
	return strings.ToLower(strings.ReplaceAll(string(full), ".", "-"))
}

// rules renders protovalidate constraints of a field in a compact form.
func rules(f *protogen.Field) string {
	opts, ok := f.Desc.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, validate.E_Field) {
		return ""
	}
	fr, _ := proto.GetExtension(opts, validate.E_Field).(*validate.FieldRules)
	if fr == nil {
		return ""
	}
	var out []string
	if fr.GetRequired() {
		out = append(out, "required")
	}
	if s := fr.GetString(); s != nil {
		out = append(out, stringRules(s)...)
	}
	if u := fr.GetUint32(); u != nil {
		if u.HasLte() {
			out = append(out, fmt.Sprintf("≤ %d", u.GetLte()))
		}
	}
	if e := fr.GetEnum(); e != nil {
		if e.GetDefinedOnly() {
			out = append(out, "defined values only")
		}
		if len(e.GetNotIn()) > 0 {
			out = append(out, "must be set")
		}
	}
	if r := fr.GetRepeated(); r != nil {
		if r.HasMinItems() {
			out = append(out, fmt.Sprintf("min %d items", r.GetMinItems()))
		}
		if r.HasMaxItems() {
			out = append(out, fmt.Sprintf("max %d items", r.GetMaxItems()))
		}
		if items := r.GetItems(); items != nil && items.GetString() != nil {
			for _, s := range stringRules(items.GetString()) {
				out = append(out, "each "+s)
			}
		}
	}
	return strings.Join(out, ", ")
}

func stringRules(s *validate.StringRules) []string {
	var out []string
	if s.HasMinLen() {
		out = append(out, fmt.Sprintf("min length %d", s.GetMinLen()))
	}
	if s.HasMaxLen() {
		out = append(out, fmt.Sprintf("max length %d", s.GetMaxLen()))
	}
	if s.HasPattern() {
		out = append(out, "pattern `"+s.GetPattern()+"`")
	}
	switch {
	case s.GetEmail():
		out = append(out, "email")
	case s.GetUuid():
		out = append(out, "UUID")
	case s.GetUri():
		out = append(out, "URI")
	case s.GetHostname():
		out = append(out, "hostname")
	}
	return out
}

// clean normalizes a proto comment: trims the single leading space of each
// line and surrounding blank lines.
func clean(c string) string {
	lines := strings.Split(strings.TrimRight(c, "\n"), "\n")
	for i, l := range lines {
		l = strings.TrimPrefix(l, " ")
		// Go-doc style "# Heading" lines would break the page hierarchy;
		// render them as bold paragraph titles instead.
		if strings.HasPrefix(l, "# ") {
			l = "**" + strings.TrimSpace(l[2:]) + "**"
		}
		lines[i] = l
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// cell renders a comment inside a Markdown table cell: paragraphs and list
// items become <br> breaks, hard-wrapped lines are joined.
func cell(c string) string {
	if c == "" {
		return ""
	}
	var b strings.Builder
	lines := strings.Split(c, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			if i > 0 && i < len(lines)-1 {
				b.WriteString("<br><br>")
			}
			continue
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			if b.Len() > 0 {
				b.WriteString("<br>")
			}
			b.WriteString("• " + t[2:])
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "<br>") {
			b.WriteString(" ")
		}
		b.WriteString(t)
	}
	return strings.ReplaceAll(b.String(), "|", "\\|")
}

func firstSentence(c string) string {
	c = strings.Join(strings.Fields(c), " ")
	if i := strings.Index(c, ". "); i >= 0 {
		return c[:i+1]
	}
	return c
}
