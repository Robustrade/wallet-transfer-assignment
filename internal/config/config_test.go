package config_test

import (
	"errors"
	"os"
	"testing"

	"wallet-transfer-assignment/internal/config"
)

func TestConfig_Load_DevelopmentDefaults(t *testing.T) {
	_ = os.Setenv("APP_ENV", "development")
	defer os.Unsetenv("APP_ENV")
	_ = os.Unsetenv("DATABASE_URL")
	_ = os.Unsetenv("ENV")
	_ = os.Unsetenv("PORT")
	_ = os.Unsetenv("ADMIN_KEY")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error loading dev defaults, got %v", err)
	}

	if cfg.Port != "8080" {
		t.Fatalf("expected default port 8080, got %s", cfg.Port)
	}

	if cfg.AdminKey != "admin-secret-dev" {
		t.Fatalf("expected default dev admin key 'admin-secret-dev', got %q", cfg.AdminKey)
	}

	// Least-privileged application role rather than superuser 'postgres'
	const expectedDevURL = "postgres://wallet_app:wallet_app_password@localhost:5432/wallet_db?sslmode=disable"
	if cfg.DatabaseURL != expectedDevURL {
		t.Fatalf("expected default dev DB URL %q, got %q", expectedDevURL, cfg.DatabaseURL)
	}
}

func TestConfig_Load_MissingEnvironmentTreatedAsNonDevelopment(t *testing.T) {
	_ = os.Unsetenv("APP_ENV")
	_ = os.Unsetenv("ENV")
	_ = os.Unsetenv("DATABASE_URL")
	_ = os.Unsetenv("ADMIN_KEY")

	cfg, err := config.Load()
	if err == nil {
		t.Fatalf("expected error when environment is omitted, got cfg: %+v", cfg)
	}
	if !errors.Is(err, config.ErrMissingDatabaseURL) {
		t.Fatalf("expected ErrMissingDatabaseURL when environment is omitted, got %v", err)
	}
}

func TestConfig_Load_CustomDatabaseURLRequiresExplicitAdminKey(t *testing.T) {
	const customURL = "postgres://custom_user:secret@customhost:5432/custom_db?sslmode=require"
	_ = os.Setenv("APP_ENV", "development")
	defer os.Unsetenv("APP_ENV")
	_ = os.Setenv("DATABASE_URL", customURL)
	defer os.Unsetenv("DATABASE_URL")
	_ = os.Unsetenv("ADMIN_KEY")
	_ = os.Unsetenv("ADMIN_API_KEY")

	// Setting custom database URL without ADMIN_KEY must fail
	cfg, err := config.Load()
	if err == nil {
		t.Fatalf("expected error when ADMIN_KEY is omitted for custom DATABASE_URL, got %+v", cfg)
	}
	if !errors.Is(err, config.ErrMissingAdminKey) {
		t.Fatalf("expected ErrMissingAdminKey, got %v", err)
	}

	// With ADMIN_KEY provided, it succeeds
	_ = os.Setenv("ADMIN_KEY", "custom-admin-key")
	defer os.Unsetenv("ADMIN_KEY")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("expected success with explicit admin key and custom DB URL, got %v", err)
	}
	if cfg.DatabaseURL != customURL || cfg.AdminKey != "custom-admin-key" {
		t.Fatalf("unexpected config values: %+v", cfg)
	}
}

func TestConfig_Load_ProductionRequiresExplicitDatabaseURL(t *testing.T) {
	nonDevEnvs := []string{"production", "staging", "prod"}

	for _, env := range nonDevEnvs {
		t.Run(env, func(t *testing.T) {
			_ = os.Setenv("APP_ENV", env)
			defer os.Unsetenv("APP_ENV")
			_ = os.Unsetenv("DATABASE_URL")

			cfg, err := config.Load()
			if err == nil {
				t.Fatalf("expected error when DATABASE_URL is omitted in %s, got cfg: %+v", env, cfg)
			}
			if !errors.Is(err, config.ErrMissingDatabaseURL) {
				t.Fatalf("expected ErrMissingDatabaseURL, got %v", err)
			}
		})
	}
}

func TestConfig_Load_ProductionRequiresExplicitAdminKey(t *testing.T) {
	const prodURL = "postgres://prod_user:strong_password@prod_host:5432/prod_wallet_db?sslmode=verify-full"
	nonDevEnvs := []string{"production", "staging", "prod"}

	for _, env := range nonDevEnvs {
		t.Run(env, func(t *testing.T) {
			_ = os.Setenv("APP_ENV", env)
			defer os.Unsetenv("APP_ENV")
			_ = os.Setenv("DATABASE_URL", prodURL)
			defer os.Unsetenv("DATABASE_URL")
			_ = os.Unsetenv("ADMIN_KEY")
			_ = os.Unsetenv("ADMIN_API_KEY")

			cfg, err := config.Load()
			if err == nil {
				t.Fatalf("expected error when ADMIN_KEY is omitted in %s, got cfg: %+v", env, cfg)
			}
			if !errors.Is(err, config.ErrMissingAdminKey) {
				t.Fatalf("expected ErrMissingAdminKey, got %v", err)
			}
		})
	}
}

func TestConfig_Load_ProductionWithExplicitDatabaseURLAndAdminKey(t *testing.T) {
	const prodURL = "postgres://prod_user:strong_password@prod_host:5432/prod_wallet_db?sslmode=verify-full"
	const prodAdminKey = "prod-super-secret-admin-key"
	_ = os.Setenv("APP_ENV", "production")
	defer os.Unsetenv("APP_ENV")
	_ = os.Setenv("DATABASE_URL", prodURL)
	defer os.Unsetenv("DATABASE_URL")
	_ = os.Setenv("ADMIN_KEY", prodAdminKey)
	defer os.Unsetenv("ADMIN_KEY")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error with explicit prod URL and admin key, got %v", err)
	}
	if cfg.DatabaseURL != prodURL {
		t.Fatalf("expected %q, got %q", prodURL, cfg.DatabaseURL)
	}
	if cfg.AdminKey != prodAdminKey {
		t.Fatalf("expected %q, got %q", prodAdminKey, cfg.AdminKey)
	}
}
