package db

import (
	"strings"
	"testing"
)

// TestOpen_InvalidDSN verifies that a malformed DSN fails synchronously
// at startup rather than producing a *sql.DB that explodes later on
// the first query. This matters because a typo'd DSN in a deployment
// should panic at process start (Kubernetes restart loop, instantly
// visible) instead of after the first traffic arrives.
func TestOpen_InvalidDSN(t *testing.T) {
	if _, err := Open("not-a-valid-dsn"); err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

// TestOpen_UnreachableHost verifies that Open fails fast when the
// host is reachable-as-a-DNS-name but the TCP connection cannot be
// established.
//
// 127.0.0.1:1 is the discard port: nothing listens there, so pgx
// will get an immediate ECONNREFUSED. connect_timeout=1 caps the
// wait so a slow CI runner doesn't make this test flaky.
//
// We do NOT assert on the error message text — pgx version updates
// reword these regularly. The log line is informational only.
func TestOpen_UnreachableHost(t *testing.T) {
	_, err := Open("postgres://user:pass@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected ping error")
	}
	if !strings.Contains(err.Error(), "Ping") && !strings.Contains(err.Error(), "ping") {
		t.Logf("error did not mention ping: %v", err)
	}
}
