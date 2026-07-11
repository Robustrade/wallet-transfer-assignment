package main

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupInMemory(t *testing.T) (*Repository, *Service, func()) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// ensure a single connection for in-memory SQLite so migrate and tests use same DB
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo)
	cleanup := func() { db.Close() }
	return repo, svc, cleanup
}

func TestTransferBasic(t *testing.T) {
	repo, svc, cleanup := setupInMemory(t)
	defer cleanup()

	if err := repo.CreateWallet("wallet_1", 1000); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if err := repo.CreateWallet("wallet_2", 100); err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	res, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: "k1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 200})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if res.State != "PROCESSED" {
		t.Fatalf("unexpected state: %s", res.State)
	}

	b1, _ := repo.GetWalletBalance("wallet_1")
	b2, _ := repo.GetWalletBalance("wallet_2")
	if b1 != 800 || b2 != 300 {
		t.Fatalf("balances incorrect: %d %d", b1, b2)
	}
}

func TestIdempotency(t *testing.T) {
	repo, svc, cleanup := setupInMemory(t)
	defer cleanup()

	_ = repo.CreateWallet("w1", 500)
	_ = repo.CreateWallet("w2", 100)

	r1, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: "dup-key", FromWalletID: "w1", ToWalletID: "w2", Amount: 50})
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	r2, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: "dup-key", FromWalletID: "w1", ToWalletID: "w2", Amount: 50})
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	if r1.TransferID != r2.TransferID {
		t.Fatalf("idempotent responses differ: %s %s", r1.TransferID, r2.TransferID)
	}

	b1, _ := repo.GetWalletBalance("w1")
	b2, _ := repo.GetWalletBalance("w2")
	if b1 != 450 || b2 != 150 {
		t.Fatalf("balances incorrect after idempotent calls: %d %d", b1, b2)
	}
}

func TestConcurrentDebits(t *testing.T) {
	repo, svc, cleanup := setupInMemory(t)
	defer cleanup()

	_ = repo.CreateWallet("c_src", 100)
	_ = repo.CreateWallet("c_dst", 0)

	var wg sync.WaitGroup
	attempts := 10
	wg.Add(attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := svc.Transfer(context.TODO(), TransferRequest{IdempotencyKey: generateID(), FromWalletID: "c_src", ToWalletID: "c_dst", Amount: 30})
			errs[i] = err
		}()
	}
	wg.Wait()

	bsrc, _ := repo.GetWalletBalance("c_src")
	bdst, _ := repo.GetWalletBalance("c_dst")

	// only three transfers of 30 should have succeeded (3*30=90)
	if bsrc != 10 {
		t.Fatalf("expected src balance 10, got %d", bsrc)
	}
	if bdst != 90 {
		t.Fatalf("expected dst balance 90, got %d", bdst)
	}
}
