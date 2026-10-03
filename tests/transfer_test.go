package tests

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/malem-mayeng/wallet-transfer-assignment/internal/domain"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/repository"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/service"
)

func TestCreateTransfer_Success(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	result, err := transferService.CreateTransfer(
		ctx,
		service.CreateTransferRequest{
			IdempotencyKey: "success-1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         100,
		},
	)

	if err != nil {
		t.Fatalf("CreateTransfer() error = %v", err)
	}

	if result.Status != "PROCESSED" {
		t.Fatalf(
			"status = %q, want %q",
			result.Status,
			"PROCESSED",
		)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 2 {
		t.Fatalf("ledger entries = %d, want 2", ledgerCount)
	}
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	req := service.CreateTransferRequest{
		IdempotencyKey: "idempotency-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := transferService.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first CreateTransfer() error = %v", err)
	}

	second, err := transferService.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("second CreateTransfer() error = %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf(
			"transfer IDs differ: first=%d second=%d",
			first.ID,
			second.ID,
		)
	}

	if second.Status != "PROCESSED" {
		t.Fatalf(
			"second status = %q, want %q",
			second.Status,
			"PROCESSED",
		)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 1 {
		t.Fatalf("transfers = %d, want 1", transferCount)
	}

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 2 {
		t.Fatalf("ledger entries = %d, want 2", ledgerCount)
	}
}

func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	firstRequest := service.CreateTransferRequest{
		IdempotencyKey: "conflict-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	_, err := transferService.CreateTransfer(ctx, firstRequest)
	if err != nil {
		t.Fatalf("first CreateTransfer() error = %v", err)
	}

	conflictingRequest := service.CreateTransferRequest{
		IdempotencyKey: "conflict-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	}

	_, err = transferService.CreateTransfer(ctx, conflictingRequest)

	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf(
			"error = %v, want %v",
			err,
			domain.ErrIdempotencyConflict,
		)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 1 {
		t.Fatalf("transfers = %d, want 1", transferCount)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	result, err := transferService.CreateTransfer(
		ctx,
		service.CreateTransferRequest{
			IdempotencyKey: "insufficient-1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         2000,
		},
	)

	if err != nil {
		t.Fatalf("CreateTransfer() error = %v", err)
	}

	if result.Status != domain.TransferFailed {
		t.Fatalf(
			"status = %q, want %q",
			result.Status,
			domain.TransferFailed,
		)
	}

	// Money must not move.
	requireBalance(t, pool, "wallet_1", 1000)
	requireBalance(t, pool, "wallet_2", 500)

	// A failed transfer must not create financial ledger entries.
	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 0 {
		t.Fatalf("ledger entries = %d, want 0", ledgerCount)
	}
}

func TestCreateTransfer_FailedTransferIsIdempotent(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	req := service.CreateTransferRequest{
		IdempotencyKey: "failed-idempotency-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         2000,
	}

	first, err := transferService.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first CreateTransfer() error = %v", err)
	}

	second, err := transferService.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("second CreateTransfer() error = %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf(
			"transfer IDs differ: first=%d second=%d",
			first.ID,
			second.ID,
		)
	}

	if first.Status != domain.TransferFailed ||
		second.Status != domain.TransferFailed {
		t.Fatalf(
			"statuses = %q, %q; want both %q",
			first.Status,
			second.Status,
			domain.TransferFailed,
		)
	}

	requireBalance(t, pool, "wallet_1", 1000)
	requireBalance(t, pool, "wallet_2", 500)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 1 {
		t.Fatalf("transfers = %d, want 1", transferCount)
	}

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 0 {
		t.Fatalf("ledger entries = %d, want 0", ledgerCount)
	}
}

func TestCreateTransfer_ConcurrentTransfers(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	const (
		numberOfRequests = 30
		amount           = int64(50)
	)

	results := make(chan *domain.Transfer, numberOfRequests)
	errs := make(chan error, numberOfRequests)

	var wg sync.WaitGroup

	for i := 0; i < numberOfRequests; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			result, err := transferService.CreateTransfer(
				ctx,
				service.CreateTransferRequest{
					IdempotencyKey: "concurrent-" + strconv.Itoa(i),
					FromWalletID:   "wallet_1",
					ToWalletID:     "wallet_2",
					Amount:         amount,
				},
			)

			if err != nil {
				errs <- err
				return
			}

			results <- result
		}(i)
	}

	wg.Wait()

	close(results)
	close(errs)

	for err := range errs {
		t.Errorf("unexpected transfer error: %v", err)
	}

	processed := 0
	failed := 0

	for result := range results {
		switch result.Status {
		case domain.TransferProcessed:
			processed++
		case domain.TransferFailed:
			failed++
		default:
			t.Errorf("unexpected transfer status: %q", result.Status)
		}
	}

	if processed != 20 {
		t.Fatalf("processed transfers = %d, want 20", processed)
	}

	if failed != 10 {
		t.Fatalf("failed transfers = %d, want 10", failed)
	}

	requireBalance(t, pool, "wallet_1", 0)
	requireBalance(t, pool, "wallet_2", 1500)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 30 {
		t.Fatalf("transfers = %d, want 30", transferCount)
	}

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 40 {
		t.Fatalf("ledger entries = %d, want 40", ledgerCount)
	}
}

func TestCreateTransfer_OppositeDirections(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	const (
		transfersPerDirection = 50
		amount                = int64(10)
	)

	results := make(chan *domain.Transfer, transfersPerDirection*2)
	errs := make(chan error, transfersPerDirection*2)

	var wg sync.WaitGroup

	for i := 0; i < transfersPerDirection; i++ {
		wg.Add(2)

		go func(i int) {
			defer wg.Done()

			result, err := transferService.CreateTransfer(
				ctx,
				service.CreateTransferRequest{
					IdempotencyKey: "forward-" + strconv.Itoa(i),
					FromWalletID:   "wallet_1",
					ToWalletID:     "wallet_2",
					Amount:         amount,
				},
			)

			if err != nil {
				errs <- err
				return
			}

			results <- result
		}(i)

		go func(i int) {
			defer wg.Done()

			result, err := transferService.CreateTransfer(
				ctx,
				service.CreateTransferRequest{
					IdempotencyKey: "reverse-" + strconv.Itoa(i),
					FromWalletID:   "wallet_2",
					ToWalletID:     "wallet_1",
					Amount:         amount,
				},
			)

			if err != nil {
				errs <- err
				return
			}

			results <- result
		}(i)
	}

	wg.Wait()

	close(results)
	close(errs)

	for err := range errs {
		t.Errorf("unexpected transfer error: %v", err)
	}

	processed := 0

	for result := range results {
		if result.Status != domain.TransferProcessed {
			t.Errorf(
				"status = %q, want %q",
				result.Status,
				domain.TransferProcessed,
			)
			continue
		}

		processed++
	}

	expectedProcessed := transfersPerDirection * 2

	if processed != expectedProcessed {
		t.Fatalf(
			"processed transfers = %d, want %d",
			processed,
			expectedProcessed,
		)
	}

	// Equal amounts move in both directions, so the final
	// balances should be exactly what they were initially.
	requireBalance(t, pool, "wallet_1", 1000)
	requireBalance(t, pool, "wallet_2", 500)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != expectedProcessed {
		t.Fatalf(
			"transfers = %d, want %d",
			transferCount,
			expectedProcessed,
		)
	}

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	expectedLedgerEntries := expectedProcessed * 2

	if ledgerCount != expectedLedgerEntries {
		t.Fatalf(
			"ledger entries = %d, want %d",
			ledgerCount,
			expectedLedgerEntries,
		)
	}
}

func TestCreateTransfer_ConcurrentSameIdempotencyKey(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	const numberOfRequests = 20

	results := make(chan *domain.Transfer, numberOfRequests)
	errs := make(chan error, numberOfRequests)

	var wg sync.WaitGroup

	for i := 0; i < numberOfRequests; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			result, err := transferService.CreateTransfer(
				ctx,
				service.CreateTransferRequest{
					IdempotencyKey: "same-key",
					FromWalletID:   "wallet_1",
					ToWalletID:     "wallet_2",
					Amount:         100,
				},
			)

			if err != nil {
				errs <- err
				return
			}

			results <- result
		}()
	}

	wg.Wait()

	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}

	var transferID int64

	for result := range results {
		if transferID == 0 {
			transferID = result.ID
		}

		if result.ID != transferID {
			t.Fatalf(
				"multiple transfer IDs returned: got %d, want %d",
				result.ID,
				transferID,
			)
		}

		if result.Status != domain.TransferProcessed {
			t.Fatalf(
				"status = %q, want %q",
				result.Status,
				domain.TransferProcessed,
			)
		}
	}

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 1 {
		t.Fatalf("transfers = %d, want 1", transferCount)
	}

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 2 {
		t.Fatalf("ledger entries = %d, want 2", ledgerCount)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)
}

func TestCreateTransfer_LedgerIntegrity(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	result, err := transferService.CreateTransfer(
		ctx,
		service.CreateTransferRequest{
			IdempotencyKey: "ledger-integrity-1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         125,
		},
	)
	if err != nil {
		t.Fatalf("CreateTransfer() error = %v", err)
	}

	if result.Status != domain.TransferProcessed {
		t.Fatalf(
			"status = %q, want %q",
			result.Status,
			domain.TransferProcessed,
		)
	}

	var debitWallet string
	var debitType string
	var debitAmount int64

	err = pool.QueryRow(
		ctx,
		`
		SELECT wallet_id, entry_type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
		  AND entry_type = 'DEBIT'
		`,
		result.ID,
	).Scan(
		&debitWallet,
		&debitType,
		&debitAmount,
	)
	if err != nil {
		t.Fatalf("query debit entry: %v", err)
	}

	if debitWallet != "wallet_1" {
		t.Fatalf(
			"debit wallet = %q, want %q",
			debitWallet,
			"wallet_1",
		)
	}

	if debitType != string(domain.LedgerDebit) {
		t.Fatalf(
			"debit type = %q, want %q",
			debitType,
			domain.LedgerDebit,
		)
	}

	if debitAmount != 125 {
		t.Fatalf(
			"debit amount = %d, want 125",
			debitAmount,
		)
	}

	var creditWallet string
	var creditType string
	var creditAmount int64

	err = pool.QueryRow(
		ctx,
		`
		SELECT wallet_id, entry_type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
		  AND entry_type = 'CREDIT'
		`,
		result.ID,
	).Scan(
		&creditWallet,
		&creditType,
		&creditAmount,
	)
	if err != nil {
		t.Fatalf("query credit entry: %v", err)
	}

	if creditWallet != "wallet_2" {
		t.Fatalf(
			"credit wallet = %q, want %q",
			creditWallet,
			"wallet_2",
		)
	}

	if creditType != string(domain.LedgerCredit) {
		t.Fatalf(
			"credit type = %q, want %q",
			creditType,
			domain.LedgerCredit,
		)
	}

	if creditAmount != 125 {
		t.Fatalf(
			"credit amount = %d, want 125",
			creditAmount,
		)
	}

	if debitAmount != creditAmount {
		t.Fatalf(
			"debit amount = %d, credit amount = %d",
			debitAmount,
			creditAmount,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1",
		result.ID,
	).Scan(&ledgerCount)

	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"ledger entries = %d, want 2",
			ledgerCount,
		)
	}
}

func TestCreateTransfer_RollsBackOnDatabaseFailure(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)

	// wallet_1 has enough money to send 1.
	// wallet_2 is at the maximum BIGINT value, so adding 1
	// will overflow the Go int64 value and produce a negative
	// value. PostgreSQL's balance CHECK constraint will reject it.
	_, err := pool.Exec(
		ctx,
		`
		INSERT INTO wallets (id, balance)
		VALUES
			('wallet_1', 1000),
			('wallet_2', 9223372036854775807)
		`,
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)

	_, err = transferService.CreateTransfer(
		ctx,
		service.CreateTransferRequest{
			IdempotencyKey: "rollback-1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         1,
		},
	)

	if err == nil {
		t.Fatal("CreateTransfer() error = nil, want database error")
	}

	// The source wallet update must have been rolled back.
	requireBalance(t, pool, "wallet_1", 1000)

	// The destination wallet must also remain unchanged.
	requireBalance(t, pool, "wallet_2", 9223372036854775807)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 0 {
		t.Fatalf(
			"transfers = %d, want 0 after rollback",
			transferCount,
		)
	}

	ledgerCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM ledger_entries",
	)

	if ledgerCount != 0 {
		t.Fatalf(
			"ledger entries = %d, want 0 after rollback",
			ledgerCount,
		)
	}
}
