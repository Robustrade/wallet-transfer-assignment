package config

import (
	"crypto/x509"
	"testing"
)

func TestLoadFromEnvUsesDefaultsAndValidValues(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":       "postgres://user:pass@localhost:5432/wallet?sslmode=disable",
		"SSL_ENABLED":        "true",
		"MAX_CLAIM_ATTEMPTS": "5",
	}
	config, err := LoadFromEnv(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if config.Port != "8080" || config.MaxClaimAttempts != 5 || !config.SSLEnabled {
		t.Fatalf("unexpected config: %+v", config)
	}
}

func TestLoadFromEnvRequiresDatabaseURL(t *testing.T) {
	if _, err := LoadFromEnv(func(string) string { return "" }); err == nil {
		t.Fatal("expected missing DATABASE_URL to fail")
	}
}

func TestLoadFromEnvRequiresTLSFilesAsPair(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/wallet?sslmode=disable",
		"TLS_CERT_FILE": "server.crt",
	}
	if _, err := LoadFromEnv(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected a lone TLS certificate path to fail")
	}
}

func TestLoadFromEnvRejectsInvalidClaimAttempts(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":       "postgres://user:pass@localhost:5432/wallet?sslmode=disable",
		"MAX_CLAIM_ATTEMPTS": "0",
	}
	if _, err := LoadFromEnv(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected invalid MAX_CLAIM_ATTEMPTS to fail")
	}
}

func TestDevelopmentCertificateHasLocalhostNames(t *testing.T) {
	certificate, err := DevelopmentCertificate()
	if err != nil {
		t.Fatalf("generate development certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse development certificate: %v", err)
	}
	if err := parsed.VerifyHostname("localhost"); err != nil {
		t.Fatalf("certificate does not cover localhost: %v", err)
	}
}
