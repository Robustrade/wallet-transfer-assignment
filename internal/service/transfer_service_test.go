package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

type panicTransferRepository struct{}

func (p panicTransferRepository) BeginTx(ctx context.Context) (*repository.TransferTx, error) {
	panic("BeginTx should not be called for validation failures")
}

func (p panicTransferRepository) Get(ctx context.Context, transferID string) (*domain.Transfer, error) {
	panic("Get should not be called for validation failures")
}

func (p panicTransferRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	panic("GetTransferByIdempotencyKey should not be called for validation failures")
}

func (p panicTransferRepository) ListWalletLedgerEntries(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	panic("ListWalletLedgerEntries should not be called for validation failures")
}

func setupTransferIntegrationTest(t *testing.T) (context.Context, *pgxpool.Pool, *service.WalletService, *service.TransferService) {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run Postgres transfer integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}

	for _, migration := range []string{
		"001_init.sql",
		"002_create_transfers.sql",
		"003_create_ledger_and_idempotency.sql",
	} {
		sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("read migration %s: %v", migration, err)
		}

		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", migration, err)
		}
	}

	truncateTransferTables(t, ctx, pool)
	t.Cleanup(func() {
		truncateTransferTables(t, ctx, pool)
		pool.Close()
	})

	walletRepository := repository.NewWalletRepository(pool)
	walletService := service.NewWalletService(walletRepository)

	transferRepository := repository.NewTransferRepository(pool)
	transferService := service.NewTransferService(transferRepository)

	return ctx, pool, walletService, transferService
}

func truncateTransferTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		TRUNCATE idempotency_records, ledger_entries, transfers, wallets
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("truncate transfer tables: %v", err)
	}
}

func TestCreateTransferValidatesRequestBeforeTransaction(t *testing.T) {
	transferService := service.NewTransferService(panicTransferRepository{})

	tests := []struct {
		name    string
		req     service.CreateTransferRequest
		wantErr error
	}{
		{
			name: "missing idempotency key",
			req: service.CreateTransferRequest{
				FromWalletId: "wallet_from",
				ToWalletId:   "wallet_to",
				Amount:       100,
			},
			wantErr: domain.ErrIdempotencyKeyRequired,
		},
		{
			name: "same wallet transfer",
			req: service.CreateTransferRequest{
				IdempotencyKey: "idem_same_wallet",
				FromWalletId:   "wallet_from",
				ToWalletId:     "wallet_from",
				Amount:         100,
			},
			wantErr: domain.ErrSameWalletTransfer,
		},
		{
			name: "invalid amount",
			req: service.CreateTransferRequest{
				IdempotencyKey: "idem_invalid_amount",
				FromWalletId:   "wallet_from",
				ToWalletId:     "wallet_to",
				Amount:         0,
			},
			wantErr: domain.ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := transferService.CreateTransfer(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCreateTransferProcessesTransferAndWritesLedger(t *testing.T) {
	ctx, pool, walletService, transferService := setupTransferIntegrationTest(t)

	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_from", InitialBalance: 1000}); err != nil {
		t.Fatalf("create source wallet: %v", err)
	}
	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_to", InitialBalance: 500}); err != nil {
		t.Fatalf("create destination wallet: %v", err)
	}

	transfer, alreadyExists, err := transferService.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "idem_success",
		FromWalletId:   "wallet_from",
		ToWalletId:     "wallet_to",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if alreadyExists {
		t.Fatal("expected new transfer, got idempotent replay")
	}
	if transfer.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected processed transfer, got %q", transfer.Status)
	}

	fromWallet, err := walletService.GetWallet(ctx, "wallet_from")
	if err != nil {
		t.Fatalf("get source wallet: %v", err)
	}
	if fromWallet.Balance != 900 {
		t.Fatalf("expected source balance 900, got %d", fromWallet.Balance)
	}

	toWallet, err := walletService.GetWallet(ctx, "wallet_to")
	if err != nil {
		t.Fatalf("get destination wallet: %v", err)
	}
	if toWallet.Balance != 600 {
		t.Fatalf("expected destination balance 600, got %d", toWallet.Balance)
	}

	assertLedgerBalanced(t, ctx, pool, transfer.ID, 2, 100, 100)
}

func TestCreateTransferReplaysIdempotentRequestWithoutDuplicateLedger(t *testing.T) {
	ctx, pool, walletService, transferService := setupTransferIntegrationTest(t)

	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_from", InitialBalance: 1000}); err != nil {
		t.Fatalf("create source wallet: %v", err)
	}
	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_to", InitialBalance: 500}); err != nil {
		t.Fatalf("create destination wallet: %v", err)
	}

	req := service.CreateTransferRequest{
		IdempotencyKey: "idem_replay",
		FromWalletId:   "wallet_from",
		ToWalletId:     "wallet_to",
		Amount:         100,
	}

	firstTransfer, alreadyExists, err := transferService.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("create first transfer: %v", err)
	}
	if alreadyExists {
		t.Fatal("expected first request to create transfer")
	}

	secondTransfer, alreadyExists, err := transferService.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("replay transfer: %v", err)
	}
	if !alreadyExists {
		t.Fatal("expected second request to be idempotent replay")
	}
	if secondTransfer.ID != firstTransfer.ID {
		t.Fatalf("expected same transfer id %q, got %q", firstTransfer.ID, secondTransfer.ID)
	}

	fromWallet, err := walletService.GetWallet(ctx, "wallet_from")
	if err != nil {
		t.Fatalf("get source wallet: %v", err)
	}
	if fromWallet.Balance != 900 {
		t.Fatalf("expected source balance 900 after replay, got %d", fromWallet.Balance)
	}

	toWallet, err := walletService.GetWallet(ctx, "wallet_to")
	if err != nil {
		t.Fatalf("get destination wallet: %v", err)
	}
	if toWallet.Balance != 600 {
		t.Fatalf("expected destination balance 600 after replay, got %d", toWallet.Balance)
	}

	assertLedgerBalanced(t, ctx, pool, firstTransfer.ID, 2, 100, 100)
}

func TestCreateTransferRejectsInsufficientFundsWithoutLedger(t *testing.T) {
	ctx, pool, walletService, transferService := setupTransferIntegrationTest(t)

	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_from", InitialBalance: 50}); err != nil {
		t.Fatalf("create source wallet: %v", err)
	}
	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_to", InitialBalance: 500}); err != nil {
		t.Fatalf("create destination wallet: %v", err)
	}

	_, _, err := transferService.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "idem_insufficient",
		FromWalletId:   "wallet_from",
		ToWalletId:     "wallet_to",
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds error, got %v", err)
	}

	fromWallet, err := walletService.GetWallet(ctx, "wallet_from")
	if err != nil {
		t.Fatalf("get source wallet: %v", err)
	}
	if fromWallet.Balance != 50 {
		t.Fatalf("expected source balance unchanged at 50, got %d", fromWallet.Balance)
	}

	toWallet, err := walletService.GetWallet(ctx, "wallet_to")
	if err != nil {
		t.Fatalf("get destination wallet: %v", err)
	}
	if toWallet.Balance != 500 {
		t.Fatalf("expected destination balance unchanged at 500, got %d", toWallet.Balance)
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if ledgerCount != 0 {
		t.Fatalf("expected no ledger entries, got %d", ledgerCount)
	}
}

func TestCreateTransferConcurrentDebitsDoNotOverspend(t *testing.T) {
	ctx, pool, walletService, transferService := setupTransferIntegrationTest(t)

	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_from", InitialBalance: 100}); err != nil {
		t.Fatalf("create source wallet: %v", err)
	}
	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_to_1", InitialBalance: 0}); err != nil {
		t.Fatalf("create first destination wallet: %v", err)
	}
	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_to_2", InitialBalance: 0}); err != nil {
		t.Fatalf("create second destination wallet: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	transfers := make([]*domain.Transfer, 2)
	requests := []service.CreateTransferRequest{
		{
			IdempotencyKey: "idem_concurrent_1",
			FromWalletId:   "wallet_from",
			ToWalletId:     "wallet_to_1",
			Amount:         80,
		},
		{
			IdempotencyKey: "idem_concurrent_2",
			FromWalletId:   "wallet_from",
			ToWalletId:     "wallet_to_2",
			Amount:         80,
		},
	}

	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			transfer, _, err := transferService.CreateTransfer(ctx, requests[i])
			transfers[i] = transfer
			errs[i] = err
		}(i)
	}
	wg.Wait()

	successCount := 0
	insufficientFundsCount := 0
	var successfulTransferID domain.TransferID
	for i, err := range errs {
		switch {
		case err == nil:
			successCount++
			successfulTransferID = transfers[i].ID
		case errors.Is(err, domain.ErrInsufficientFunds):
			insufficientFundsCount++
		default:
			t.Fatalf("unexpected error from concurrent transfer %d: %v", i, err)
		}
	}

	if successCount != 1 {
		t.Fatalf("expected exactly one successful transfer, got %d", successCount)
	}
	if insufficientFundsCount != 1 {
		t.Fatalf("expected exactly one insufficient funds error, got %d", insufficientFundsCount)
	}

	fromWallet, err := walletService.GetWallet(ctx, "wallet_from")
	if err != nil {
		t.Fatalf("get source wallet: %v", err)
	}
	if fromWallet.Balance != 20 {
		t.Fatalf("expected source balance 20, got %d", fromWallet.Balance)
	}

	var transferCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers WHERE status = 'PROCESSED'`).Scan(&transferCount); err != nil {
		t.Fatalf("count processed transfers: %v", err)
	}
	if transferCount != 1 {
		t.Fatalf("expected one processed transfer, got %d", transferCount)
	}

	assertLedgerBalanced(t, ctx, pool, successfulTransferID, 2, 80, 80)
}

func TestCreateTransferConcurrentSameIdempotencyKeyReturnsSingleTransfer(t *testing.T) {
	ctx, pool, walletService, transferService := setupTransferIntegrationTest(t)

	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_from", InitialBalance: 1000}); err != nil {
		t.Fatalf("create source wallet: %v", err)
	}
	if _, err := walletService.CreateWallet(ctx, service.CreateWalletRequest{WalletID: "wallet_to", InitialBalance: 0}); err != nil {
		t.Fatalf("create destination wallet: %v", err)
	}

	req := service.CreateTransferRequest{
		IdempotencyKey: "idem_concurrent_same_key",
		FromWalletId:   "wallet_from",
		ToWalletId:     "wallet_to",
		Amount:         100,
	}

	const requestCount = 2
	var wg sync.WaitGroup
	errs := make([]error, requestCount)
	alreadyExistsValues := make([]bool, requestCount)
	transfers := make([]*domain.Transfer, requestCount)

	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			transfer, alreadyExists, err := transferService.CreateTransfer(ctx, req)
			transfers[i] = transfer
			alreadyExistsValues[i] = alreadyExists
			errs[i] = err
		}(i)
	}
	wg.Wait()

	var transferID domain.TransferID
	replayCount := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if transfers[i] == nil {
			t.Fatalf("request %d returned nil transfer", i)
		}
		if transferID == "" {
			transferID = transfers[i].ID
		}
		if transfers[i].ID != transferID {
			t.Fatalf("expected same transfer id %q, got %q", transferID, transfers[i].ID)
		}
		if alreadyExistsValues[i] {
			replayCount++
		}
	}

	if replayCount != requestCount-1 {
		t.Fatalf("expected %d idempotent replay, got %d", requestCount-1, replayCount)
	}

	fromWallet, err := walletService.GetWallet(ctx, "wallet_from")
	if err != nil {
		t.Fatalf("get source wallet: %v", err)
	}
	if fromWallet.Balance != 900 {
		t.Fatalf("expected source balance 900, got %d", fromWallet.Balance)
	}

	toWallet, err := walletService.GetWallet(ctx, "wallet_to")
	if err != nil {
		t.Fatalf("get destination wallet: %v", err)
	}
	if toWallet.Balance != 100 {
		t.Fatalf("expected destination balance 100, got %d", toWallet.Balance)
	}

	var transferCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers`).Scan(&transferCount); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if transferCount != 1 {
		t.Fatalf("expected one transfer row, got %d", transferCount)
	}

	assertLedgerBalanced(t, ctx, pool, transferID, 2, 100, 100)
}

func assertLedgerBalanced(t *testing.T, ctx context.Context, pool *pgxpool.Pool, transferID domain.TransferID, wantCount int, wantDebit int64, wantCredit int64) {
	t.Helper()

	var count int
	var debitTotal int64
	var creditTotal int64
	err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN type = 'DEBIT' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE 0 END), 0)
		FROM ledger_entries
		WHERE transfer_id = $1
	`, transferID).Scan(&count, &debitTotal, &creditTotal)
	if err != nil {
		t.Fatalf("query ledger totals: %v", err)
	}

	if count != wantCount {
		t.Fatalf("expected %d ledger entries, got %d", wantCount, count)
	}
	if debitTotal != wantDebit {
		t.Fatalf("expected debit total %d, got %d", wantDebit, debitTotal)
	}
	if creditTotal != wantCredit {
		t.Fatalf("expected credit total %d, got %d", wantCredit, creditTotal)
	}
}
