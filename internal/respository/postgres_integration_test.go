package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaPrefix = "test_repo_"

func connectTestDB(t *testing.T) (*pgxpool.Pool, string, func()) {
	t.Helper()
	connString := os.Getenv("PGX_TEST_CONN_STRING")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("integration test requires PGX_TEST_CONN_STRING or DATABASE_URL")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}

	schemaName := fmt.Sprintf("%s%d", schemaPrefix, time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schemaName}.Sanitize())); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to parse conn string: %v", err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect with schema search path: %v", err)
	}

	if err := createSchemaObjects(ctx, pool); err != nil {
		pool.Close()
		adminPool.Close()
		t.Fatalf("failed to create schema objects: %v", err)
	}

	cleanup := func() {
		pool.Close()
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", pgx.Identifier{schemaName}.Sanitize())); err != nil {
			t.Logf("failed to drop schema %s: %v", schemaName, err)
		}
		adminPool.Close()
	}

	return pool, schemaName, cleanup
}

func createSchemaObjects(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, strings.TrimSpace(`
CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id UUID NOT NULL,
    to_wallet_id UUID NOT NULL,
    amount BIGINT NOT NULL,
    status transfer_status NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    entry_type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id UUID NOT NULL,
    request_hash TEXT NOT NULL,
	response_json JSONB NOT NULL,
	status_code INTEGER NOT NULL,
	created_by TEXT NULL,
	caller_ip TEXT NULL,
	user_agent TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`))
	return err
}

// setupTest initializes a test database and repositories, returning them and a cleanup function.
func setupTest(t *testing.T) (*pgxpool.Pool, WalletRepository, TransferRepository, LedgerRepository, IdempotencyRepository, TransactionManager, func()) {
	t.Helper()
	pool, _, cleanup := connectTestDB(t)

	walletRepo := NewPostgresWalletRepository(pool)
	transferRepo := NewPostgresTransferRepository(pool)
	ledgerRepo := NewPostgresLedgerRepository(pool)
	idemRepo := NewPostgresIdempotencyRepository(pool)
	manager := NewPostgresTransactionManager(pool)

	return pool, walletRepo, transferRepo, ledgerRepo, idemRepo, manager, cleanup
}

func TestPostgresWalletRepository_CreateAndGet(t *testing.T) {
	_, walletRepo, _, _, _, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()

	wallet := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	if err := walletRepo.Create(ctx, wallet); err != nil {
		t.Fatalf("wallet create failed: %v", err)
	}

	loaded, err := walletRepo.GetByID(ctx, wallet.ID)
	if err != nil {
		t.Fatalf("wallet get failed: %v", err)
	}
	if loaded.Balance != 500 {
		t.Fatalf("expected balance 500, got %d", loaded.Balance)
	}
}

func TestPostgresWalletRepository_UpdateInTransaction(t *testing.T) {
	_, walletRepo, _, _, _, manager, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	wallet := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	_ = walletRepo.Create(ctx, wallet) // Pre-create wallet for update test

	if err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		locked, err := walletRepo.GetByIDForUpdate(txCtx, wallet.ID)
		if err != nil {
			return err
		}
		locked.Balance = 400
		return walletRepo.Update(txCtx, locked)
	}); err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	loaded, err := walletRepo.GetByID(ctx, wallet.ID)
	if err != nil {
		t.Fatalf("wallet get failed: %v", err)
	}
	if loaded.Balance != 400 {
		t.Fatalf("expected balance 400 after update, got %d", loaded.Balance)
	}
}

func TestPostgresTransferRepository_CreateAndGet(t *testing.T) {
	_, _, transferRepo, _, _, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	walletA := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	walletB := &domain.Wallet{ID: "33333333-3333-3333-3333-333333333333", Balance: 0}

	transfer := &domain.Transfer{
		ID:             "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "idem-123",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         100,
		Status:         domain.TransferStatusPending,
	} // Using walletB's ID for ToWalletID
	if err := transferRepo.Create(ctx, transfer); err != nil {
		t.Fatalf("transfer create failed: %v", err)
	}

	found, err := transferRepo.GetByIdempotencyKey(ctx, transfer.IdempotencyKey)
	if err != nil {
		t.Fatalf("transfer get by idempotency failed: %v", err)
	}
	if found.ID != transfer.ID {
		t.Fatalf("expected transfer id %s, got %s", transfer.ID, found.ID)
	}
}

func TestPostgresTransferRepository_UpdateStatus(t *testing.T) {
	_, _, transferRepo, _, _, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	walletA := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	walletB := &domain.Wallet{ID: "33333333-3333-3333-3333-333333333333", Balance: 0}
	transfer := &domain.Transfer{
		ID:             "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "idem-123",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         100,
		Status:         domain.TransferStatusPending,
	}
	_ = transferRepo.Create(ctx, transfer) // Pre-create transfer for status update test

	if err := transferRepo.UpdateStatus(ctx, transfer.ID, domain.TransferStatusProcessed, ""); err != nil {
		t.Fatalf("transfer update status failed: %v", err)
	}
}

func TestPostgresLedgerRepository_CreateAndGet(t *testing.T) {
	_, _, transferRepo, ledgerRepo, _, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	walletA := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	walletB := &domain.Wallet{ID: "33333333-3333-3333-3333-333333333333", Balance: 0}
	transfer := &domain.Transfer{
		ID:             "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "idem-123",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         100,
		Status:         domain.TransferStatusPending,
	}
	_ = transferRepo.Create(ctx, transfer) // Pre-create transfer for ledger entries

	entries := []domain.LedgerEntry{
		{ID: "44444444-4444-4444-4444-444444444444", TransferID: transfer.ID, WalletID: walletA.ID, EntryType: domain.LedgerEntryTypeDebit, Amount: 100},
		{ID: "55555555-5555-5555-5555-555555555555", TransferID: transfer.ID, WalletID: walletB.ID, EntryType: domain.LedgerEntryTypeCredit, Amount: 100},
	}
	if err := ledgerRepo.CreateEntries(ctx, entries); err != nil {
		t.Fatalf("ledger create entries failed: %v", err)
	}

	loadedEntries, err := ledgerRepo.GetByTransferID(ctx, transfer.ID)
	if err != nil {
		t.Fatalf("ledger get by transfer failed: %v", err)
	}
	if len(loadedEntries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(loadedEntries))
	}
}

func TestPostgresIdempotencyRepository_CreateGetAndUpdate(t *testing.T) {
	_, _, _, _, idemRepo, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	transfer := &domain.Transfer{
		ID:             "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "idem-123",
		FromWalletID:   "11111111-1111-1111-1111-111111111111",
		ToWalletID:     "33333333-3333-3333-3333-333333333333",
		Amount:         100,
		Status:         domain.TransferStatusPending,
	}
	// Note: transferRepo.Create is not called here, as idempotency record can exist independently for a short period.
	// In a real scenario, the transfer would be created shortly after.

	record := &IdempotencyRecord{
		IdempotencyKey: "idem-record-1",
		TransferID:     transfer.ID,
		RequestHash:    "hash-1",
		Status:         IdempotencyStatusCompleted,
		ResponseJSON:   []byte(`{"status":"ok"}`),
		StatusCode:     200,
	}
	if err := idemRepo.Create(ctx, record); err != nil {
		t.Fatalf("idempotency create failed: %v", err)
	}

	loadedRecord, err := idemRepo.GetByKey(ctx, record.IdempotencyKey)
	if err != nil {
		t.Fatalf("idempotency get failed: %v", err)
	}
	if loadedRecord.TransferID != record.TransferID {
		t.Fatalf("expected transfer id %s, got %s", record.TransferID, loadedRecord.TransferID)
	}

	if err := idemRepo.UpdateResponse(ctx, record.IdempotencyKey, []byte(`{"status":"ok","retry":true}`), 202, IdempotencyStatusCompleted); err != nil {
		t.Fatalf("idempotency update failed: %v", err)
	}

	loadedRecord, err = idemRepo.GetByKey(ctx, record.IdempotencyKey)
	if err != nil {
		t.Fatalf("idempotency get failed: %v", err)
	}
	if loadedRecord.StatusCode != 202 {
		t.Fatalf("expected status code 202, got %d", loadedRecord.StatusCode)
	}
}
