package ownership

import "testing"

// FuzzRules feeds arbitrary text to the ownership rule and CODEOWNERS
// parsers and matches what they accept against an event.
func FuzzRules(f *testing.F) {
	for _, text := range []string{
		"path:src/billing/*          billing@kentos.com.tr\nmodule:Kentos.Tahsilat.*    #tahsilat\nurl:*/odeme/*  web@kentos.com.tr\n",
		"tags.customer:sivas sivas@kentos.com.tr\npath:**/*.java java@kentos.com.tr\n# comment\n",
		"/src/Tahsilat/ @tahsilat-dev\n/src/billing/ @billing-dev\n*.cs @dotnet\n[Section]\n",
		"path:[a-z ayse@example.com\n", "\x00\xff path:*\n",
	} {
		f.Add(text)
	}
	subject := SubjectOf(dotnetEvent())
	f.Fuzz(func(t *testing.T, text string) {
		rules, _ := ParseRules(text)
		codeowners, _ := ParseCodeowners(text)
		rs := &Ruleset{Rules: append(rules, codeowners...)}
		matched := rs.Match(subject)
		if len(matched) > 0 {
			_ = Winner(matched).Text()
		}
	})
}
