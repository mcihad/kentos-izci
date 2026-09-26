package ingest

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestScrubValue(t *testing.T) {
	s := NewScrubber(nil, nil)
	cases := map[string]string{
		"Authorization failed for Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig": "Authorization failed for Bearer [Filtered]",
		"connect postgres://izci:s3cr3t@db:5432/izci":                      "connect postgres://izci:[Filtered]@db:5432/izci",
		"login password=hunter2&user=ali":                                  "login password=[Filtered]&user=ali",
		`{"token": "abc123def456"}`:                                        `{"token": "[Filtered]"}`,
		"card 4111 1111 1111 1111 declined":                                "card [Filtered] declined",
		"order 1234567890123 shipped":                                      "order 1234567890123 shipped", // not Luhn-valid
		"TCKN 10000000146 geçersiz":                                        "TCKN [Filtered] geçersiz",
		"id 12345678901 is not a tckn":                                     "id 12345678901 is not a tckn",
		"IBAN TR33 0006 1005 1978 6457 8413 26":                            "IBAN [Filtered]",
		"short":                                                            "short",
	}
	for in, want := range cases {
		if got := s.Value(in); got != want {
			t.Errorf("Value(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestScrubKeysAndStructures(t *testing.T) {
	s := NewScrubber([]string{"musteri_no"}, []string{"session_count"})
	for key, want := range map[string]bool{
		"password": true, "X-Api-Key": true, "user_PASSWORD_hash": true, "musteri_no": true,
		"session_count": false, "name": false,
	} {
		if got := s.SensitiveKey(key); got != want {
			t.Errorf("SensitiveKey(%q) = %v, want %v", key, got, want)
		}
	}
	if !s.SensitiveHeader("Cookie") || s.SensitiveHeader("Accept-Language") {
		t.Error("header classification wrong")
	}

	if got := s.QueryString("page=2&api_key=xyz&q=a%20b"); got != "page=2&api_key=%5BFiltered%5D&q=a%20b" {
		t.Errorf("QueryString = %q", got)
	}

	body := s.Body(`{"user":"ali","password":"p","nested":{"card_number":"4111111111111111","note":"ok"},"list":[{"secret":1}]}`, "application/json")
	for _, leak := range []string{`"p"`, "4111111111111111", `"secret":1`} {
		if strings.Contains(body, leak) {
			t.Errorf("body leaks %s: %s", leak, body)
		}
	}
	if !strings.Contains(body, `"note":"ok"`) {
		t.Errorf("body lost safe data: %s", body)
	}

	if got := s.Body("user=ali&password=x", "application/x-www-form-urlencoded"); got != "user=ali&password=%5BFiltered%5D" {
		t.Errorf("form body = %q", got)
	}

	st, _ := structpb.NewStruct(map[string]any{
		"token":   "abc",
		"payment": map[string]any{"cvv": "123", "provider": "iyzico"},
		"notes":   []any{"Bearer abcdefghijklmnop"},
	})
	s.Struct(st)
	m := st.AsMap()
	if m["token"] != Filtered || m["payment"].(map[string]any)["cvv"] != Filtered || m["payment"].(map[string]any)["provider"] != "iyzico" {
		t.Errorf("struct scrub wrong: %v", m)
	}
	if m["notes"].([]any)[0] != "Bearer [Filtered]" {
		t.Errorf("list scrub wrong: %v", m["notes"])
	}
}

func TestLuhnAndTCKN(t *testing.T) {
	if !luhn("4111111111111111") || luhn("4111111111111112") {
		t.Error("luhn")
	}
	if !validTCKN("10000000146") || validTCKN("10000000147") || validTCKN("01234567890") {
		t.Error("tckn")
	}
}
