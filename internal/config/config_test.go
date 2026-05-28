package config

import "testing"

// TestLoad_DefaultsHTTPAddr verifies that when HTTP_ADDR is empty,
// Load picks the documented default (":8080"). DATABASE_URL is the
// only required var — everything else has a sensible default.
//
// t.Setenv automatically unsets the var when the test ends so
// concurrent tests don't see polluted state.
func TestLoad_DefaultsHTTPAddr(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("HTTP_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://x" {
		t.Fatalf("dsn: %q", cfg.DatabaseURL)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("addr: %q", cfg.HTTPAddr)
	}
}

// TestLoad_CustomHTTPAddr verifies that a non-empty HTTP_ADDR is
// passed through unchanged. Together with DefaultsHTTPAddr this
// exercises both branches of the default-or-override logic.
func TestLoad_CustomHTTPAddr(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("HTTP_ADDR", ":9999")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Fatalf("addr: %q", cfg.HTTPAddr)
	}
}

// TestLoad_RequiresDatabaseURL verifies the only hard requirement:
// without DATABASE_URL the service cannot start, and Load must say
// so explicitly rather than returning a partially-built config.
func TestLoad_RequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}

// TestLoad_DebugAddrDefaultsDisabled is a security regression test.
// The pprof admin listener MUST be opt-in — leaving DEBUG_ADDR unset
// or empty must produce a Config with DebugAddr == "" so cmd/server
// never starts the debug listener by accident.
func TestLoad_DebugAddrDefaultsDisabled(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("DEBUG_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DebugAddr != "" {
		t.Fatalf("debug addr should default to empty (disabled), got %q", cfg.DebugAddr)
	}
}

// TestLoad_DebugAddrCustom verifies that an explicit DEBUG_ADDR is
// honoured. The format check itself (loopback vs not) lives in
// cmd/server.isLoopbackAddr and has its own test.
func TestLoad_DebugAddrCustom(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("DEBUG_ADDR", "127.0.0.1:6060")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DebugAddr != "127.0.0.1:6060" {
		t.Fatalf("debug addr: %q", cfg.DebugAddr)
	}
}

// TestLoad_DBPoolDefaults verifies the documented defaults land
// in Config when the corresponding env vars are unset. These
// defaults are the contract the README's "Concurrency and scale"
// section quotes; changing one without updating the README would
// drift the docs.
func TestLoad_DBPoolDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	t.Setenv("DB_MAX_IDLE_CONNS", "")
	t.Setenv("DB_CONN_MAX_LIFETIME", "")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBMaxOpenConns != DefaultDBMaxOpenConns {
		t.Fatalf("max open: got %d want %d", cfg.DBMaxOpenConns, DefaultDBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != DefaultDBMaxIdleConns {
		t.Fatalf("max idle: got %d want %d", cfg.DBMaxIdleConns, DefaultDBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != DefaultDBConnMaxLifetime {
		t.Fatalf("max lifetime: got %v want %v", cfg.DBConnMaxLifetime, DefaultDBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != DefaultDBConnMaxIdleTime {
		t.Fatalf("max idle time: got %v want %v", cfg.DBConnMaxIdleTime, DefaultDBConnMaxIdleTime)
	}
}

// TestLoad_DBPoolCustom verifies operator overrides take effect
// across every tunable. Each subtest checks one var to keep
// failure messages tightly scoped.
func TestLoad_DBPoolCustom(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("DB_MAX_OPEN_CONNS", "100")
	t.Setenv("DB_MAX_IDLE_CONNS", "20")
	t.Setenv("DB_CONN_MAX_LIFETIME", "10m")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "30s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBMaxOpenConns != 100 {
		t.Fatalf("max open: %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 20 {
		t.Fatalf("max idle: %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 10*60*1e9 { // 10m
		t.Fatalf("max lifetime: %v", cfg.DBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != 30*1e9 { // 30s
		t.Fatalf("max idle time: %v", cfg.DBConnMaxIdleTime)
	}
}

// TestLoad_DBPoolRejectsBadValues asserts the fail-loud-on-parse
// stance. A typo'd env var must surface as a Load error, not
// silently revert to the default — otherwise the operator's
// intent is lost.
func TestLoad_DBPoolRejectsBadValues(t *testing.T) {
	cases := []struct {
		key, val string
	}{
		{"DB_MAX_OPEN_CONNS", "not-a-number"},
		{"DB_MAX_OPEN_CONNS", "0"},
		{"DB_MAX_OPEN_CONNS", "-5"},
		{"DB_MAX_IDLE_CONNS", "abc"},
		{"DB_CONN_MAX_LIFETIME", "five minutes"},
		{"DB_CONN_MAX_LIFETIME", "0s"},
		{"DB_CONN_MAX_IDLE_TIME", "1week"},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.val, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv(c.key, c.val)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %s=%q", c.key, c.val)
			}
		})
	}
}
