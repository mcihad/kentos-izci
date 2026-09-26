// Package logging builds the process-wide slog logger.
package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/mcihad/kentos-izci/internal/config"
)

// New returns a logger configured from cfg and installs it as slog's default.
func New(cfg config.LogConfig) *slog.Logger {
	return NewWithWriter(cfg, os.Stderr)
}

// NewWithWriter is New with an explicit output, used by tests.
func NewWithWriter(cfg config.LogConfig, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	var h slog.Handler
	if cfg.Format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger
}
