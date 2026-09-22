package config

import (
	"crypto/tls"
	"testing"
)

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/wallet")
	t.Setenv("TLS_CERT_FILE", "")
	t.Setenv("TLS_KEY_FILE", "")
}

func TestLoadDefaultSSL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SSL_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() = false, want true")
	}

	tlsConfig, err := cfg.TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig() error = %v", err)
	}
	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %d, want %d", tlsConfig.MinVersion, tls.VersionTLS13)
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(tlsConfig.Certificates))
	}
}

func TestLoadRejectsPartialCertificatePair(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SSL_ENABLED", "true")
	t.Setenv("TLS_CERT_FILE", "server.crt")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want partial certificate pair error")
	}
}

func TestLoadRejectsInvalidSSLEnabled(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SSL_ENABLED", "sometimes")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid boolean error")
	}
}

func TestLoadMaxClaimAttempts(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MAX_CLAIM_ATTEMPTS", "7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxClaimAttempts != 7 {
		t.Fatalf("MaxClaimAttempts = %d, want 7", cfg.MaxClaimAttempts)
	}
}

func TestLoadRejectsInvalidMaxClaimAttempts(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MAX_CLAIM_ATTEMPTS", "0")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid max claim attempts error")
	}
}

func TestTLSConfiguredWithCertificatePair(t *testing.T) {
	cfg := Config{SSLEnabled: true, TLSCertFile: "server.crt", TLSKeyFile: "server.key"}
	if !cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() = false, want true")
	}
}
