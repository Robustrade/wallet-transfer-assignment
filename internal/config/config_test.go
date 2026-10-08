package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	defaults := map[string]string{
		"DATABASE_URL": "postgres://localhost/wallet", "HTTP_ADDR": ":8080",
		"DB_MAX_OPEN_CONNS": "10", "DB_MAX_IDLE_CONNS": "5",
		"DB_CONN_MAX_IDLE_TIME": "5m", "HTTP_REQUEST_TIMEOUT": "10s",
		"DB_CONN_MAX_LIFETIME": "30m", "STARTUP_TIMEOUT": "5s", "SHUTDOWN_TIMEOUT": "10s",
	}
	for key, value := range defaults {
		t.Setenv(key, value)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.StartupTimeout != 5*time.Second || c.DBMaxOpenConns != 10 || c.DBConnMaxIdleTime != 5*time.Minute || c.HTTPRequestTimeout != 10*time.Second {
		t.Fatalf("unexpected configuration: %+v", c)
	}
	for _, tc := range []struct{ key, value string }{
		{"DATABASE_URL", ""}, {"HTTP_ADDR", " "}, {"DB_MAX_OPEN_CONNS", "0"},
		{"DB_MAX_IDLE_CONNS", "11"}, {"DB_MAX_IDLE_CONNS", "-1"},
		{"STARTUP_TIMEOUT", "invalid"}, {"SHUTDOWN_TIMEOUT", "0s"},
		{"DB_CONN_MAX_LIFETIME", "-1s"}, {"DB_CONN_MAX_IDLE_TIME", "0s"}, {"HTTP_REQUEST_TIMEOUT", "-1s"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected invalid configuration to be rejected")
			}
		})
	}
}
