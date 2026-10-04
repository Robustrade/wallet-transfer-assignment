package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsPasswordInFile(t *testing.T) {
	t.Setenv("WALLET_DB_PASSWORD", "secret")
	t.Setenv("DATABASE_URL", "")
	path := writeConfig(t, `
server:
  address: ":8080"
  readHeaderTimeout: 5s
  readTimeout: 15s
  writeTimeout: 15s
  idleTimeout: 60s
  shutdownTimeout: 15s
  requestTimeout: 10s
database:
  host: localhost
  port: 5432
  name: wallet
  user: wallet
  password: "nope"
  sslmode: disable
  maxConns: 20
  minConns: 2
  maxConnLifetime: 30m
  maxConnIdleTime: 5m
  connectRetries: 10
  connectRetryInterval: 1s
  lockTimeout: 5s
  statementTimeout: 8s
log:
  level: info
  format: json
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "must not contain database.password") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRequiresPassword(t *testing.T) {
	t.Setenv("WALLET_DB_PASSWORD", "")
	t.Setenv("DATABASE_URL", "")
	clearDBEnv(t)
	path := writeConfig(t, validYAML())
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "WALLET_DB_PASSWORD") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadEnvOverridesAndDatabaseURL(t *testing.T) {
	clearDBEnv(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("WALLET_DB_PASSWORD", "s3cret")
	t.Setenv("WALLET_DB_HOST", "db.internal")
	t.Setenv("WALLET_DB_PORT", "5433")
	path := writeConfig(t, validYAML())
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "db.internal" || cfg.Database.Port != 5433 || cfg.Database.Password != "s3cret" {
		t.Fatalf("cfg database = %+v", cfg.Database)
	}
	if strings.Contains(cfg.Database.RedactedTarget(), "s3cret") {
		t.Fatal("redacted target leaked the password")
	}
	if !strings.Contains(cfg.Database.DSN(), "s3cret") {
		t.Fatal("dsn missing password")
	}

	t.Setenv("DATABASE_URL", "postgres://wallet:s3cret@localhost:5432/wallet?sslmode=disable")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.DSN() != "postgres://wallet:s3cret@localhost:5432/wallet?sslmode=disable" {
		t.Fatalf("dsn = %s", cfg.Database.DSN())
	}
}

func TestLoadUnknownField(t *testing.T) {
	t.Setenv("WALLET_DB_PASSWORD", "secret")
	t.Setenv("DATABASE_URL", "")
	clearDBEnv(t)
	yaml := strings.Replace(validYAML(), "format: json", "format: json\n  extra: true", 1)
	path := writeConfig(t, yaml)
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field to fail")
	}
}

func validYAML() string {
	return `
server:
  address: ":8080"
  readHeaderTimeout: 5s
  readTimeout: 15s
  writeTimeout: 15s
  idleTimeout: 60s
  shutdownTimeout: 15s
  requestTimeout: 10s
database:
  host: localhost
  port: 5432
  name: wallet
  user: wallet
  sslmode: disable
  maxConns: 20
  minConns: 2
  maxConnLifetime: 30m
  maxConnIdleTime: 5m
  connectRetries: 10
  connectRetryInterval: 1s
  lockTimeout: 5s
  statementTimeout: 8s
log:
  level: info
  format: json
`
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func clearDBEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"WALLET_DB_HOST", "WALLET_DB_PORT", "WALLET_DB_NAME", "WALLET_DB_USER", "WALLET_DB_SSLMODE"} {
		t.Setenv(key, "")
	}
}
