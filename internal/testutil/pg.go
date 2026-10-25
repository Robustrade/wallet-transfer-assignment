// Package testutil provides a real PostgreSQL for integration tests.
//
// By default it starts an embedded PostgreSQL (downloaded once and cached), so
// `go test ./...` needs no Docker and no manual setup. Set TEST_DATABASE_URL to
// a server you control to use that instead. Each test gets its own freshly
// migrated database, so tests can run in parallel without interfering.
package testutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/GaureshPatil/wallet-transfer-assignment/migrations"
)

// adminURL points at the server used to create per-test databases.
var adminURL string

var dbCounter atomic.Int64

// Run starts PostgreSQL (unless TEST_DATABASE_URL is set), runs the package's
// tests and shuts the server down. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.Run(m)) }
func Run(m *testing.M) int {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		adminURL = url
		return m.Run()
	}

	stop, err := startEmbedded()
	if err != nil {
		fmt.Fprintf(os.Stderr, "start embedded postgres: %v\n", err)
		return 1
	}
	code := m.Run()
	stop()
	return code
}

func startEmbedded() (stop func(), err error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "wallet-pg-*")
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}

	cfg := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(uint32(port)).
		DataPath(filepath.Join(dir, "data")).
		RuntimePath(filepath.Join(dir, "runtime")).
		CachePath(filepath.Join(cache, "wallet-transfer-embedded-pg")).
		Logger(io.Discard).
		StartTimeout(2 * time.Minute)
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	adminURL = fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
	return func() {
		_ = pg.Stop()
		_ = os.RemoveAll(dir)
	}, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// NewPool returns a pool connected to a brand-new, fully migrated database that
// is dropped when the test ends.
func NewPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if adminURL == "" {
		t.Fatal("testutil.Run must be called from TestMain")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer admin.Close(ctx)

	name := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), dbCounter.Add(1))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 40
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		conn, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			return
		}
		defer conn.Close(ctx)
		_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+strings.ToLower(name)+" WITH (FORCE)")
	})

	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}
