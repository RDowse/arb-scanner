package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New reads LOG_FORMAT (text|json, default text) and LOG_LEVEL
// (debug|info|warn|error, default info).
func New() *slog.Logger {
	return NewWith(os.Stdout, os.Getenv("LOG_FORMAT"), os.Getenv("LOG_LEVEL"))
}

func NewWith(w io.Writer, format, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: ParseLevel(level)}

	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}

// ParseLevel falls back to info on unrecognised input, so a typo in the
// environment cannot stop a container from starting.
func ParseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.TrimSpace(s))); err != nil {
		return slog.LevelInfo
	}
	return l
}
