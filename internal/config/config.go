package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type Detector struct {
	DatabaseURL  string
	StrategyPath string
}

type API struct {
	DatabaseURL string
	HTTPAddr    string
	MaxFeedAge  time.Duration
}

func DetectorFromEnv() (Detector, error) {
	c := Detector{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		StrategyPath: envOr("ARB_CONFIG", "/etc/arb-scanner/strategies.yaml"),
	}

	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	return c, errors.Join(errs...)
}

func APIFromEnv() (API, error) {
	c := API{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    envOr("HTTP_ADDR", ":8080"),
	}

	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}

	maxFeedAge, err := envDuration("MAX_FEED_AGE", 30*time.Second)
	if err != nil {
		errs = append(errs, err)
	}
	c.MaxFeedAge = maxFeedAge

	return c, errors.Join(errs...)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
