package config

import "testing"

func setRequiredEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"APP_PORT": "8080", "DB_HOST": "localhost", "DB_PORT": "5432",
		"DB_USER": "u", "DB_PASSWORD": "p", "DB_NAME": "d", "DB_SSLMODE": "disable",
	} {
		t.Setenv(k, v)
	}
}

func TestLoad_DBLogLevelDefaultsToWarn(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBLogLevel != "warn" {
		t.Fatalf("DBLogLevel = %q, want warn", cfg.DBLogLevel)
	}
}

func TestLoad_DBLogLevelCanBeSetAndIsValidated(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("DB_LOG_LEVEL", "info")
	cfg, err := Load()
	if err != nil || cfg.DBLogLevel != "info" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}

	t.Setenv("DB_LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an unknown DB_LOG_LEVEL")
	}
}

func TestLoad_MissingRequiredVariable(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_HOST", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DB_HOST is missing")
	}
}
