package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type result struct {
	transfer *domain.Transfer
	err      error
}

type transferResult struct {
	transfer *domain.Transfer
	err      error
}

func testDatabaseURL() string {
	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}

	return "postgres://wallet:wallet@127.0.0.1:5433/wallet_transfer"
}

func setupTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDatabaseURL())
	if err != nil {
		t.Fatalf("create test database pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func seedWallets(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance) VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(1000),
		toID,
		int64(500),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
		 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
		 WHERE from_wallet_id = $1
		    OR to_wallet_id = $2`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
		 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	return fromID, toID
}

func newTestService(pool *pgxpool.Pool) *TransferService {
	return NewTransferService(
		pool,
		repository.NewPostgresWalletRepository(),
		repository.NewPostgresTransferRepository(),
		repository.NewPostgresLedgerRepository(),
	)
}

func TestCreateTransfer(t *testing.T) {
	pool := setupTestDatabase(t)
	fromID, toID := seedWallets(t, pool)

	service := newTestService(pool)

	result, err := service.CreateTransfer(
		context.Background(),
		CreateTransferRequest{
			IdempotencyKey: "test-success-" + uuid.NewString(),
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         100,
		},
	)
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	if result.Status != "PROCESSED" {
		t.Fatalf(
			"expected status PROCESSED, got %s",
			result.Status,
		)
	}

	var fromBalance int64
	var toBalance int64

	err = pool.QueryRow(
		context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`,
		fromID,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatalf("read source balance: %v", err)
	}

	err = pool.QueryRow(
		context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`,
		toID,
	).Scan(&toBalance)
	if err != nil {
		t.Fatalf("read destination balance: %v", err)
	}

	if fromBalance != 900 {
		t.Fatalf(
			"expected source balance 900, got %d",
			fromBalance,
		)
	}

	if toBalance != 600 {
		t.Fatalf(
			"expected destination balance 600, got %d",
			toBalance,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`,
		result.ID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"expected 2 ledger entries, got %d",
			ledgerCount,
		)
	}
}

func TestCreateTransfer_PopulatesTimestamps(t *testing.T) {
	pool := setupTestDatabase(t)
	fromID, toID := seedWallets(t, pool)

	service := newTestService(pool)

	result, err := service.CreateTransfer(
		context.Background(),
		CreateTransferRequest{
			IdempotencyKey: "test-timestamps-" + uuid.NewString(),
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         100,
		},
	)
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	if result == nil {
		t.Fatal("expected transfer result")
	}

	if result.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be populated")
	}

	if result.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be populated")
	}

	if result.CreatedAt.After(time.Now()) {
		t.Fatal("expected CreatedAt not to be in the future")
	}

	if result.UpdatedAt.Before(result.CreatedAt) {
		t.Fatal("expected UpdatedAt to be at or after CreatedAt")
	}
}
func TestCreateTransfer_WalletNotFound(t *testing.T) {
	pool := setupTestDatabase(t)
	_, toID := seedWallets(t, pool)

	service := newTestService(pool)

	missingWalletID := uuid.NewString()

	_, err := service.CreateTransfer(
		context.Background(),
		CreateTransferRequest{
			IdempotencyKey: "test-wallet-not-found-" + uuid.NewString(),
			FromWalletID:   missingWalletID,
			ToWalletID:     toID,
			Amount:         100,
		},
	)

	if !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf(
			"expected ErrWalletNotFound, got %v",
			err,
		)
	}
}
func TestCreateTransfer_InsufficientBalance(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance) VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(50),
		toID,
		int64(500),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE from_wallet_id = $1
			    OR to_wallet_id = $2`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	svc := newTestService(pool)

	_, err = svc.CreateTransfer(
		ctx,
		CreateTransferRequest{
			IdempotencyKey: "test-insufficient-" + uuid.NewString(),
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         100,
		},
	)

	if err == nil {
		t.Fatal("expected insufficient balance error")
	}

	var fromBalance int64
	var toBalance int64

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		fromID,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatalf("read source balance: %v", err)
	}

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		toID,
	).Scan(&toBalance)
	if err != nil {
		t.Fatalf("read destination balance: %v", err)
	}

	if fromBalance != 50 {
		t.Fatalf(
			"expected source balance 50, got %d",
			fromBalance,
		)
	}

	if toBalance != 500 {
		t.Fatalf(
			"expected destination balance 500, got %d",
			toBalance,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM ledger_entries
		 WHERE wallet_id IN ($1, $2)`,
		fromID,
		toID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 0 {
		t.Fatalf(
			"expected no ledger entries, got %d",
			ledgerCount,
		)
	}
}

func TestCreateTransfer_InsufficientBalance_PersistsFailedTransfer(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()
	idempotencyKey := "test-failed-" + uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance) VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(50),
		toID,
		int64(500),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE idempotency_key = $1`,
			idempotencyKey,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	svc := newTestService(pool)

	_, err = svc.CreateTransfer(
		ctx,
		CreateTransferRequest{
			IdempotencyKey: idempotencyKey,
			FromWalletID:   fromID,
			ToWalletID:     toID,
			Amount:         100,
		},
	)

	if err == nil {
		t.Fatal("expected insufficient balance error")
	}

	var status string

	err = pool.QueryRow(
		ctx,
		`SELECT status
		 FROM transfers
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(&status)
	if err != nil {
		t.Fatalf("read failed transfer: %v", err)
	}

	if status != "FAILED" {
		t.Fatalf(
			"expected transfer status FAILED, got %s",
			status,
		)
	}
}

func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID1 := uuid.NewString()
	toID2 := uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES ($1, $2), ($3, $4), ($5, $6)`,
		fromID,
		int64(100),
		toID1,
		int64(0),
		toID2,
		int64(0),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2, $3)`,
			fromID,
			toID1,
			toID2,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE from_wallet_id = $1
			    OR to_wallet_id IN ($2, $3)`,
			fromID,
			toID1,
			toID2,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2, $3)`,
			fromID,
			toID1,
			toID2,
		)
	})

	svc := newTestService(pool)

	results := make(chan transferResult, 2)

	start := make(chan struct{})

	go func() {
		<-start

		transfer, err := svc.CreateTransfer(
			context.Background(),
			CreateTransferRequest{
				IdempotencyKey: "concurrent-1-" + uuid.NewString(),
				FromWalletID:   fromID,
				ToWalletID:     toID1,
				Amount:         80,
			},
		)

		results <- transferResult{
			transfer: transfer,
			err:      err,
		}
	}()

	go func() {
		<-start

		transfer, err := svc.CreateTransfer(
			context.Background(),
			CreateTransferRequest{
				IdempotencyKey: "concurrent-2-" + uuid.NewString(),
				FromWalletID:   fromID,
				ToWalletID:     toID2,
				Amount:         80,
			},
		)

		results <- transferResult{
			transfer: transfer,
			err:      err,
		}
	}()

	close(start)

	var successCount int
	var failureCount int

	for i := 0; i < 2; i++ {
		res := <-results

		if res.err == nil {
			successCount++

			if res.transfer.Status != "PROCESSED" {
				t.Fatalf(
					"successful transfer has status %s",
					res.transfer.Status,
				)
			}
		} else {
			failureCount++

			if res.transfer == nil {
				t.Fatalf(
					"failed transfer should return transfer result",
				)
			}

			if res.transfer.Status != "FAILED" {
				t.Fatalf(
					"failed transfer has status %s",
					res.transfer.Status,
				)
			}
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"expected exactly 1 successful transfer, got %d",
			successCount,
		)
	}

	if failureCount != 1 {
		t.Fatalf(
			"expected exactly 1 failed transfer, got %d",
			failureCount,
		)
	}

	var fromBalance int64

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		fromID,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatalf("read source balance: %v", err)
	}

	if fromBalance != 20 {
		t.Fatalf(
			"expected source balance 20, got %d",
			fromBalance,
		)
	}

	var totalBalance int64

	err = pool.QueryRow(
		ctx,
		`SELECT COALESCE(SUM(balance), 0)
		 FROM wallets
		 WHERE id IN ($1, $2, $3)`,
		fromID,
		toID1,
		toID2,
	).Scan(&totalBalance)
	if err != nil {
		t.Fatalf("calculate total balance: %v", err)
	}

	if totalBalance != 100 {
		t.Fatalf(
			"expected total balance 100, got %d",
			totalBalance,
		)
	}

	var processedCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM transfers
		 WHERE from_wallet_id = $1
		   AND status = 'PROCESSED'`,
		fromID,
	).Scan(&processedCount)
	if err != nil {
		t.Fatalf("count processed transfers: %v", err)
	}

	if processedCount != 1 {
		t.Fatalf(
			"expected exactly 1 processed transfer, got %d",
			processedCount,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM ledger_entries
		 WHERE wallet_id IN ($1, $2, $3)`,
		fromID,
		toID1,
		toID2,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			ledgerCount,
		)
	}
}

func TestCreateTransfer_ConcurrentIdempotency(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()
	idempotencyKey := "concurrent-idempotency-" + uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(1000),
		toID,
		int64(0),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE idempotency_key = $1`,
			idempotencyKey,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	svc := newTestService(pool)

	const goroutineCount = 20

	start := make(chan struct{})
	results := make(chan transferResult, goroutineCount)

	for i := 0; i < goroutineCount; i++ {
		go func() {
			<-start

			transfer, err := svc.CreateTransfer(
				context.Background(),
				CreateTransferRequest{
					IdempotencyKey: idempotencyKey,
					FromWalletID:   fromID,
					ToWalletID:     toID,
					Amount:         100,
				},
			)

			results <- transferResult{
				transfer: transfer,
				err:      err,
			}
		}()
	}

	close(start)

	var successfulResults []*domain.Transfer

	for i := 0; i < goroutineCount; i++ {
		res := <-results

		if res.err != nil {
			t.Fatalf(
				"unexpected error from duplicate request: %v",
				res.err,
			)
		}

		if res.transfer == nil {
			t.Fatal("expected transfer result")
		}

		successfulResults = append(
			successfulResults,
			res.transfer,
		)
	}

	if len(successfulResults) != goroutineCount {
		t.Fatalf(
			"expected %d successful responses, got %d",
			goroutineCount,
			len(successfulResults),
		)
	}

	firstTransferID := successfulResults[0].ID

	for _, transfer := range successfulResults {
		if transfer.ID != firstTransferID {
			t.Fatalf(
				"expected same transfer ID %s, got %s",
				firstTransferID,
				transfer.ID,
			)
		}

		if transfer.Status != domain.TransferProcessed {
			t.Fatalf(
				"expected PROCESSED, got %s",
				transfer.Status,
			)
		}
	}

	var transferCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM transfers
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(&transferCount)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}

	if transferCount != 1 {
		t.Fatalf(
			"expected exactly 1 transfer, got %d",
			transferCount,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM ledger_entries
		 WHERE transfer_id = $1`,
		firstTransferID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			ledgerCount,
		)
	}

	var fromBalance int64
	var toBalance int64

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		fromID,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatalf("read source balance: %v", err)
	}

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		toID,
	).Scan(&toBalance)
	if err != nil {
		t.Fatalf("read destination balance: %v", err)
	}

	if fromBalance != 900 {
		t.Fatalf(
			"expected source balance 900, got %d",
			fromBalance,
		)
	}

	if toBalance != 100 {
		t.Fatalf(
			"expected destination balance 100, got %d",
			toBalance,
		)
	}
}

func TestCreateTransfer_ConcurrentIdempotencyConflict(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()
	idempotencyKey := "concurrent-idempotency-conflict-" + uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(1000),
		toID,
		int64(0),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE idempotency_key = $1`,
			idempotencyKey,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	svc := newTestService(pool)

	start := make(chan struct{})
	results := make(chan transferResult, 2)

	// Request 1 uses amount 100.
	go func() {
		<-start

		transfer, err := svc.CreateTransfer(
			context.Background(),
			CreateTransferRequest{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         100,
			},
		)

		results <- transferResult{
			transfer: transfer,
			err:      err,
		}
	}()

	// Request 2 uses the same idempotency key but amount 200.
	go func() {
		<-start

		transfer, err := svc.CreateTransfer(
			context.Background(),
			CreateTransferRequest{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         200,
			},
		)

		results <- transferResult{
			transfer: transfer,
			err:      err,
		}
	}()

	close(start)

	var successCount int
	var conflictCount int

	for i := 0; i < 2; i++ {
		res := <-results

		if res.err == nil {
			successCount++

			if res.transfer == nil {
				t.Fatal("expected transfer result for successful request")
			}

			if res.transfer.Status != domain.TransferProcessed {
				t.Fatalf(
					"expected successful transfer to be PROCESSED, got %s",
					res.transfer.Status,
				)
			}

			continue
		}

		if errors.Is(res.err, ErrIdempotencyConflict) {
			conflictCount++
			continue
		}

		t.Fatalf("unexpected error: %v", res.err)
	}

	if successCount != 1 {
		t.Fatalf(
			"expected exactly one successful request, got %d",
			successCount,
		)
	}

	if conflictCount != 1 {
		t.Fatalf(
			"expected exactly one idempotency conflict, got %d",
			conflictCount,
		)
	}

	var transferCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM transfers
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(&transferCount)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}

	if transferCount != 1 {
		t.Fatalf(
			"expected exactly one transfer record, got %d",
			transferCount,
		)
	}
}
func TestCreateTransfer_FailedTransferIsIdempotent(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()
	idempotencyKey := "failed-idempotency-" + uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(50),
		toID,
		int64(500),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE idempotency_key = $1`,
			idempotencyKey,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	svc := newTestService(pool)

	req := CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         100,
	}

	firstTransfer, firstErr := svc.CreateTransfer(
		ctx,
		req,
	)

	if !errors.Is(firstErr, ErrInsufficientBalance) {
		t.Fatalf(
			"expected insufficient balance error, got %v",
			firstErr,
		)
	}

	if firstTransfer == nil {
		t.Fatal("expected failed transfer result")
	}

	if firstTransfer.Status != domain.TransferFailed {
		t.Fatalf(
			"expected FAILED status, got %s",
			firstTransfer.Status,
		)
	}

	secondTransfer, secondErr := svc.CreateTransfer(
		ctx,
		req,
	)

	if !errors.Is(secondErr, ErrInsufficientBalance) {
		t.Fatalf(
			"expected same insufficient balance error on retry, got %v",
			secondErr,
		)
	}

	if secondTransfer == nil {
		t.Fatal("expected failed transfer result on retry")
	}

	if secondTransfer.ID != firstTransfer.ID {
		t.Fatalf(
			"expected same transfer ID %s, got %s",
			firstTransfer.ID,
			secondTransfer.ID,
		)
	}

	if secondTransfer.Status != domain.TransferFailed {
		t.Fatalf(
			"expected FAILED status on retry, got %s",
			secondTransfer.Status,
		)
	}

	var fromBalance int64

	err = pool.QueryRow(
		ctx,
		`SELECT balance
		 FROM wallets
		 WHERE id = $1`,
		fromID,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatalf("read source balance: %v", err)
	}

	if fromBalance != 50 {
		t.Fatalf(
			"expected source balance to remain 50, got %d",
			fromBalance,
		)
	}

	var transferCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM transfers
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(&transferCount)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}

	if transferCount != 1 {
		t.Fatalf(
			"expected exactly 1 transfer, got %d",
			transferCount,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM ledger_entries
		 WHERE transfer_id = $1`,
		firstTransfer.ID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 0 {
		t.Fatalf(
			"expected no ledger entries for failed transfer, got %d",
			ledgerCount,
		)
	}
}

func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	fromID := uuid.NewString()
	toID := uuid.NewString()
	idempotencyKey := "conflict-" + uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES ($1, $2), ($3, $4)`,
		fromID,
		int64(1000),
		toID,
		int64(0),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			fromID,
			toID,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE idempotency_key = $1`,
			idempotencyKey,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			fromID,
			toID,
		)
	})

	svc := newTestService(pool)

	firstRequest := CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         100,
	}

	firstTransfer, err := svc.CreateTransfer(
		ctx,
		firstRequest,
	)
	if err != nil {
		t.Fatalf(
			"first transfer should succeed: %v",
			err,
		)
	}

	if firstTransfer == nil {
		t.Fatal("expected first transfer result")
	}

	if firstTransfer.Status != domain.TransferProcessed {
		t.Fatalf(
			"expected first transfer to be PROCESSED, got %s",
			firstTransfer.Status,
		)
	}

	// Same idempotency key, but different amount.
	secondRequest := CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         200,
	}

	secondTransfer, err := svc.CreateTransfer(
		ctx,
		secondRequest,
	)

	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf(
			"expected ErrIdempotencyConflict, got %v",
			err,
		)
	}

	if secondTransfer != nil {
		t.Fatalf(
			"expected no transfer result for conflict, got %+v",
			secondTransfer,
		)
	}

	var transferCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM transfers
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	).Scan(&transferCount)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}

	if transferCount != 1 {
		t.Fatalf(
			"expected exactly 1 transfer, got %d",
			transferCount,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM ledger_entries
		 WHERE transfer_id = $1`,
		firstTransfer.ID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			ledgerCount,
		)
	}

	var fromBalance int64
	var toBalance int64

	err = pool.QueryRow(
		ctx,
		`SELECT balance
		 FROM wallets
		 WHERE id = $1`,
		fromID,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatalf("read source balance: %v", err)
	}

	err = pool.QueryRow(
		ctx,
		`SELECT balance
		 FROM wallets
		 WHERE id = $1`,
		toID,
	).Scan(&toBalance)
	if err != nil {
		t.Fatalf("read destination balance: %v", err)
	}

	if fromBalance != 900 {
		t.Fatalf(
			"expected source balance 900, got %d",
			fromBalance,
		)
	}

	if toBalance != 100 {
		t.Fatalf(
			"expected destination balance 100, got %d",
			toBalance,
		)
	}
}

func TestCreateTransfer_ConcurrentReverseTransfers(t *testing.T) {
	pool := setupTestDatabase(t)

	ctx := context.Background()

	walletA := uuid.NewString()
	walletB := uuid.NewString()

	_, err := pool.Exec(
		ctx,
		`INSERT INTO wallets (id, balance)
		 VALUES ($1, $2), ($3, $4)`,
		walletA,
		int64(1000),
		walletB,
		int64(1000),
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}

	keyAB := "reverse-ab-" + uuid.NewString()
	keyBA := "reverse-ba-" + uuid.NewString()

	t.Cleanup(func() {
		_, _ = pool.Exec(
			ctx,
			`DELETE FROM ledger_entries
			 WHERE wallet_id IN ($1, $2)`,
			walletA,
			walletB,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM transfers
			 WHERE idempotency_key IN ($1, $2)`,
			keyAB,
			keyBA,
		)

		_, _ = pool.Exec(
			ctx,
			`DELETE FROM wallets
			 WHERE id IN ($1, $2)`,
			walletA,
			walletB,
		)
	})

	svc := newTestService(pool)

	start := make(chan struct{})
	results := make(chan transferResult, 2)

	go func() {
		<-start

		transfer, err := svc.CreateTransfer(
			context.Background(),
			CreateTransferRequest{
				IdempotencyKey: keyAB,
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         100,
			},
		)

		results <- transferResult{
			transfer: transfer,
			err:      err,
		}
	}()

	go func() {
		<-start

		transfer, err := svc.CreateTransfer(
			context.Background(),
			CreateTransferRequest{
				IdempotencyKey: keyBA,
				FromWalletID:   walletB,
				ToWalletID:     walletA,
				Amount:         50,
			},
		)

		results <- transferResult{
			transfer: transfer,
			err:      err,
		}
	}()

	close(start)

	// If the transactions deadlock, this test must fail rather than
	// hanging indefinitely.
	timeout := time.After(5 * time.Second)

	var successCount int

	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf(
					"reverse transfer failed: %v",
					result.err,
				)
			}

			if result.transfer == nil {
				t.Fatal("expected transfer result")
			}

			if result.transfer.Status != domain.TransferProcessed {
				t.Fatalf(
					"expected PROCESSED, got %s",
					result.transfer.Status,
				)
			}

			successCount++

		case <-timeout:
			t.Fatal("concurrent reverse transfers timed out")
		}
	}

	if successCount != 2 {
		t.Fatalf(
			"expected 2 successful transfers, got %d",
			successCount,
		)
	}

	var balanceA int64
	var balanceB int64

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		walletA,
	).Scan(&balanceA)
	if err != nil {
		t.Fatalf("read wallet A balance: %v", err)
	}

	err = pool.QueryRow(
		ctx,
		`SELECT balance FROM wallets WHERE id = $1`,
		walletB,
	).Scan(&balanceB)
	if err != nil {
		t.Fatalf("read wallet B balance: %v", err)
	}

	// A: 1000 - 100 + 50 = 950
	// B: 1000 + 100 - 50 = 1050
	if balanceA != 950 {
		t.Fatalf(
			"expected wallet A balance 950, got %d",
			balanceA,
		)
	}

	if balanceB != 1050 {
		t.Fatalf(
			"expected wallet B balance 1050, got %d",
			balanceB,
		)
	}

	var ledgerCount int

	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM ledger_entries
		 WHERE wallet_id IN ($1, $2)`,
		walletA,
		walletB,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}

	if ledgerCount != 4 {
		t.Fatalf(
			"expected 4 ledger entries, got %d",
			ledgerCount,
		)
	}
}
