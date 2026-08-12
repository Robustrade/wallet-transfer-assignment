package integration

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/repository"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/service"
)

func testDatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable"
}

func setupDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDatabaseURL())
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	return pool
}

func createWallet(t *testing.T, pool *pgxpool.Pool, balance int64) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`,
		id, balance,
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func walletBalance(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) int64 {
	t.Helper()

	var balance int64
	err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, id,
	).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}
	return balance
}

func ledgerCount(t *testing.T, pool *pgxpool.Pool, transferID uuid.UUID) int {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`,
		transferID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func newService(pool *pgxpool.Pool) *service.TransferService {
	repos := repository.NewRepositories()
	return service.NewTransferService(pool, repos)
}

func TestSuccessfulTransfer(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	ctx := context.Background()
	from := createWallet(t, pool, 10000)
	to := createWallet(t, pool, 5000)

	svc := newService(pool)

	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "success-" + uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         2500,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Transfer.Status != "PROCESSED" {
		t.Fatalf("expected PROCESSED, got %s", result.Transfer.Status)
	}

	if got := walletBalance(t, pool, from); got != 7500 {
		t.Fatalf("source balance: expected 7500, got %d", got)
	}
	if got := walletBalance(t, pool, to); got != 7500 {
		t.Fatalf("destination balance: expected 7500, got %d", got)
	}
	if got := ledgerCount(t, pool, result.Transfer.ID); got != 2 {
		t.Fatalf("ledger entries: expected 2, got %d", got)
	}
}

func TestIdempotentRetryReturnsOriginalTransfer(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	ctx := context.Background()
	from := createWallet(t, pool, 10000)
	to := createWallet(t, pool, 0)
	key := "retry-" + uuid.NewString()

	svc := newService(pool)

	first, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.Transfer.ID != second.Transfer.ID {
		t.Fatalf("expected same transfer ID, got %s and %s",
			first.Transfer.ID, second.Transfer.ID)
	}

	if got := walletBalance(t, pool, from); got != 6000 {
		t.Fatalf("source was charged more than once: %d", got)
	}
	if got := walletBalance(t, pool, to); got != 4000 {
		t.Fatalf("destination was credited incorrectly: %d", got)
	}
	if got := ledgerCount(t, pool, first.Transfer.ID); got != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", got)
	}
}

func TestSameIdempotencyKeyDifferentPayloadConflicts(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	ctx := context.Background()
	from := createWallet(t, pool, 10000)
	to := createWallet(t, pool, 0)
	key := "conflict-" + uuid.NewString()

	svc := newService(pool)

	_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 2000,
	})
	if err == nil {
		t.Fatal("expected idempotency conflict")
	}
}

func TestInsufficientFundsDoesNotModifyBalances(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	ctx := context.Background()
	from := createWallet(t, pool, 100)
	to := createWallet(t, pool, 0)

	svc := newService(pool)

	_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "funds-" + uuid.NewString(),
		FromWalletID:   from, ToWalletID: to, Amount: 101,
	})
	if err == nil {
		t.Fatal("expected insufficient funds")
	}

	if got := walletBalance(t, pool, from); got != 100 {
		t.Fatalf("source balance changed: %d", got)
	}
	if got := walletBalance(t, pool, to); got != 0 {
		t.Fatalf("destination balance changed: %d", got)
	}
}

func TestConcurrentSpendingOnlyOneTransferSucceeds(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	from := createWallet(t, pool, 10000)
	to1 := createWallet(t, pool, 0)
	to2 := createWallet(t, pool, 0)
	svc := newService(pool)

	type result struct {
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		_, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{
			IdempotencyKey: "concurrent-1-" + uuid.NewString(),
			FromWalletID:   from, ToWalletID: to1, Amount: 10000,
		})
		results <- result{err: err}
	}()

	go func() {
		defer wg.Done()
		<-start
		_, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{
			IdempotencyKey: "concurrent-2-" + uuid.NewString(),
			FromWalletID:   from, ToWalletID: to2, Amount: 10000,
		})
		results <- result{err: err}
	}()

	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for r := range results {
		if r.err == nil {
			successes++
		} else {
			failures++
		}
	}

	if successes != 1 || failures != 1 {
		t.Fatalf("expected one success and one failure, got successes=%d failures=%d",
			successes, failures)
	}

	if got := walletBalance(t, pool, from); got != 0 {
		t.Fatalf("source balance: expected 0, got %d", got)
	}

	totalDestination := walletBalance(t, pool, to1) + walletBalance(t, pool, to2)
	if totalDestination != 10000 {
		t.Fatalf("destination total: expected 10000, got %d", totalDestination)
	}
}

func TestConcurrentSameIdempotencyKeyCreatesOneTransfer(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	from := createWallet(t, pool, 10000)
	to := createWallet(t, pool, 0)
	svc := newService(pool)
	key := "same-key-" + uuid.NewString()

	const workers = 10
	results := make(chan *service.TransferResult, workers)
	errorsCh := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{
				IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 3000,
			})
			results <- &result
			errorsCh <- err
		}()
	}

	close(start)
	wg.Wait()
	close(results)
	close(errorsCh)

	var transferID uuid.UUID
	for i := 0; i < workers; i++ {
		err := <-errorsCh
		if err != nil {
			t.Fatalf("concurrent idempotent request failed: %v", err)
		}
		result := <-results
		if transferID == uuid.Nil {
			transferID = result.Transfer.ID
		} else if result.Transfer.ID != transferID {
			t.Fatalf("different transfer IDs returned: %s vs %s",
				transferID, result.Transfer.ID)
		}
	}

	if got := walletBalance(t, pool, from); got != 7000 {
		t.Fatalf("source balance: expected 7000, got %d", got)
	}
	if got := walletBalance(t, pool, to); got != 3000 {
		t.Fatalf("destination balance: expected 3000, got %d", got)
	}
	if got := ledgerCount(t, pool, transferID); got != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", got)
	}
}

func TestLedgerBalancesForTransfer(t *testing.T) {
	pool := setupDB(t)
	defer pool.Close()

	ctx := context.Background()
	from := createWallet(t, pool, 10000)
	to := createWallet(t, pool, 10000)

	svc := newService(pool)
	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "ledger-" + uuid.NewString(),
		FromWalletID:   from, ToWalletID: to, Amount: 3500,
	})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `
		SELECT type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY type
	`, result.Transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var debit, credit int
	for rows.Next() {
		var typ string
		var amount int64
		if err := rows.Scan(&typ, &amount); err != nil {
			t.Fatal(err)
		}
		switch typ {
		case "CREDIT":
			credit++
			if amount != 3500 {
				t.Fatalf("credit amount: expected 3500, got %d", amount)
			}
		case "DEBIT":
			debit++
			if amount != 3500 {
				t.Fatalf("debit amount: expected 3500, got %d", amount)
			}
		}
	}
	if err := rows.Err(); err != nil && err != pgx.ErrNoRows {
		t.Fatal(err)
	}

	if debit != 1 || credit != 1 {
		t.Fatalf("expected one debit and one credit, got debit=%d credit=%d", debit, credit)
	}
}
