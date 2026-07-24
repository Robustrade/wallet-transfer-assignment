package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"gorm.io/gorm"

	"wallet-assignment/internal/db"
	"wallet-assignment/internal/domain"
)

// ---------- SETUP ----------
func setup(t *testing.T) (*TransferService, *gorm.DB) {
	database := db.Init()

	// Clean tables
	database.Exec("DELETE FROM wallets")
	database.Exec("DELETE FROM transfers")
	database.Exec("DELETE FROM ledger_entries")
	database.Exec("DELETE FROM idempotency_records")

	// Seed data
	database.Create(&domain.Wallet{ID: "w1", Balance: 100})
	database.Create(&domain.Wallet{ID: "w2", Balance: 0})

	svc := NewTransferService(database)
	return svc, database
}

// ---------- TEST: SUCCESS ----------
func TestCreateTransfer_Success(t *testing.T) {
	svc, _ := setup(t)

	id, err := svc.CreateTransfer(context.Background(), "key1", "w1", "w2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == "" {
		t.Fatal("expected non-empty transfer id")
	}
}

// ---------- TEST: IDEMPOTENCY ----------
func TestCreateTransfer_Idempotency(t *testing.T) {
	svc, _ := setup(t)

	id1, _ := svc.CreateTransfer(context.Background(), "same-key", "w1", "w2", 30)
	id2, _ := svc.CreateTransfer(context.Background(), "same-key", "w1", "w2", 30)

	if id1 != id2 {
		t.Fatalf("expected same transfer id, got %s and %s", id1, id2)
	}
}

// ---------- TEST: INSUFFICIENT FUNDS ----------
func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, _ := setup(t)

	_, err := svc.CreateTransfer(context.Background(), "key2", "w1", "w2", 1000)

	if err == nil {
		t.Fatal("expected insufficient funds error")
	}
}

// ---------- TEST: BALANCE UPDATE ----------
func TestCreateTransfer_BalanceUpdate(t *testing.T) {
	svc, database := setup(t)

	_, _ = svc.CreateTransfer(context.Background(), "key3", "w1", "w2", 40)

	var w1, w2 domain.Wallet
	database.First(&w1, "id = ?", "w1")
	database.First(&w2, "id = ?", "w2")

	if w1.Balance != 60 {
		t.Fatalf("expected w1 balance 60, got %d", w1.Balance)
	}

	if w2.Balance != 40 {
		t.Fatalf("expected w2 balance 40, got %d", w2.Balance)
	}
}

// ---------- TEST: LEDGER ----------
func TestCreateTransfer_LedgerEntries(t *testing.T) {
	svc, database := setup(t)

	transferID, _ := svc.CreateTransfer(context.Background(), "key4", "w1", "w2", 20)

	var entries []domain.LedgerEntry
	database.Where("transfer_id = ?", transferID).Find(&entries)

	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(entries))
	}
}

// ---------- TEST: IDEMPOTENCY STORAGE ----------
func TestCreateTransfer_IdempotencyStoredResponse(t *testing.T) {
	svc, database := setup(t)

	id, _ := svc.CreateTransfer(context.Background(), "key5", "w1", "w2", 10)

	var record domain.IdempotencyRecord
	database.First(&record, "key = ?", "key5")

	var resp map[string]string
	//json.Unmarshal([]byte(record.Response), &resp)
	if err := json.Unmarshal([]byte(record.Response), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp["transferId"] != id {
		t.Fatalf("expected stored transferId %s, got %s", id, resp["transferId"])
	}
}

// ---------- TEST: CONCURRENCY ----------
func TestCreateTransfer_Concurrent(t *testing.T) {
	svc, database := setup(t)

	wg := sync.WaitGroup{}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), string(rune(i)), "w1", "w2", 5)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}

	wg.Wait()

	var w1 domain.Wallet
	database.First(&w1, "id = ?", "w1")

	if w1.Balance < 0 {
		t.Fatal("balance should never be negative")
	}
}
