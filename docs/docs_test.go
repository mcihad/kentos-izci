package docs

import (
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var (
	linkRe    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	headingRe = regexp.MustCompile(`(?m)^#{1,6} (.+)$`)
	fenceRe   = regexp.MustCompile("(?s)```.*?```")
)

// anchor is GitHub's heading id: lower case, punctuation removed, spaces
// turned into hyphens.
func anchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' || unicode.Is(unicode.Mn, r):
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func anchors(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatch(fenceRe.ReplaceAllString(text, ""), -1) {
		out[anchor(strings.Trim(m[1], "`*"))] = true
	}
	return out
}

// TestLinksResolve keeps the guides free of broken links: links between
// guides must name an existing guide and heading, links into the repository
// an existing file.
func TestLinksResolve(t *testing.T) {
	for _, g := range Guides {
		text, ok := Read(g.Slug)
		if !ok || strings.TrimSpace(text) == "" {
			t.Errorf("guide %s is missing", g.Slug)
			continue
		}
		if strings.HasPrefix(g.Slug, "api/") {
			continue // generated from proto comments
		}
		for _, m := range linkRe.FindAllStringSubmatch(fenceRe.ReplaceAllString(text, ""), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			file, frag, _ := strings.Cut(target, "#")
			if strings.HasPrefix(file, "../") {
				if _, err := os.Stat(file); err != nil {
					t.Errorf("%s: link to %s: %v", g.Slug, target, err)
				}
				continue
			}
			slug := g.Slug
			if file != "" {
				slug = strings.TrimSuffix(path.Join(path.Dir(g.Slug), file), ".md")
			}
			dest, ok := Read(slug)
			if !ok {
				t.Errorf("%s: link to unknown guide %s", g.Slug, target)
				continue
			}
			if frag != "" && !anchors(dest)[frag] {
				t.Errorf("%s: link to missing heading %s", g.Slug, target)
			}
		}
	}
}

func TestLLMs(t *testing.T) {
	index := LLMs("https://izci.example.com/", false)
	if !strings.Contains(index, "(https://izci.example.com/docs/raw/kurulum)") || !strings.Contains(index, "https://izci.example.com/mcp") {
		t.Fatalf("index:\n%s", index)
	}
	full := LLMs("https://izci.example.com", true)
	for _, g := range Guides {
		if !strings.Contains(full, "<!-- "+g.Slug+" -->") {
			t.Errorf("llms-full.txt lacks %s", g.Slug)
		}
	}
	if _, ok := Read("../internal/app/app"); ok {
		t.Fatal("Read accepted a path outside the guides")
	}
}

func TestAnchor(t *testing.T) {
	for heading, want := range map[string]string{
		"2. Veritabanını hazırlayın":      "2-veritabanını-hazırlayın",
		"Yalıtım nasıl sağlanır":          "yalıtım-nasıl-sağlanır",
		"GitHub ve yapay zekâ (opencode)": "github-ve-yapay-zekâ-opencode",
	} {
		if got := anchor(heading); got != want {
			t.Errorf("anchor(%q) = %q, want %q", heading, got, want)
		}
	}
	if anchors("## Güvenlik\n")["guvenlik"] {
		t.Error("anchors must keep Turkish letters")
	}
}
