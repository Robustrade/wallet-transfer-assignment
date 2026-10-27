// Package testutil provides PostgreSQL fixtures shared by integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-service/internal/database"
)

// Each test owns a database. Never truncate or migrate the supplied database.
func NewDatabase(t *testing.T) (*pgxpool.Pool, *migrate.Migrate) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL is required in CI")
		}
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL tests")
	}
	ctx := t.Context()
	admin, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "wallet_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	query := u.Query()
	query.Del("dbname")
	query.Del("database")
	u.RawQuery = query.Encode()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test fixtures")
	}
	root := filepath.Join(filepath.Dir(file), "../..")
	path, err := filepath.Abs(filepath.Join(root, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New((&url.URL{Scheme: "file", Path: path}).String(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sourceErr, dbErr := m.Close()
		if err := errors.Join(sourceErr, dbErr); err != nil {
			t.Error(err)
		}
	})
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	// Leave room for the blocker, concurrent requests, and lock observation in CI.
	query = u.Query()
	query.Set("pool_max_conns", "8")
	u.RawQuery = query.Encode()
	pool, err := database.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	seed, err := os.ReadFile(filepath.Join(root, "seeds/wallets.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	return pool, m
}
