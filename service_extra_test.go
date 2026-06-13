package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestTransfer_QuickIdempotency(t *testing.T) {
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

	// insert completed idempotency
	_, err = db.Exec(`INSERT INTO idempotency_records (idempotency_key, transfer_id, status) VALUES (?, ?, ?)`, "quickk", "t_quick", "COMPLETED")
	if err != nil {
		t.Fatalf("insert idempotency: %v", err)
	}

	res, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: "quickk", FromWalletID: "a", ToWalletID: "b", Amount: 1})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if res.TransferID != "t_quick" {
		t.Fatalf("expected t_quick got %s", res.TransferID)
	}
}

func TestTransfer_InsertIdempotencyConflictPoll(t *testing.T) {
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

	// create wallets and a transfer row that will be referenced
	_, _ = db.Exec(`INSERT INTO wallets (id, balance) VALUES (?, ?)`, "x", 0)
	_, _ = db.Exec(`INSERT INTO wallets (id, balance) VALUES (?, ?)`, "y", 0)
	_, err = db.Exec(`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state) VALUES (?, ?, ?, ?, ?)`, "t_conf", "x", "y", 1, "PROCESSED")
	if err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	// insert idempotency as IN_PROGRESS so InsertIdempotency will conflict
	_, err = db.Exec(`INSERT INTO idempotency_records (idempotency_key, status) VALUES (?, ?)`, "kconf", "IN_PROGRESS")
	if err != nil {
		t.Fatalf("insert idemp: %v", err)
	}

	// after a short delay, mark it completed
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = db.Exec(`UPDATE idempotency_records SET transfer_id = ?, status = ? WHERE idempotency_key = ?`, "t_conf", "COMPLETED", "kconf")
	}()

	res, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: "kconf", FromWalletID: "x", ToWalletID: "y", Amount: 1})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if res.TransferID != "t_conf" {
		t.Fatalf("expected t_conf got %s", res.TransferID)
	}
}

func TestTransfer_InsufficientFunds_Service(t *testing.T) {
	repo, svc, cleanup := setupInMemory(t)
	defer cleanup()
	_ = repo.CreateWallet("s_src", 10)
	_ = repo.CreateWallet("s_dst", 0)

	_, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: "if1", FromWalletID: "s_src", ToWalletID: "s_dst", Amount: 100})
	if err == nil {
		t.Fatalf("expected insufficient funds error")
	}
	if err != ErrInsufficientFunds {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartHTTPServer_CreatesServer(t *testing.T) {
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
	srv := StartHTTPServer(db, ":9999")
	if srv == nil {
		t.Fatalf("expected server")
	}
	if srv.Addr != ":9999" {
		t.Fatalf("unexpected addr: %s", srv.Addr)
	}
}

func TestIdempotency_FailedThenRetry(t *testing.T) {
	repo, svc, cleanup := setupInMemory(t)
	defer cleanup()

	_ = repo.CreateWallet("f_src", 10)
	_ = repo.CreateWallet("f_dst", 0)

	key := "fail-key"
	_, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: key, FromWalletID: "f_src", ToWalletID: "f_dst", Amount: 100})
	if err == nil {
		t.Fatalf("expected insufficient funds on first attempt")
	}
	if err != ErrInsufficientFunds {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	// idempotency record should be finalized as FAILED with known response
	tid, status, resp, err := repo.GetIdempotency(context.TODO(), key)
	if err != nil {
		t.Fatalf("GetIdempotency: %v", err)
	}
	if status != "FAILED" {
		t.Fatalf("expected FAILED idempotency status, got %s", status)
	}
	if resp != "insufficient_funds" {
		t.Fatalf("expected response 'insufficient_funds', got %s", resp)
	}
	if tid == "" {
		t.Fatalf("expected transfer id saved in idempotency record")
	}

	// Retry with same key should return the same terminal error quickly
	_, err2 := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: key, FromWalletID: "f_src", ToWalletID: "f_dst", Amount: 100})
	if err2 == nil {
		t.Fatalf("expected error on retry")
	}
	if err2 != ErrInsufficientFunds {
		t.Fatalf("expected ErrInsufficientFunds on retry, got %v", err2)
	}

	// balances unchanged
	b1, _ := repo.GetWalletBalance("f_src")
	b2, _ := repo.GetWalletBalance("f_dst")
	if b1 != 10 || b2 != 0 {
		t.Fatalf("balances changed after failed attempts: %d %d", b1, b2)
	}
}
