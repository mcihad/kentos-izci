package izci

import (
	"fmt"
	"time"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Level is the severity of an event.
type Level int32

// Severity levels, mirroring izci.ingest.v1.Level.
const (
	LevelDebug   = Level(ingestv1.Level_LEVEL_DEBUG)
	LevelInfo    = Level(ingestv1.Level_LEVEL_INFO)
	LevelWarning = Level(ingestv1.Level_LEVEL_WARNING)
	LevelError   = Level(ingestv1.Level_LEVEL_ERROR)
	LevelFatal   = Level(ingestv1.Level_LEVEL_FATAL)
)

// User identifies the end user affected by an event.
type User struct {
	ID         string
	Username   string
	Email      string
	Name       string
	IPAddress  string
	Segment    string
	Attributes map[string]string
}

// Breadcrumb is something that happened before an event.
type Breadcrumb struct {
	// Type selects the rendering: "default", "http", "navigation", "query",
	// "ui", "user", "error", "debug", "info".
	Type      string
	Category  string
	Level     Level
	Message   string
	Data      map[string]any
	Timestamp time.Time
}

// Protocol selects the transport protocol.
type Protocol int

const (
	// ProtocolConnect uses the Connect protocol (HTTP/1.1 or HTTP/2, works
	// through any proxy). The default.
	ProtocolConnect Protocol = iota
	// ProtocolGRPC uses native gRPC (HTTP/2; cleartext h2c for http:// URLs).
	ProtocolGRPC
)

func fmtValue(v any) string { return fmt.Sprintf("%+v", v) }
