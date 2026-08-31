package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/domain"
)

const testDatabaseURL = "postgres://postgres:postgres@localhost:5437/wallet_db?sslmode=disable"

// ============================================================
// Test DB Setup
// ============================================================

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()

	db, err := pgxpool.New(ctx, testDatabaseURL)
	if err != nil {
		t.Fatalf("failed to create database pool: %v", err)
	}

	if err := db.Ping(ctx); err != nil {
		db.Close()
		t.Fatalf("failed to connect to database: %v", err)
	}

	return db
}

// ============================================================
// Reset DB
// ============================================================

func resetDatabase(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		TRUNCATE TABLE
			ledger_entries,
			transfers,
			wallets
		RESTART IDENTITY CASCADE
		`,
	)

	if err != nil {
		t.Fatalf("failed to reset database: %v", err)
	}
}

// ============================================================
// Test BeginTx
// ============================================================

func TestPostgresTransferRepository_BeginTx(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	if tx == nil {
		t.Fatal("expected transaction, got nil")
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback transaction: %v", err)
	}
}

// ============================================================
// Test CreateTransfer
// ============================================================

func TestPostgresTransferRepository_CreateTransfer(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	transferID := uuid.New().String()

	transfer := &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: "idem-test-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
		Status:         domain.StatusPending,
	}

	err = repo.CreateTransfer(ctx, tx, transfer)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("failed to create transfer: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	var (
		id             string
		idempotencyKey string
		fromWalletID   string
		toWalletID     string
		amount         int64
		status         string
	)

	err = db.QueryRow(
		ctx,
		`
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		FROM transfers
		WHERE id = $1
		`,
		transfer.ID,
	).Scan(
		&id,
		&idempotencyKey,
		&fromWalletID,
		&toWalletID,
		&amount,
		&status,
	)

	if err != nil {
		t.Fatalf("failed to read transfer: %v", err)
	}

	if id != transfer.ID {
		t.Fatalf("expected ID %s, got %s", transfer.ID, id)
	}

	if idempotencyKey != transfer.IdempotencyKey {
		t.Fatalf(
			"expected idempotency key %s, got %s",
			transfer.IdempotencyKey,
			idempotencyKey,
		)
	}

	if fromWalletID != transfer.FromWalletID {
		t.Fatalf(
			"expected from wallet %s, got %s",
			transfer.FromWalletID,
			fromWalletID,
		)
	}

	if toWalletID != transfer.ToWalletID {
		t.Fatalf(
			"expected to wallet %s, got %s",
			transfer.ToWalletID,
			toWalletID,
		)
	}

	if amount != transfer.Amount {
		t.Fatalf(
			"expected amount %d, got %d",
			transfer.Amount,
			amount,
		)
	}

	if status != string(domain.StatusPending) {
		t.Fatalf(
			"expected status %s, got %s",
			domain.StatusPending,
			status,
		)
	}
}

// ============================================================
// Test GetTransferByIdempotencyKey
// ============================================================

func TestPostgresTransferRepository_GetTransferByIdempotencyKey(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	transferID := uuid.New().String()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES (
			$1,
			'idem-test-002',
			'wallet_1',
			'wallet_2',
			200,
			'PROCESSED'
		)
		`,
		transferID,
	)

	if err != nil {
		t.Fatalf("failed to insert transfer: %v", err)
	}

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	defer tx.Rollback(ctx)

	transfer, err := repo.GetTransferByIdempotencyKey(
		ctx,
		tx,
		"idem-test-002",
	)

	if err != nil {
		t.Fatalf("failed to get transfer: %v", err)
	}

	if transfer == nil {
		t.Fatal("expected transfer, got nil")
	}

	if transfer.ID != transferID {
		t.Fatalf(
			"expected transfer ID %s, got %s",
			transferID,
			transfer.ID,
		)
	}

	if transfer.Amount != 200 {
		t.Fatalf(
			"expected amount 200, got %d",
			transfer.Amount,
		)
	}

	if transfer.Status != domain.StatusProcessed {
		t.Fatalf(
			"expected status PROCESSED, got %s",
			transfer.Status,
		)
	}
}

// ============================================================
// Test GetTransferByIdempotencyKey - Not Found
// ============================================================

func TestPostgresTransferRepository_GetTransferByIdempotencyKey_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	defer tx.Rollback(ctx)

	_, err = repo.GetTransferByIdempotencyKey(
		ctx,
		tx,
		"does-not-exist",
	)

	if err != pgx.ErrNoRows {
		t.Fatalf(
			"expected pgx.ErrNoRows, got %v",
			err,
		)
	}
}

// ============================================================
// Test LockWallet
// ============================================================

func TestPostgresTransferRepository_LockWallet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES ('wallet_1', 1000)
		`,
	)

	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	defer tx.Rollback(ctx)

	wallet, err := repo.LockWallet(
		ctx,
		tx,
		"wallet_1",
	)

	if err != nil {
		t.Fatalf("failed to lock wallet: %v", err)
	}

	if wallet == nil {
		t.Fatal("expected wallet, got nil")
	}

	if wallet.ID != "wallet_1" {
		t.Fatalf(
			"expected wallet_1, got %s",
			wallet.ID,
		)
	}

	if wallet.Balance != 1000 {
		t.Fatalf(
			"expected balance 1000, got %d",
			wallet.Balance,
		)
	}
}

// ============================================================
// Test LockWallet - Not Found
// ============================================================

func TestPostgresTransferRepository_LockWallet_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	defer tx.Rollback(ctx)

	_, err = repo.LockWallet(
		ctx,
		tx,
		"wallet-does-not-exist",
	)

	if err != pgx.ErrNoRows {
		t.Fatalf(
			"expected pgx.ErrNoRows, got %v",
			err,
		)
	}
}

// ============================================================
// Test UpdateWalletBalance
// ============================================================

func TestPostgresTransferRepository_UpdateWalletBalance(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES ('wallet_1', 1000)
		`,
	)

	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	err = repo.UpdateWalletBalance(
		ctx,
		tx,
		"wallet_1",
		900,
	)

	if err != nil {
		tx.Rollback(ctx)

		t.Fatalf(
			"failed to update wallet balance: %v",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	var balance int64

	err = db.QueryRow(
		ctx,
		`
		SELECT balance
		FROM wallets
		WHERE id = 'wallet_1'
		`,
	).Scan(&balance)

	if err != nil {
		t.Fatalf("failed to read wallet balance: %v", err)
	}

	if balance != 900 {
		t.Fatalf(
			"expected balance 900, got %d",
			balance,
		)
	}
}

// ============================================================
// Test CreateLedgerEntry
// ============================================================

func TestPostgresTransferRepository_CreateLedgerEntry(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES
			('wallet_1', 1000),
			('wallet_2', 500)
		`,
	)

	if err != nil {
		t.Fatalf("failed to create wallets: %v", err)
	}

	transferID := uuid.New().String()

	_, err = db.Exec(
		ctx,
		`
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES (
			$1,
			'ledger-idem-001',
			'wallet_1',
			'wallet_2',
			100,
			'PENDING'
		)
		`,
		transferID,
	)

	if err != nil {
		t.Fatalf("failed to create transfer: %v", err)
	}

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	defer tx.Rollback(ctx)

	debitEntry := &domain.LedgerEntry{
		WalletID:   "wallet_1",
		TransferID: transferID,
		Type:       domain.LedgerDebit,
		Amount:     100,
	}

	err = repo.CreateLedgerEntry(
		ctx,
		tx,
		debitEntry,
	)

	if err != nil {
		t.Fatalf(
			"failed to create debit entry: %v",
			err,
		)
	}

	creditEntry := &domain.LedgerEntry{
		WalletID:   "wallet_2",
		TransferID: transferID,
		Type:       domain.LedgerCredit,
		Amount:     100,
	}

	err = repo.CreateLedgerEntry(
		ctx,
		tx,
		creditEntry,
	)

	if err != nil {
		t.Fatalf(
			"failed to create credit entry: %v",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	var count int

	err = db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
		`,
		transferID,
	).Scan(&count)

	if err != nil {
		t.Fatalf(
			"failed to count ledger entries: %v",
			err,
		)
	}

	if count != 2 {
		t.Fatalf(
			"expected 2 ledger entries, got %d",
			count,
		)
	}
}

// ============================================================
// Test UpdateTransferStatus
// ============================================================

func TestPostgresTransferRepository_UpdateTransferStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	transferID := uuid.New().String()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES (
			$1,
			'status-idem-001',
			'wallet_1',
			'wallet_2',
			100,
			'PENDING'
		)
		`,
		transferID,
	)

	if err != nil {
		t.Fatalf(
			"failed to create transfer: %v",
			err,
		)
	}

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	defer tx.Rollback(ctx)

	response := []byte(
		`{"transferId":"` + transferID + `","status":"PROCESSED"}`,
	)

	err = repo.UpdateTransferStatus(
		ctx,
		tx,
		transferID,
		domain.StatusProcessed,
		response,
	)

	if err != nil {
		t.Fatalf(
			"failed to update transfer status: %v",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf(
			"failed to commit transaction: %v",
			err,
		)
	}

	var (
		status     string
		responseDB []byte
	)

	err = db.QueryRow(
		ctx,
		`
		SELECT status, response
		FROM transfers
		WHERE id = $1
		`,
		transferID,
	).Scan(
		&status,
		&responseDB,
	)

	if err != nil {
		t.Fatalf(
			"failed to read transfer: %v",
			err,
		)
	}

	if status != string(domain.StatusProcessed) {
		t.Fatalf(
			"expected PROCESSED, got %s",
			status,
		)
	}

	var expectedResponse map[string]interface{}
	var actualResponse map[string]interface{}

	if err := json.Unmarshal(response, &expectedResponse); err != nil {
		t.Fatalf("failed to unmarshal expected response: %v", err)
	}

	if err := json.Unmarshal(responseDB, &actualResponse); err != nil {
		t.Fatalf("failed to unmarshal database response: %v", err)
	}

	if !reflect.DeepEqual(expectedResponse, actualResponse) {
		t.Fatalf(
			"expected response %s, got %s",
			string(response),
			string(responseDB),
		)
	}
}

// ============================================================
// Test Transaction Rollback
// ============================================================

func TestPostgresTransferRepository_TransactionRollback(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	tx, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf(
			"failed to begin transaction: %v",
			err,
		)
	}

	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES ('wallet_rollback', 1000)
		`,
	)

	if err != nil {
		tx.Rollback(ctx)

		t.Fatalf(
			"failed to insert wallet: %v",
			err,
		)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf(
			"failed to rollback: %v",
			err,
		)
	}

	var count int

	err = db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM wallets
		WHERE id = 'wallet_rollback'
		`,
	).Scan(&count)

	if err != nil {
		t.Fatalf(
			"failed to check wallet: %v",
			err,
		)
	}

	if count != 0 {
		t.Fatalf(
			"expected wallet not to exist after rollback, got %d",
			count,
		)
	}
}

// ============================================================
// Test Row-Level Locking
// ============================================================

func TestPostgresTransferRepository_LockWallet_Concurrent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	resetDatabase(t, db)

	repo := NewPostgresTransferRepository(db)

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES ('wallet_lock_test', 1000)
		`,
	)

	if err != nil {
		t.Fatalf(
			"failed to create wallet: %v",
			err,
		)
	}

	// --------------------------------------------------------
	// Transaction 1 gets the lock.
	// --------------------------------------------------------

	tx1, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf(
			"failed to begin tx1: %v",
			err,
		)
	}

	_, err = repo.LockWallet(
		ctx,
		tx1,
		"wallet_lock_test",
	)

	if err != nil {
		tx1.Rollback(ctx)

		t.Fatalf(
			"failed to lock wallet in tx1: %v",
			err,
		)
	}

	// --------------------------------------------------------
	// Transaction 2 attempts to get the same lock.
	// --------------------------------------------------------

	tx2, err := repo.BeginTx(ctx)
	if err != nil {
		tx1.Rollback(ctx)

		t.Fatalf(
			"failed to begin tx2: %v",
			err,
		)
	}

	lockCtx, cancel := context.WithTimeout(
		ctx,
		100*time.Millisecond,
	)
	defer cancel()

	_, err = repo.LockWallet(
		lockCtx,
		tx2,
		"wallet_lock_test",
	)

	// Transaction 2 should fail because tx1
	// is still holding the row lock.

	if err == nil {
		tx1.Rollback(ctx)
		tx2.Rollback(ctx)

		t.Fatal(
			"expected second transaction to wait for row lock",
		)
	}

	// Release transaction 1.

	if err := tx1.Rollback(ctx); err != nil {
		t.Fatalf(
			"failed to rollback tx1: %v",
			err,
		)
	}

	// Transaction 2 may already be aborted because
	// its context timed out.
	_ = tx2.Rollback(ctx)
}
