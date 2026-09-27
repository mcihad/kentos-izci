package ownership

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// TestCodeownersPatterns checks the examples of GitHub's CODEOWNERS
// documentation.
func TestCodeownersPatterns(t *testing.T) {
	for pattern, cases := range map[string]map[string]bool{
		"*":            {"README.md": true, "src/a/b.go": true},
		"*.js":         {"app.js": true, "src/ui/app.js": true, "app.jsx": false},
		"/build/logs/": {"build/logs/a.log": true, "build/logs/x/y.log": true, "src/build/logs/a.log": false, "build/logs": false},
		"docs/*":       {"docs/getting-started.md": true, "docs/build-app/troubleshooting.md": false, "src/docs/a.md": false},
		"apps/":        {"apps/a.go": true, "src/apps/x/y.go": true, "apps.go": false},
		"/docs/":       {"docs/a.md": true, "docs/x/y.md": true, "src/docs/a.md": false},
		"**/logs":      {"build/logs/a": true, "deeply/nested/logs/x/y": true, "logs/a": true, "logs": true, "catalogs/a": false},
		"/apps/github": {"apps/github": true, "apps/github/x.go": true, "apps/githubber": false},
		"src/**/*.cs":  {"src/a.cs": true, "src/Billing/Invoice.cs": true, "lib/src/a.cs": false},
		"/docs/**":     {"docs/a.md": true, "docs/x/y.md": true},
		`\#notes.md`:   {"#notes.md": true},
	} {
		re, err := codeownersRegexp(strings.TrimPrefix(pattern, `\`))
		if err != nil {
			t.Fatalf("%s: %v", pattern, err)
		}
		for path, want := range cases {
			if got := re.MatchString(path); got != want {
				t.Errorf("%s ~ %s = %v, want %v (%s)", pattern, path, got, want, re)
			}
		}
	}
}

func TestParse(t *testing.T) {
	rules, errs := ParseRules(`
# Billing team
path:src/billing/*       ayse@kentos.com.tr @mehmet
module:Kentos.Tahsilat.* #tahsilat
url:*/odeme/*            ayse@kentos.com.tr
tags.customer:sivas      ali@kentos.com.tr

path:src/x/*
unknown:x a@b.co
path:src/y/* not-an-owner
`)
	if len(rules) != 4 || rules[0].Text() != "path:src/billing/*" || rules[3].Tag != "customer" || rules[1].Owners[0] != "#tahsilat" {
		t.Fatalf("rules: %+v", rules)
	}
	if len(errs) != 3 || errs[0].Line != 8 || errs[1].Line != 9 || errs[2].Line != 10 {
		t.Fatalf("errors: %v", errs)
	}

	co, errs := ParseCodeowners(`
# Owners
*                 @kentos/platform
/src/billing/     @ayse ayse@kentos.com.tr # billing
[Frontend]
*.ts              @kentos/web
/src/billing/legacy/
`)
	if len(errs) != 0 || len(co) != 4 || len(co[1].Owners) != 2 || len(co[3].Owners) != 0 || co[1].Line != 4 {
		t.Fatalf("codeowners: %+v %v", co, errs)
	}
}

func dotnetEvent() *ingestv1.Event {
	return &ingestv1.Event{
		Level: ingestv1.Level_LEVEL_ERROR, Environment: "production", Tags: map[string]string{"customer.plan": "kurumsal"},
		Customer: &ingestv1.Customer{Id: "sivas"},
		Request:  &ingestv1.Request{Url: "https://kalem.kentos.com.tr/odeme/tamamla?x=1"},
		Exceptions: []*ingestv1.Exception{{Type: "InvalidOperationException", Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{
			{Module: "Microsoft.AspNetCore.Mvc", Filename: "ControllerActionInvoker.cs", InApp: proto.Bool(false)},
			{Module: "Kentos.Tahsilat.Api", AbsPath: "/app/src/Tahsilat/Api/OdemeController.cs", Filename: "OdemeController.cs", InApp: proto.Bool(true)},
			{Module: "Kentos.Billing.Invoice", AbsPath: `C:\build\src\billing\Invoice.cs`, Filename: "Invoice.cs", Lineno: 42, InApp: proto.Bool(true)},
		}}}},
	}
}

func TestMatch(t *testing.T) {
	rules, errs := ParseRules(`
path:src/billing/*          billing@kentos.com.tr
module:Kentos.Tahsilat.*    #tahsilat
url:*/odeme/*               web@kentos.com.tr
tags.customer:sivas         sivas@kentos.com.tr
tags.level:warning          nobody@kentos.com.tr
path:*.java                 java@kentos.com.tr
`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	co, _ := ParseCodeowners("/src/Tahsilat/ @tahsilat-dev\n/src/billing/ @billing-dev\n")
	rs := &Ruleset{Rules: append(co, rules...)}
	var got []string
	for _, r := range rs.Match(SubjectOf(dotnetEvent())) {
		got = append(got, r.Text())
	}
	want := []string{"codeowners:/src/Tahsilat/", "codeowners:/src/billing/", "path:src/billing/*", "module:Kentos.Tahsilat.*",
		"url:*/odeme/*", "tags.customer:sivas"}
	if !slices.Equal(got, want) {
		t.Fatalf("matches:\n%v\nwant\n%v", got, want)
	}
	if w := Winner(rs.Match(SubjectOf(dotnetEvent()))); w.Text() != "tags.customer:sivas" {
		t.Fatalf("winner: %s", w.Text())
	}

	// Path mappings turn frame paths into repository paths: with one, the
	// mapped path alone counts.
	co, _ = ParseCodeowners("/Tahsilat/ @x\n")
	mapped := &Ruleset{Rules: co, Rewrites: []Rewrite{{From: "/app/src/", To: ""}}}
	if len(mapped.Match(SubjectOf(dotnetEvent()))) != 1 {
		t.Fatal("mapped codeowners path did not match")
	}
	elsewhere := &Ruleset{Rules: co, Rewrites: []Rewrite{{From: "/app/src/", To: "backend/"}}}
	if len(elsewhere.Match(SubjectOf(dotnetEvent()))) != 0 {
		t.Fatal("anchored pattern matched a suffix although a mapping applied")
	}
	// Without a mapping that applies, shorter suffixes are tried.
	unmapped := &Ruleset{Rules: co, Rewrites: []Rewrite{{From: "/srv/", To: ""}}}
	if len(unmapped.Match(SubjectOf(dotnetEvent()))) != 1 {
		t.Fatal("suffix of an unmapped path did not match")
	}
}

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{
		"webpack:///./src/app.ts":                   "/./src/app.ts",
		"app:///src/main.dart":                      "/src/main.dart",
		"https://kalem.example.com/assets/a.js?v=2": "/assets/a.js",
		`C:\src\Billing\Invoice.cs`:                 "C:/src/Billing/Invoice.cs",
		"~/src/invoice.ts":                          "/src/invoice.ts",
	} {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := RepoPaths("/app/src/a/b.cs", nil); !slices.Equal(got, []string{"app/src/a/b.cs", "src/a/b.cs", "a/b.cs", "b.cs"}) {
		t.Errorf("suffixes: %v", got)
	}
}
