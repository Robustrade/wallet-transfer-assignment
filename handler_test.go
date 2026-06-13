package main

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestCreateTransferHandler_Success(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo)
	h := NewHandler(svc)

	// seed wallets
	if err := repo.CreateWallet("hw1", 500); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.CreateWallet("hw2", 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := `{"idempotencyKey":"k","fromWalletId":"hw1","toWalletId":"hw2","amount":100}`
	res, err := http.Post(ts.URL+"/transfers", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("unexpected status: %d body:%s", res.StatusCode, string(b))
	}

	b1, _ := repo.GetWalletBalance("hw1")
	b2, _ := repo.GetWalletBalance("hw2")
	if b1 != 400 || b2 != 100 {
		t.Fatalf("balances incorrect: %d %d", b1, b2)
	}
}

func TestCreateTransferHandler_InsufficientFunds(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo)
	h := NewHandler(svc)

	// seed wallet with small balance
	if err := repo.CreateWallet("s1", 10); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.CreateWallet("s2", 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := `{"idempotencyKey":"k2","fromWalletId":"s1","toWalletId":"s2","amount":100}`
	res, err := http.Post(ts.URL+"/transfers", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnprocessableEntity {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 422 got %d body:%s", res.StatusCode, string(b))
	}
}

func TestHandler_IdempotencyQuickReturn(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	// pre-insert an idempotency record as COMPLETED
	_, err = db.Exec(`INSERT INTO idempotency_records (idempotency_key, transfer_id, status) VALUES (?, ?, ?)`, "ik1", "t_existing", "COMPLETED")
	if err != nil {
		t.Fatalf("insert idempotency: %v", err)
	}
	svc := NewService(repo)
	h := NewHandler(svc)

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	body := `{"idempotencyKey":"ik1","fromWalletId":"x","toWalletId":"y","amount":1}`
	res, err := http.Post(ts.URL+"/transfers", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("unexpected status: %d body:%s", res.StatusCode, string(b))
	}
	// response should contain transferId t_existing
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), "t_existing") {
		t.Fatalf("expected existing transfer id in response, got: %s", string(b))
	}
}

func TestCreateTransferHandler_MethodNotAllowed(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo)
	h := NewHandler(svc)

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/transfers")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", res.StatusCode)
	}
}

func TestCreateTransferHandler_BadJSON(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo)
	h := NewHandler(svc)

	ts := httptest.NewServer(h.Router())
	defer ts.Close()

	res, err := http.Post(ts.URL+"/transfers", "application/json", strings.NewReader("not-json"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", res.StatusCode)
	}
}
