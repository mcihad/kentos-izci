package slug

import "testing"

func TestMake(t *testing.T) {
	cases := map[string]string{
		"KentOS Kalem":             "kentos-kalem",
		"Sivas Belediyesi Şikâyet": "sivas-belediyesi-sikayet",
		"  İzci -- Öğrenci!!  ":    "izci-ogrenci",
		"123 Uygulama":             "p-123-uygulama",
		"!!!":                      "",
		"a":                        "",
	}
	for in, want := range cases {
		if got := Make(in, 50); got != want {
			t.Errorf("Make(%q) = %q, want %q", in, got, want)
		}
		if want != "" && !Valid(want) {
			t.Errorf("Make(%q) produced invalid slug %q", in, want)
		}
	}
	if got := Make("abcdefghij-klmnop", 12); got != "abcdefghij-k" {
		t.Errorf("truncation: %q", got)
	}
	for _, bad := range []string{"", "a", "-ab", "ab-", "Ab", "a_b", "1ab"} {
		if Valid(bad) {
			t.Errorf("Valid(%q) must be false", bad)
		}
	}
}
