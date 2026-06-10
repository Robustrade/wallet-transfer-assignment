package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func connectTestDB(t *testing.T) (*pgxpool.Pool, func()) {
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

	schemaName := fmt.Sprintf("test_app_%d", time.Now().UnixNano())
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

	return pool, cleanup
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
    transfer_id UUID REFERENCES transfers(id),
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    response_json JSONB NOT NULL DEFAULT '{}',
    status_code INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`))
	return err
}

// setupIntegrationService initializes repositories and transaction manager from a test pool.
func setupIntegrationService(pool *pgxpool.Pool) (*TransferService, repository.WalletRepository, repository.TransferRepository, repository.LedgerRepository, repository.IdempotencyRepository, repository.TransactionManager) {
	walletRepo := repository.NewPostgresWalletRepository(pool)
	transferRepo := repository.NewPostgresTransferRepository(pool)
	ledgerRepo := repository.NewPostgresLedgerRepository(pool)
	idemRepo := repository.NewPostgresIdempotencyRepository(pool)
	manager := repository.NewPostgresTransactionManager(pool)
	service := NewTransferService(manager, walletRepo, transferRepo, ledgerRepo, idemRepo, nil)
	return service, walletRepo, transferRepo, ledgerRepo, idemRepo, manager
}

func createIntegrationWallets(t *testing.T, ctx context.Context, repo repository.WalletRepository, wallets ...*domain.Wallet) {
	t.Helper()
	for _, w := range wallets {
		if err := repo.Create(ctx, w); err != nil {
			t.Fatalf("failed to create wallet %s: %v", w.ID, err)
		}
	}
}

func TestTransferService_SelectForUpdateBlocksConcurrentTransfer(t *testing.T) {
	pool, cleanup := connectTestDB(t)
	defer cleanup()

	ctx := context.Background()
	service, walletRepo, _, _, _, manager := setupIntegrationService(pool)

	walletA := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	walletB := &domain.Wallet{ID: "22222222-2222-2222-2222-222222222222", Balance: 0}
	createIntegrationWallets(t, ctx, walletRepo, walletA, walletB)

	locked := make(chan struct{})
	releaseLock := make(chan struct{})
	txErrCh := make(chan error, 1)
	go func() {
		txErrCh <- manager.RunInTransaction(ctx, func(txCtx context.Context) error {
			lockedWallet, err := walletRepo.GetByIDForUpdate(txCtx, walletA.ID)
			if err != nil {
				return err
			}
			close(locked)
			<-releaseLock
			return walletRepo.Update(txCtx, lockedWallet)
		})
	}()

	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for lock transaction to acquire row lock")
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.Execute(ctx, TransferRequest{
			TransferID:     "33333333-3333-3333-3333-333333333333",
			IdempotencyKey: "idem-lock-1",
			FromWalletID:   walletA.ID,
			ToWalletID:     walletB.ID,
			Amount:         100,
			RequestHash:    "hash-lock-1",
		})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("transfer completed before lock released: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(releaseLock)
	if err := <-txErrCh; err != nil {
		t.Fatalf("lock transaction failed: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("transfer failed after lock release: %v", err)
	}

	updatedA, err := walletRepo.GetByID(ctx, walletA.ID)
	if err != nil {
		t.Fatalf("failed to load wallet A: %v", err)
	}
	updatedB, err := walletRepo.GetByID(ctx, walletB.ID)
	if err != nil {
		t.Fatalf("failed to load wallet B: %v", err)
	}
	if updatedA.Balance != 400 {
		t.Fatalf("expected wallet A balance 400, got %d", updatedA.Balance)
	}
	if updatedB.Balance != 100 {
		t.Fatalf("expected wallet B balance 100, got %d", updatedB.Balance)
	}
}

func TestTransferService_IdempotencyConcurrentRetries(t *testing.T) {
	pool, cleanup := connectTestDB(t)
	defer cleanup()

	ctx := context.Background()
	service, walletRepo, _, _, _, _ := setupIntegrationService(pool)

	walletA := &domain.Wallet{ID: "44444444-4444-4444-4444-444444444444", Balance: 500}
	walletB := &domain.Wallet{ID: "55555555-5555-5555-5555-555555555555", Balance: 0}
	createIntegrationWallets(t, ctx, walletRepo, walletA, walletB)

	req := TransferRequest{
		TransferID:     "66666666-6666-6666-6666-666666666666",
		IdempotencyKey: "idem-concurrent-1",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         100,
		RequestHash:    "hash-concurrent-1",
	}

	const concurrency = 5
	results, errs := runConcurrentServiceCalls(ctx, service, req, concurrency)

	verifyConcurrentResults(t, results, errs, req.TransferID, concurrency)
	assertFinalIntegrationBalances(t, ctx, pool, walletRepo, req, 400, 100)
}

func runConcurrentServiceCalls(ctx context.Context, service *TransferService, req TransferRequest, n int) ([]*domain.Transfer, []error) {
	var wg sync.WaitGroup
	wg.Add(n)
	resChan := make(chan *domain.Transfer, n)
	errChan := make(chan error, n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			transfer, err := service.Execute(ctx, req)
			if err != nil {
				errChan <- err
				return
			}
			resChan <- transfer
		}()
	}
	wg.Wait()
	close(resChan)
	close(errChan)

	var results []*domain.Transfer
	for r := range resChan {
		results = append(results, r)
	}
	var errs []error
	for e := range errChan {
		errs = append(errs, e)
	}
	return results, errs
}

func verifyConcurrentResults(t *testing.T, results []*domain.Transfer, errs []error, expectedID string, concurrency int) {
	t.Helper()
	for _, err := range errs {
		t.Fatalf("concurrent execution failed: %v", err)
	}

	ids := map[string]int{}
	for _, tr := range results {
		ids[tr.ID]++
	}
	if len(ids) != 1 || ids[expectedID] != concurrency {
		t.Fatalf("idempotency check failed: expected 1 unique ID with %d instances, got %v", concurrency, ids)
	}
}

func assertFinalIntegrationBalances(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo repository.WalletRepository, req TransferRequest, balA, balB int64) {
	t.Helper()
	var rowCount int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers WHERE idempotency_key = $1`, req.IdempotencyKey).Scan(&rowCount)
	if rowCount != 1 {
		t.Fatalf("expected 1 transfer row, got %d", rowCount)
	}

	a, _ := repo.GetByID(ctx, req.FromWalletID)
	b, _ := repo.GetByID(ctx, req.ToWalletID)
	if a.Balance != balA || b.Balance != balB {
		t.Fatalf("balance mismatch: expected A=%d B=%d, got A=%d B=%d", balA, balB, a.Balance, b.Balance)
	}
}

func TestTransferService_CreatesLedgerAndIdempotencyRecords(t *testing.T) {
	pool, cleanup := connectTestDB(t)
	defer cleanup()

	ctx := context.Background()
	service, walletRepo, _, _, _, _ := setupIntegrationService(pool)

	walletA := &domain.Wallet{ID: "77777777-7777-7777-7777-777777777777", Balance: 700}
	walletB := &domain.Wallet{ID: "88888888-8888-8888-8888-888888888888", Balance: 0}
	createIntegrationWallets(t, ctx, walletRepo, walletA, walletB)

	req := TransferRequest{
		TransferID:     "99999999-9999-9999-9999-999999999999",
		IdempotencyKey: "idem-ledger-1",
		FromWalletID:   walletA.ID,
		ToWalletID:     walletB.ID,
		Amount:         200,
		RequestHash:    "hash-ledger-1",
	}

	transfer, err := service.Execute(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if transfer.ID != req.TransferID {
		t.Fatalf("expected transfer id %s, got %s", req.TransferID, transfer.ID)
	}

	var ledgerCount, idempotencyCount int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, req.TransferID).Scan(&ledgerCount)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE idempotency_key = $1`, req.IdempotencyKey).Scan(&idempotencyCount)

	if ledgerCount != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", ledgerCount)
	}
	if idempotencyCount != 1 {
		t.Fatalf("expected 1 idempotency record, got %d", idempotencyCount)
	}

	assertFinalIntegrationBalances(t, ctx, pool, walletRepo, req, 500, 200)
}

func TestTransferService_ConcurrentDistinctDebits(t *testing.T) {
	pool, cleanup := connectTestDB(t)
	defer cleanup()

	ctx := context.Background()
	service, walletRepo, _, _, _, _ := setupIntegrationService(pool)

	walletA := &domain.Wallet{ID: "11111111-1111-1111-1111-111111111111", Balance: 500}
	walletB := &domain.Wallet{ID: "22222222-2222-2222-2222-222222222222", Balance: 0}
	createIntegrationWallets(t, ctx, walletRepo, walletA, walletB)

	const concurrency = 10
	const amount = 100

	var wg sync.WaitGroup
	results := make(chan *domain.Transfer, concurrency)
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := TransferRequest{
				IdempotencyKey: fmt.Sprintf("idem-dist-%d", i),
				FromWalletID:   walletA.ID,
				ToWalletID:     walletB.ID,
				Amount:         amount,
				RequestHash:    fmt.Sprintf("hash-dist-%d", i),
			}
			transfer, err := service.Execute(ctx, req)
			if err != nil {
				errs <- err
				return
			}
			results <- transfer
		}(i)
	}

	wg.Wait()
	close(results)
	close(errs)

	successCount := 0
	failCount := 0
	for err := range errs {
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			t.Fatalf("unexpected error: %v", err)
		}
		failCount++
	}
	for range results {
		successCount++
	}

	if successCount != 5 {
		t.Fatalf("expected 5 successful transfers, got %d", successCount)
	}
	if failCount != 5 {
		t.Fatalf("expected 5 failed transfers, got %d", failCount)
	}

	a, err := walletRepo.GetByID(ctx, walletA.ID)
	if err != nil {
		t.Fatalf("failed to load wallet A: %v", err)
	}
	b, err := walletRepo.GetByID(ctx, walletB.ID)
	if err != nil {
		t.Fatalf("failed to load wallet B: %v", err)
	}

	if a.Balance != 0 {
		t.Fatalf("expected source wallet balance 0, got %d", a.Balance)
	}
	if b.Balance != 500 {
		t.Fatalf("expected destination wallet balance 500, got %d", b.Balance)
	}

	var totalLedgerEntries int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&totalLedgerEntries); err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}
	if totalLedgerEntries != 10 {
		t.Fatalf("expected 10 ledger entries for 5 successful transfers, got %d", totalLedgerEntries)
	}
}
