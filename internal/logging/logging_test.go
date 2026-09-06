package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"INFO":     slog.LevelInfo,
		" warn  ":  slog.LevelWarn,
		"error":    slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewWithFormats(t *testing.T) {
	t.Run("defaults to text", func(t *testing.T) {
		var buf bytes.Buffer
		NewWith(&buf, "", "").Info("hello", "venue", "binance")

		out := buf.String()
		if !strings.Contains(out, `msg=hello`) || !strings.Contains(out, `venue=binance`) {
			t.Fatalf("expected text output, got %q", out)
		}
	})

	t.Run("json when requested", func(t *testing.T) {
		var buf bytes.Buffer
		NewWith(&buf, "JSON", "").Info("hello", "venue", "binance")

		var rec map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
			t.Fatalf("output is not JSON: %v (%q)", err, buf.String())
		}
		if rec["msg"] != "hello" || rec["venue"] != "binance" {
			t.Fatalf("unexpected record: %v", rec)
		}
	})
}

func TestNewWithLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := NewWith(&buf, "text", "warn")

	log.Info("dropped")
	log.Warn("kept")

	out := buf.String()
	if strings.Contains(out, "dropped") {
		t.Errorf("info record should be filtered at warn level: %q", out)
	}
	if !strings.Contains(out, "kept") {
		t.Errorf("warn record should be emitted: %q", out)
	}
}
