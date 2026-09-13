package datasource

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type testLogger struct{}

func (testLogger) Info(string, ...zap.Field)  {}
func (testLogger) Error(string, ...zap.Field) {}
func (testLogger) Warn(string, ...zap.Field)  {}
func (testLogger) Debug(string, ...zap.Field) {}
func (testLogger) Sync()                      {}

func newTestDataSource(t *testing.T) DataSource {
	t.Helper()

	con, err := sql.Open("sqlite", "file:transactions-test?mode=memory&cache=shared&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	con.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = con.Close() })

	if _, err := con.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := con.Exec(testSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	return DataSource{
		log:     testLogger{},
		db:      con,
		Queries: db.New(con),
	}
}

const testSchema = `
CREATE TABLE wallet (
    wallet_id TEXT PRIMARY KEY,
    balance INTEGER NOT NULL CHECK (balance >= 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE transfer (
    transaction_id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallet(wallet_id),
    to_wallet_id TEXT NOT NULL REFERENCES wallet(wallet_id),
    amount INTEGER NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    comments TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (from_wallet_id <> to_wallet_id)
);
CREATE TABLE ledger_entry (
    entry_id TEXT PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallet(wallet_id),
    transaction_id TEXT NOT NULL REFERENCES transfer(transaction_id),
    amount INTEGER NOT NULL CHECK (amount > 0),
    balance_after INTEGER NOT NULL,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE idempotency_record (
    idempotency_key TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transfer(transaction_id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

func addWallet(t *testing.T, dataSource DataSource, walletID string, balance int64) {
	t.Helper()
	if _, err := dataSource.db.ExecContext(context.Background(),
		"INSERT INTO wallet (wallet_id, balance) VALUES (?, ?)", walletID, balance,
	); err != nil {
		t.Fatalf("create wallet %q: %v", walletID, err)
	}
}

func addPendingTransfer(t *testing.T, dataSource DataSource, transactionID, fromWalletID, toWalletID string, amount int64) {
	t.Helper()
	_, err := dataSource.Queries.CreateTransfer(context.Background(), db.CreateTransferParams{
		TransactionID: transactionID,
		FromWalletID:  fromWalletID,
		ToWalletID:    toWalletID,
		Amount:        amount,
		Status:        string(utils.PENDING),
	})
	if err != nil {
		t.Fatalf("create pending transfer: %v", err)
	}
}

func TestTransferIsIdempotent(t *testing.T) {
	dataSource := newTestDataSource(t)
	addWallet(t, dataSource, "wallet_1", 500)
	addWallet(t, dataSource, "wallet_2", 100)
	request := db.CreateTransferParams{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       125,
		Status:       string(utils.PENDING),
	}

	firstID, err := dataSource.Transfer(context.Background(), request, "request_1")
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	secondID, err := dataSource.Transfer(context.Background(), request, "request_1")
	if err != nil {
		t.Fatalf("replayed transfer: %v", err)
	}
	if firstID == nil || secondID == nil || *firstID != *secondID {
		t.Fatalf("idempotent transfer IDs differ: first=%v second=%v", firstID, secondID)
	}

	sender, err := dataSource.Queries.GetWallet(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := dataSource.Queries.GetWallet(context.Background(), "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	if sender.Balance != 375 || receiver.Balance != 225 {
		t.Fatalf("duplicate transfer changed balances: sender=%d receiver=%d", sender.Balance, receiver.Balance)
	}
}

func TestProcessTransferCreatesBalancedLedgerAndProcessesTransfer(t *testing.T) {
	dataSource := newTestDataSource(t)
	addWallet(t, dataSource, "wallet_1", 500)
	addWallet(t, dataSource, "wallet_2", 100)
	addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 125)

	err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{
		TransactionID: "transaction_1",
		FromWalletID:  "wallet_1",
		ToWalletID:    "wallet_2",
		Amount:        125,
	}, "transaction_1")
	if err != nil {
		t.Fatalf("process transfer: %v", err)
	}

	transfer, err := dataSource.Queries.GetTransactionById(context.Background(), "transaction_1")
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Status != string(utils.PROCESSED) {
		t.Fatalf("transfer status: got %q, want %q", transfer.Status, utils.PROCESSED)
	}

	sender, err := dataSource.Queries.GetWallet(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := dataSource.Queries.GetWallet(context.Background(), "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	if sender.Balance != 375 || receiver.Balance != 225 {
		t.Fatalf("balances: sender=%d receiver=%d", sender.Balance, receiver.Balance)
	}

	entries, err := dataSource.Queries.GetStatement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entry count: got %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.TransactionID != "transaction_1" || entry.Amount != 125 {
			t.Errorf("invalid ledger entry: %#v", entry)
		}
	}
}

func TestMarkTransferFailedOnlyTransitionsPendingTransfer(t *testing.T) {
	dataSource := newTestDataSource(t)
	addWallet(t, dataSource, "wallet_1", 100)
	addWallet(t, dataSource, "wallet_2", 100)
	addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 150)

	if err := markTransferFailed(context.Background(), dataSource, "transaction_1"); err != nil {
		t.Fatalf("mark transfer failed: %v", err)
	}
	transfer, err := dataSource.Queries.GetTransactionById(context.Background(), "transaction_1")
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Status != string(utils.FAILED) {
		t.Fatalf("transfer status: got %q, want %q", transfer.Status, utils.FAILED)
	}

	if err := markTransferFailed(context.Background(), dataSource, "transaction_1"); err == nil {
		t.Fatal("expected a second failure transition to be rejected")
	}
}

func TestProcessTransferDoesNotOverspendUnderConcurrentDebits(t *testing.T) {
	dataSource := newTestDataSource(t)
	addWallet(t, dataSource, "wallet_1", 100)
	addWallet(t, dataSource, "wallet_2", 0)
	addWallet(t, dataSource, "wallet_3", 0)
	addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 75)
	addPendingTransfer(t, dataSource, "transaction_2", "wallet_1", "wallet_3", 75)

	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, 2)
	for _, transactionID := range []string{"transaction_1", "transaction_2"} {
		waitGroup.Add(1)
		go func(transactionID string) {
			defer waitGroup.Done()
			errorsFound <- processTransfer(context.Background(), dataSource, db.CreateTransferParams{
				TransactionID: transactionID,
				FromWalletID:  "wallet_1",
				ToWalletID:    map[string]string{"transaction_1": "wallet_2", "transaction_2": "wallet_3"}[transactionID],
				Amount:        75,
			}, transactionID)
		}(transactionID)
	}
	waitGroup.Wait()
	close(errorsFound)

	var successfulTransfers int
	for err := range errorsFound {
		if err == nil {
			successfulTransfers++
		} else if !errors.Is(err, sql.ErrNoRows) &&
			!strings.Contains(err.Error(), "insufficient balance") &&
			!strings.Contains(err.Error(), "database table is locked") {
			// SQLite may serialize one transaction behind the other; unexpected errors still fail the test.
			t.Fatalf("concurrent transfer: %v", err)
		}
	}
	if successfulTransfers != 1 {
		t.Fatalf("successful concurrent transfers: got %d, want 1", successfulTransfers)
	}

	sender, err := dataSource.Queries.GetWallet(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if sender.Balance != 25 {
		t.Fatalf("sender balance: got %d, want 25", sender.Balance)
	}
}

func TestWalletDataSourceMethods(t *testing.T) {
	dataSource := newTestDataSource(t)
	addWallet(t, dataSource, "wallet_1", 100)

	wallets, err := dataSource.GetWallets(context.Background())
	if err != nil || len(wallets) != 1 || wallets[0].WalletID != "wallet_1" {
		t.Fatalf("get wallets: %#v, %v", wallets, err)
	}
	wallet, err := dataSource.GetWalletById(context.Background(), "wallet_1")
	if err != nil || wallet.WalletID != "wallet_1" {
		t.Fatalf("get wallet: %#v, %v", wallet, err)
	}
	if _, err := dataSource.GetWalletById(context.Background(), "missing"); err == nil {
		t.Fatal("expected missing wallet error")
	}
}

func TestDatasourceTransactionFailures(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		if err := dataSource.db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := dataSource.Transfer(context.Background(), db.CreateTransferParams{}, "key"); err == nil {
			t.Fatal("expected transfer begin error")
		}
		if err := markTransferFailed(context.Background(), dataSource, "id"); err == nil {
			t.Fatal("expected failed-status begin error")
		}
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{}, "id"); err == nil {
			t.Fatal("expected process begin error")
		}
	})

	t.Run("missing sender and receiver", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{FromWalletID: "missing", ToWalletID: "also-missing", Amount: 1}, "id"); err == nil {
			t.Fatal("expected sender lookup error")
		}
		addWallet(t, dataSource, "wallet_1", 100)
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{FromWalletID: "wallet_1", ToWalletID: "missing", Amount: 1}, "id"); err == nil {
			t.Fatal("expected receiver lookup error")
		}
	})

	t.Run("insufficient balance", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		addWallet(t, dataSource, "wallet_1", 1)
		addWallet(t, dataSource, "wallet_2", 0)
		addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 2)
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 2}, "transaction_1"); err == nil {
			t.Fatal("expected insufficient balance error")
		}
	})

	t.Run("invalid transfer state", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		addWallet(t, dataSource, "wallet_1", 100)
		addWallet(t, dataSource, "wallet_2", 0)
		addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 1)
		if _, err := dataSource.Queries.UpdateTransferStatus(context.Background(), db.UpdateTransferStatusParams{Status: string(utils.PROCESSED), TransactionID: "transaction_1"}); err != nil {
			t.Fatal(err)
		}
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1}, "transaction_1"); err == nil {
			t.Fatal("expected invalid state transition error")
		}
	})
}

func TestTransferFailureBranches(t *testing.T) {
	t.Run("idempotency lookup error", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		if _, err := dataSource.db.Exec("DROP TABLE idempotency_record"); err != nil {
			t.Fatal(err)
		}
		if _, err := dataSource.Transfer(context.Background(), db.CreateTransferParams{}, "key"); err == nil {
			t.Fatal("expected idempotency lookup error")
		}
	})

	t.Run("create transfer error", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		if _, err := dataSource.Transfer(context.Background(), db.CreateTransferParams{
			FromWalletID: "wallet_1",
			ToWalletID:   "wallet_2",
			Amount:       0,
			Status:       string(utils.PENDING),
		}, "key"); err == nil {
			t.Fatal("expected create transfer error")
		}
	})

	t.Run("idempotency insert error", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		if _, err := dataSource.db.Exec(`CREATE TRIGGER reject_idempotency BEFORE INSERT ON idempotency_record BEGIN SELECT RAISE(ABORT, 'reject idempotency'); END;`); err != nil {
			t.Fatal(err)
		}
		if _, err := dataSource.Transfer(context.Background(), db.CreateTransferParams{
			FromWalletID: "wallet_1",
			ToWalletID:   "wallet_2",
			Amount:       1,
			Status:       string(utils.PENDING),
		}, "key"); err == nil {
			t.Fatal("expected idempotency insert error")
		}
	})

	t.Run("processing failure marks transfer failed", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		addWallet(t, dataSource, "wallet_1", 1)
		addWallet(t, dataSource, "wallet_2", 0)
		_, err := dataSource.Transfer(context.Background(), db.CreateTransferParams{
			FromWalletID: "wallet_1",
			ToWalletID:   "wallet_2",
			Amount:       2,
			Status:       string(utils.PENDING),
		}, "key")
		if err == nil {
			t.Fatal("expected processing failure")
		}
		transfers, queryErr := dataSource.Queries.GetTransactions(context.Background())
		if queryErr != nil || len(transfers) != 1 || transfers[0].Status != string(utils.FAILED) {
			t.Fatalf("failed transfer state: %#v, %v", transfers, queryErr)
		}
	})

	t.Run("processing and failed-state errors", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		addWallet(t, dataSource, "wallet_1", 100)
		addWallet(t, dataSource, "wallet_2", 0)
		addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 1)
		if _, err := dataSource.db.Exec(`CREATE TRIGGER reject_transfer_update BEFORE UPDATE ON transfer BEGIN SELECT RAISE(ABORT, 'reject transfer update'); END;`); err != nil {
			t.Fatal(err)
		}
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1}, "transaction_1"); err == nil {
			t.Fatal("expected transfer status update error")
		}
		if err := markTransferFailed(context.Background(), dataSource, "transaction_1"); err == nil {
			t.Fatal("expected failed status update error")
		}
	})

	t.Run("ledger failure", func(t *testing.T) {
		dataSource := newTestDataSource(t)
		addWallet(t, dataSource, "wallet_1", 100)
		addWallet(t, dataSource, "wallet_2", 0)
		addPendingTransfer(t, dataSource, "transaction_1", "wallet_1", "wallet_2", 1)
		if _, err := dataSource.db.Exec("DROP TABLE ledger_entry"); err != nil {
			t.Fatal(err)
		}
		if err := processTransfer(context.Background(), dataSource, db.CreateTransferParams{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1}, "transaction_1"); err == nil {
			t.Fatal("expected ledger error")
		}
	})
}
