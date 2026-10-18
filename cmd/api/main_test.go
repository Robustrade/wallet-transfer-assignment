package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// The service must refuse to start, without touching the network or the
// database, when required configuration is missing.
func TestRunFailsFastWhenConfigurationIsMissing(t *testing.T) {
	for _, key := range []string{
		"APP_PORT", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
	} {
		t.Setenv(key, "")
	}

	err := run(slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err == nil || !strings.Contains(err.Error(), "configuration validation failed") {
		t.Fatalf("got %v, want a configuration validation error", err)
	}
}
