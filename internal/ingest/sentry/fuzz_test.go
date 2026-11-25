package sentry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FuzzEnvelope feeds arbitrary bytes to the envelope parser and the item
// decoders: whatever a client sends, they return an error instead of
// panicking.
func FuzzEnvelope(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "sdk", "*.envelope"))
	for _, name := range files {
		if data, err := os.ReadFile(name); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("{}\n{\"type\":\"event\",\"length\":2}\n{}\n"))
	f.Add([]byte("{\"event_id\":\"9ec79c33ec9942ab8353589fcb2e04dc\"}\n{\"type\":\"attachment\",\"length\":3}\nabc"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, items, err := parseEnvelope(data)
		if err != nil {
			return
		}
		for _, it := range items {
			decodeItem(it)
		}
	})
}

// FuzzDecodeEvent feeds arbitrary JSON to the event decoder.
func FuzzDecodeEvent(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "sdk", "*.envelope"))
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		if _, items, err := parseEnvelope(data); err == nil {
			for _, it := range items {
				if it.typ == "event" || it.typ == "transaction" {
					f.Add(it.payload)
				}
			}
		}
	}
	f.Add([]byte(`{"exception":{"values":[{"type":"E","value":"v","stacktrace":{"frames":[{"lineno":-1}]}}]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data)
	})
}

// decodeItem runs the decoder of an item's type.
func decodeItem(it item) {
	const drift = time.Hour
	switch it.typ {
	case "event", "transaction":
		_, _ = Decode(it.payload)
	case "feedback":
		_, _ = DecodeFeedback(it.payload, drift)
	case "user_report":
		_, _ = DecodeUserReport(it.payload)
	case "session":
		_, _ = DecodeSession(it.payload, drift)
	case "sessions":
		_, _ = DecodeSessionAggregates(it.payload, drift)
	case "check_in":
		_, _ = DecodeCheckIn(it.payload, drift)
	}
}
