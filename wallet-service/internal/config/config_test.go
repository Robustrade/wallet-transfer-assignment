package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name, addr, url string
		valid           bool
	}{
		{"defaults", "", "postgres://wallet:wallet@localhost/wallet", true},
		{"overrides", "127.0.0.1:9090", "postgresql://wallet@localhost/wallet", true},
		{"ipv6", "[::1]:8080", "postgres://localhost/wallet", true},
		{"missing database", "", "", false},
		{"wrong scheme", "", "http://localhost/wallet", false},
		{"missing database name", "", "postgres://localhost", false},
		{"invalid port", "localhost:bad", "postgres://localhost/wallet", false},
		{"out of range port", ":65536", "postgres://localhost/wallet", false},
		{"malformed secret", "", "postgres://user:secret@localhost:bad/wallet", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", tc.addr)
			t.Setenv("DATABASE_URL", tc.url)
			cfg, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("config: %+v, error: %v", cfg, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("credential leaked: %v", err)
			}
			if tc.valid {
				addr := tc.addr
				if addr == "" {
					addr = ":8080"
				}
				if cfg.HTTPAddr != addr || cfg.DatabaseURL != tc.url {
					t.Fatalf("unexpected config: %+v", cfg)
				}
			}
		})
	}
}
