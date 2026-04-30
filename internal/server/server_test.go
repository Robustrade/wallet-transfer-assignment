package server

import (
	"errors"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func makeSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&model.Wallet{}, &model.Transfer{}, &model.LedgerEntry{}, &model.IdempotencyRecord{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	return db
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Port != ":8080" {
		t.Fatalf("unexpected default port: %s", cfg.Port)
	}
	if cfg.ReadTimeout <= 0 || cfg.WriteTimeout <= 0 || cfg.IdleTimeout <= 0 || cfg.ShutdownTimeout <= 0 {
		t.Fatalf("expected positive timeout values")
	}
	if cfg.DatabaseURL == "" {
		t.Fatalf("default database URL must not be empty")
	}
}

func TestNewAndShutdown(t *testing.T) {
	original := openDB
	defer func() { openDB = original }()

	testDB := makeSQLiteDB(t)
	openDB = func(dsn string) (*gorm.DB, error) {
		return testDB, nil
	}

	cfg := DefaultConfig()
	cfg.Port = ":0"
	cfg.ShutdownTimeout = 2 * time.Second

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	if srv == nil || srv.httpServer == nil {
		t.Fatalf("expected initialized server")
	}
	if srv.httpServer.Addr != ":0" {
		t.Fatalf("expected configured port on http server")
	}

	// Ensure mux routes are wired
	healthReq, _ := http.NewRequest(http.MethodGet, "/health", nil)
	healthRec := &testResponseWriter{header: make(http.Header)}
	srv.httpServer.Handler.ServeHTTP(healthRec, healthReq)
	if healthRec.code != http.StatusOK {
		t.Fatalf("expected health route to respond 200")
	}

	if err := srv.Shutdown(); err != nil {
		t.Fatalf("shutdown should succeed before start, got: %v", err)
	}
}

func TestNewDBOpenFailure(t *testing.T) {
	original := openDB
	defer func() { openDB = original }()

	openDB = func(dsn string) (*gorm.DB, error) {
		return nil, errors.New("cannot connect")
	}

	_, err := New(DefaultConfig())
	if err == nil {
		t.Fatalf("expected server creation to fail when db open fails")
	}
}

func TestStartGracefulShutdownOnSignal(t *testing.T) {
	original := openDB
	defer func() { openDB = original }()
	origSignalNotify := signalNotify
	defer func() { signalNotify = origSignalNotify }()

	testDB := makeSQLiteDB(t)
	openDB = func(dsn string) (*gorm.DB, error) {
		return testDB, nil
	}

	cfg := DefaultConfig()
	cfg.Port = ":0"
	cfg.ShutdownTimeout = 2 * time.Second
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}

	signalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
		c <- syscall.SIGTERM
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("start should return nil on graceful shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for server shutdown")
	}
}

type testResponseWriter struct {
	header http.Header
	code   int
}

func (w *testResponseWriter) Header() http.Header {
	return w.header
}

func (w *testResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}
