package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

const testDatabaseURL = "postgres://postgres:postgres@localhost:5437/wallet_db?sslmode=disable"

// ============================================================
// Test database setup
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
// Reset database
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
// Create test wallets
// ============================================================

func createWallets(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES
			('wallet_1', 1000),
			('wallet_2', 500),
			('wallet_3', 0)
		`,
	)

	if err != nil {
		t.Fatalf("failed to create test wallets: %v", err)
	}
}

// ============================================================
// Create service
// ============================================================

func setupService(t *testing.T) (*TransferService, *pgxpool.Pool) {
	t.Helper()

	db := setupTestDB(t)

	repo := repository.NewPostgresTransferRepository(db)

	service := NewTransferService(repo)

	return service, db
}

// ============================================================
// Test successful transfer
// ============================================================

func TestCreateTransfer_Success(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)
	createWallets(t, db)

	ctx := context.Background()

	input := CreateTransferInput{
		IdempotencyKey: "test-success-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	response, err := service.CreateTransfer(ctx, input)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if response == nil {
		t.Fatal("expected response, got nil")
	}

	if response.Status != string(domain.StatusProcessed) {
		t.Fatalf(
			"expected status %s, got %s",
			domain.StatusProcessed,
			response.Status,
		)
	}

	if response.Amount != 100 {
		t.Fatalf(
			"expected amount 100, got %d",
			response.Amount,
		)
	}

	// Check balances.

	var sourceBalance int64
	var destinationBalance int64

	err = db.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = 'wallet_1'`,
	).Scan(&sourceBalance)

	if err != nil {
		t.Fatalf("failed to read source balance: %v", err)
	}

	err = db.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = 'wallet_2'`,
	).Scan(&destinationBalance)

	if err != nil {
		t.Fatalf("failed to read destination balance: %v", err)
	}

	if sourceBalance != 900 {
		t.Fatalf(
			"expected source balance 900, got %d",
			sourceBalance,
		)
	}

	if destinationBalance != 600 {
		t.Fatalf(
			"expected destination balance 600, got %d",
			destinationBalance,
		)
	}
}

// ============================================================
// Test idempotency
// ============================================================

func TestCreateTransfer_Idempotency(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)
	createWallets(t, db)

	ctx := context.Background()

	input := CreateTransferInput{
		IdempotencyKey: "same-key-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	// First request.

	firstResponse, err := service.CreateTransfer(ctx, input)

	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	// Second request with exactly the same idempotency key.

	secondResponse, err := service.CreateTransfer(ctx, input)

	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	// Both requests must return the same transfer.

	if firstResponse.TransferID != secondResponse.TransferID {
		t.Fatalf(
			"expected same transfer ID, got first=%s second=%s",
			firstResponse.TransferID,
			secondResponse.TransferID,
		)
	}

	// Verify only one transfer exists.

	var transferCount int

	err = db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM transfers
		WHERE idempotency_key = $1
		`,
		input.IdempotencyKey,
	).Scan(&transferCount)

	if err != nil {
		t.Fatalf("failed to count transfers: %v", err)
	}

	if transferCount != 1 {
		t.Fatalf(
			"expected 1 transfer, got %d",
			transferCount,
		)
	}

	// Verify balances were changed only once.

	var sourceBalance int64
	var destinationBalance int64

	err = db.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = 'wallet_1'`,
	).Scan(&sourceBalance)

	if err != nil {
		t.Fatalf("failed to read source balance: %v", err)
	}

	err = db.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = 'wallet_2'`,
	).Scan(&destinationBalance)

	if err != nil {
		t.Fatalf("failed to read destination balance: %v", err)
	}

	if sourceBalance != 900 {
		t.Fatalf(
			"expected source balance 900, got %d",
			sourceBalance,
		)
	}

	if destinationBalance != 600 {
		t.Fatalf(
			"expected destination balance 600, got %d",
			destinationBalance,
		)
	}

	// Verify exactly two ledger entries exist.

	var ledgerCount int

	err = db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
		`,
		firstResponse.TransferID,
	).Scan(&ledgerCount)

	if err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"expected 2 ledger entries, got %d",
			ledgerCount,
		)
	}
}

// ============================================================
// Test idempotency conflict
// ============================================================

func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)
	createWallets(t, db)

	ctx := context.Background()

	firstInput := CreateTransferInput{
		IdempotencyKey: "conflict-key-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	_, err := service.CreateTransfer(ctx, firstInput)

	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	// Same key but different amount.

	secondInput := CreateTransferInput{
		IdempotencyKey: "conflict-key-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	}

	_, err = service.CreateTransfer(ctx, secondInput)

	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf(
			"expected ErrIdempotencyConflict, got %v",
			err,
		)
	}
}

// ============================================================
// Test insufficient balance
// ============================================================

func TestCreateTransfer_InsufficientBalance(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)

	ctx := context.Background()

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES
			('wallet_1', 50),
			('wallet_2', 500)
		`,
	)

	if err != nil {
		t.Fatalf("failed to create wallets: %v", err)
	}

	input := CreateTransferInput{
		IdempotencyKey: "insufficient-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	response, err := service.CreateTransfer(ctx, input)

	if err != nil {
		t.Fatalf("expected no service error, got: %v", err)
	}

	if response.Status != string(domain.StatusFailed) {
		t.Fatalf(
			"expected FAILED, got %s",
			response.Status,
		)
	}

	// Source balance must remain unchanged.

	var sourceBalance int64

	err = db.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = 'wallet_1'`,
	).Scan(&sourceBalance)

	if err != nil {
		t.Fatalf("failed to read balance: %v", err)
	}

	if sourceBalance != 50 {
		t.Fatalf(
			"expected source balance 50, got %d",
			sourceBalance,
		)
	}

	// No ledger entries should be created.

	var ledgerCount int

	err = db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
		`,
		response.TransferID,
	).Scan(&ledgerCount)

	if err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}

	if ledgerCount != 0 {
		t.Fatalf(
			"expected 0 ledger entries, got %d",
			ledgerCount,
		)
	}
}

// ============================================================
// Test wallet not found
// ============================================================

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)

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

	input := CreateTransferInput{
		IdempotencyKey: "wallet-not-found-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet-does-not-exist",
		Amount:         100,
	}

	_, err = service.CreateTransfer(ctx, input)

	if !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf(
			"expected ErrWalletNotFound, got %v",
			err,
		)
	}

	// The transaction should have rolled back.
	// Therefore no transfer should remain.

	var transferCount int

	err = db.QueryRow(
		ctx,
		`
		SELECT COUNT(*)
		FROM transfers
		WHERE idempotency_key = $1
		`,
		input.IdempotencyKey,
	).Scan(&transferCount)

	if err != nil {
		t.Fatalf("failed to count transfers: %v", err)
	}

	if transferCount != 0 {
		t.Fatalf(
			"expected 0 transfers after rollback, got %d",
			transferCount,
		)
	}
}

// ============================================================
// Test double-entry ledger
// ============================================================

func TestCreateTransfer_LedgerCorrectness(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)
	createWallets(t, db)

	ctx := context.Background()

	input := CreateTransferInput{
		IdempotencyKey: "ledger-test-001",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	response, err := service.CreateTransfer(ctx, input)

	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	rows, err := db.Query(
		ctx,
		`
		SELECT wallet_id, type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY id
		`,
		response.TransferID,
	)

	if err != nil {
		t.Fatalf("failed to query ledger: %v", err)
	}

	defer rows.Close()

	type ledgerResult struct {
		walletID string
		typ      string
		amount   int64
	}

	var entries []ledgerResult

	for rows.Next() {
		var entry ledgerResult

		if err := rows.Scan(
			&entry.walletID,
			&entry.typ,
			&entry.amount,
		); err != nil {
			t.Fatalf("failed to scan ledger entry: %v", err)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("ledger rows error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			len(entries),
		)
	}

	// First entry must be DEBIT.

	if entries[0].walletID != "wallet_1" {
		t.Fatalf(
			"expected first entry wallet_1, got %s",
			entries[0].walletID,
		)
	}

	if entries[0].typ != string(domain.LedgerDebit) {
		t.Fatalf(
			"expected DEBIT, got %s",
			entries[0].typ,
		)
	}

	if entries[0].amount != 100 {
		t.Fatalf(
			"expected debit amount 100, got %d",
			entries[0].amount,
		)
	}

	// Second entry must be CREDIT.

	if entries[1].walletID != "wallet_2" {
		t.Fatalf(
			"expected second entry wallet_2, got %s",
			entries[1].walletID,
		)
	}

	if entries[1].typ != string(domain.LedgerCredit) {
		t.Fatalf(
			"expected CREDIT, got %s",
			entries[1].typ,
		)
	}

	if entries[1].amount != 100 {
		t.Fatalf(
			"expected credit amount 100, got %d",
			entries[1].amount,
		)
	}
}

// ============================================================
// Test concurrent transfers
// ============================================================

func TestCreateTransfer_ConcurrentTransfers(t *testing.T) {
	service, db := setupService(t)
	defer db.Close()

	resetDatabase(t, db)

	ctx := context.Background()

	// wallet_1 has exactly 1000.

	_, err := db.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES
			('wallet_1', 1000),
			('wallet_2', 0),
			('wallet_3', 0)
		`,
	)

	if err != nil {
		t.Fatalf("failed to create wallets: %v", err)
	}

	// Two concurrent transfers:
	//
	// wallet_1 -> wallet_2 = 700
	// wallet_1 -> wallet_3 = 500
	//
	// Total requested = 1200.
	//
	// Only one should succeed.

	type result struct {
		response *TransferResponse
		err      error
	}

	results := make(chan result, 2)

	go func() {
		response, err := service.CreateTransfer(
			ctx,
			CreateTransferInput{
				IdempotencyKey: "concurrent-001",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         700,
			},
		)

		results <- result{
			response: response,
			err:      err,
		}
	}()

	go func() {
		response, err := service.CreateTransfer(
			ctx,
			CreateTransferInput{
				IdempotencyKey: "concurrent-002",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_3",
				Amount:         500,
			},
		)

		results <- result{
			response: response,
			err:      err,
		}
	}()

	first := <-results
	second := <-results

	successCount := 0

	for _, result := range []result{first, second} {

		if result.err != nil {
			continue
		}

		if result.response != nil &&
			result.response.Status == string(domain.StatusProcessed) {

			successCount++
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"expected exactly 1 successful transfer, got %d",
			successCount,
		)
	}

	// wallet_1 must have exactly 300 remaining.

	var sourceBalance int64

	err = db.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = 'wallet_1'`,
	).Scan(&sourceBalance)

	if err != nil {
		t.Fatalf("failed to read source balance: %v", err)
	}

	if sourceBalance != 300 {
		t.Fatalf(
			"expected wallet_1 balance 300, got %d",
			sourceBalance,
		)
	}

	// Total money must remain 1000.

	var totalBalance int64

	err = db.QueryRow(
		ctx,
		`SELECT COALESCE(SUM(balance), 0) FROM wallets`,
	).Scan(&totalBalance)

	if err != nil {
		t.Fatalf("failed to calculate total balance: %v", err)
	}

	if totalBalance != 1000 {
		t.Fatalf(
			"expected total balance 1000, got %d",
			totalBalance,
		)
	}

	// Exactly two ledger entries should exist for
	// the successful transfer.

	var ledgerCount int

	err = db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM ledger_entries`,
	).Scan(&ledgerCount)

	if err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			ledgerCount,
		)
	}
}
