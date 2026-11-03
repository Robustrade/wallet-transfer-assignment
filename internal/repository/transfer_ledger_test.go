package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
)

func TestTransferRepositoryCreateAndGet(t *testing.T) {
	t.Run("create pending transfer", func(t *testing.T) {
		db := testRepository(t, queryResult{
			contains: "INSERT INTO transfers",
			columns: []string{"id", "from_wallet_id", "to_wallet_id", "amount", "status", "failure_code"},
			values: []driver.Value{"t-1", "w-1", "w-2", int64(125), "PENDING", nil},
		})
		got, err := NewTransferRepository(db.db).Create(context.Background(), domain.Transfer{
			ID: "t-1", FromWalletID: "w-1", ToWalletID: "w-2", Amount: 125, Status: domain.TransferPending,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "t-1" || got.Amount != 125 || got.Status != domain.TransferPending {
			t.Fatalf("created transfer = %+v", got)
		}
	})
	t.Run("get missing transfer", func(t *testing.T) {
		db := testRepository(t, queryResult{contains: "FROM transfers", err: sql.ErrNoRows})
		_, err := NewTransferRepository(db.db).GetByID(context.Background(), "missing")
		if !errors.Is(err, ErrTransferNotFound) {
			t.Fatalf("GetByID() error = %v, want %v", err, ErrTransferNotFound)
		}
	})
}

func TestTransferRepositoryRejectsInvalidTransfer(t *testing.T) {
	db := testRepository(t)
	_, err := NewTransferRepository(db.db).Create(context.Background(), domain.Transfer{
		ID: "t-1", FromWalletID: "w-1", ToWalletID: "w-1", Amount: 100, Status: domain.TransferPending,
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("Create() error = %v, want %v", err, domain.ErrSameWallet)
	}
}

func TestTransferRepositoryStatusTransitions(t *testing.T) {
	t.Run("mark processed", func(t *testing.T) {
		db := testRepository(t, queryResult{
			contains: "SET status = 'PROCESSED'",
			columns: []string{"id", "from_wallet_id", "to_wallet_id", "amount", "status", "failure_code"},
			values: []driver.Value{"t-1", "w-1", "w-2", int64(50), "PROCESSED", nil},
		})
		got, err := NewTransferRepository(db.db).MarkProcessed(context.Background(), "t-1")
		if err != nil || got.Status != domain.TransferProcessed {
			t.Fatalf("MarkProcessed() = %+v, error = %v", got, err)
		}
	})
	t.Run("mark failed", func(t *testing.T) {
		db := testRepository(t, queryResult{
			contains: "SET status = 'FAILED'",
			columns: []string{"id", "from_wallet_id", "to_wallet_id", "amount", "status", "failure_code"},
			values: []driver.Value{"t-1", "w-1", "w-2", int64(50), "FAILED", "INSUFFICIENT_FUNDS"},
		})
		got, err := NewTransferRepository(db.db).MarkFailed(context.Background(), "t-1", "INSUFFICIENT_FUNDS")
		if err != nil || got.Status != domain.TransferFailed || got.FailureCode != "INSUFFICIENT_FUNDS" {
			t.Fatalf("MarkFailed() = %+v, error = %v", got, err)
		}
	})
}

func TestLedgerRepositoryCreate(t *testing.T) {
	db := testRepository(t, queryResult{
		contains: "INSERT INTO ledger_entries",
		columns: []string{"id", "transfer_id", "wallet_id", "entry_type", "amount"},
		values: []driver.Value{"e-1", "t-1", "w-1", "DEBIT", int64(75)},
	})
	got, err := NewLedgerRepository(db.db).Create(context.Background(), domain.LedgerEntry{
		ID: "e-1", TransferID: "t-1", WalletID: "w-1", Type: domain.EntryDebit, Amount: 75,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != domain.EntryDebit || got.Amount != 75 {
		t.Fatalf("created ledger entry = %+v", got)
	}
}

func TestLedgerRepositoryRejectsInvalidEntry(t *testing.T) {
	db := testRepository(t)
	_, err := NewLedgerRepository(db.db).Create(context.Background(), domain.LedgerEntry{
		ID: "e-1", TransferID: "t-1", WalletID: "w-1", Type: domain.EntryDebit, Amount: 0,
	})
	if !errors.Is(err, domain.ErrInvalidLedgerEntry) {
		t.Fatalf("Create() error = %v, want %v", err, domain.ErrInvalidLedgerEntry)
	}
}
