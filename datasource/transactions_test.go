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
