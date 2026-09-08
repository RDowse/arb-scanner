package config

import (
	"strings"
	"testing"
	"time"
)

func TestDetectorFromEnv(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/arb")
		t.Setenv("EVAL_INTERVAL", "")
		t.Setenv("MAX_BOOK_AGE", "")

		got, err := DetectorFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.EvalInterval != time.Second {
			t.Errorf("EvalInterval = %s, want 1s", got.EvalInterval)
		}
		if got.MaxBookAge != 5*time.Second {
			t.Errorf("MaxBookAge = %s, want 5s", got.MaxBookAge)
		}
	})

	t.Run("requires DATABASE_URL", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")

		if _, err := DetectorFromEnv(); err == nil {
			t.Fatal("expected an error when DATABASE_URL is unset")
		}
	})
}

func TestAPIFromEnv(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/arb")
		t.Setenv("HTTP_ADDR", "")
		t.Setenv("MAX_FEED_AGE", "")

		got, err := APIFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.HTTPAddr != ":8080" {
			t.Errorf("HTTPAddr = %q", got.HTTPAddr)
		}
		if got.MaxFeedAge != 30*time.Second {
			t.Errorf("MaxFeedAge = %v", got.MaxFeedAge)
		}
	})

	t.Run("parses MAX_FEED_AGE", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/arb")
		t.Setenv("MAX_FEED_AGE", "5s")

		got, err := APIFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.MaxFeedAge != 5*time.Second {
			t.Errorf("MaxFeedAge = %v", got.MaxFeedAge)
		}
	})

	t.Run("reports every problem at once", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("MAX_FEED_AGE", "half a minute")

		_, err := APIFromEnv()
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "MAX_FEED_AGE") {
			t.Errorf("expected both failures, got: %v", err)
		}
	})
}
