package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	migrations "github.com/Robustrade/wallet-transfer-assignment/migrations"
	"log/slog"
	"os"
)

// newIntegrationDB starts a PostgreSQL container, runs migrations, and returns
// the connection pool. It is skipped under -short.
func newIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("wallet_concurrency_test"),
		postgres.WithUsername("wallet"),
		postgres.WithPassword("wallet"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	if err := db.Migrate(dsn, migrations.FS, "."); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func newRealService(pool *pgxpool.Pool) *service.TransferService {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	return service.NewTransferService(
		pool,
		repository.NewWalletRepo(log),
		repository.NewTransferRepo(log),
		repository.NewLedgerRepo(log),
		repository.NewIdempotencyRepo(log),
		log,
	)
}

// seedWallet inserts a wallet with the given starting balance via raw SQL.
func seedWallet(t *testing.T, pool *pgxpool.Pool, w domain.Wallet) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance, version, created_at, updated_at)
		 VALUES ($1, $2, 0, NOW(), NOW())`,
		w.ID, int64(w.Balance),
	)
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
}

// TestConcurrentDebitsFromSameWallet fires N goroutines all trying to debit
// the same source wallet simultaneously. It asserts:
//   - No wallet ends up with a negative balance.
//   - The total number of PROCESSED transfers matches the number of successful
//     service calls.
//   - Final balances are consistent: fromBalance + toBalance == initial total.
func TestConcurrentDebitsFromSameWallet(t *testing.T) {
	pool := newIntegrationDB(t)
	ctx := context.Background()
	svc := newRealService(pool)

	const (
		initialFromBalance = 500_00 // 500.00
		transferAmount     = 100_00 // 100.00 per goroutine
		goroutines         = 10     // 10 concurrent attempts
	)

	from := domain.NewWallet()
	from.Balance = initialFromBalance
	to := domain.NewWallet()

	seedWallet(t, pool, from)
	seedWallet(t, pool, to)

	var (
		mu        sync.Mutex
		successes int
		failures  int
	)

	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-debit-%d", n),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         transferAmount,
			}

			_, err := svc.Execute(ctx, req)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				successes++
			} else {
				failures++
			}
		}(i)
	}

	wg.Wait()

	// Verify final balances.
	walletRepo := repository.NewWalletRepo(slog.Default())
	fromFinal, err := walletRepo.GetByID(ctx, pool, from.ID)
	if err != nil {
		t.Fatalf("get from wallet: %v", err)
	}

	toFinal, err := walletRepo.GetByID(ctx, pool, to.ID)
	if err != nil {
		t.Fatalf("get to wallet: %v", err)
	}

	total := fromFinal.Balance + toFinal.Balance
	if total != initialFromBalance {
		t.Fatalf("balance conservation violated: from=%d to=%d total=%d want=%d",
			fromFinal.Balance, toFinal.Balance, total, initialFromBalance)
	}

	if fromFinal.Balance < 0 {
		t.Fatalf("from wallet went negative: %d", fromFinal.Balance)
	}

	expectedSuccesses := int(initialFromBalance / transferAmount)
	if successes != expectedSuccesses {
		t.Fatalf("expected %d successes (max possible debits), got %d successes %d failures",
			expectedSuccesses, successes, failures)
	}
}

// TestIdempotentConcurrentRequests fires N goroutines all using the same
// idempotency key simultaneously. Only one transfer should execute; all
// goroutines should receive a successful response.
func TestIdempotentConcurrentRequests(t *testing.T) {
	pool := newIntegrationDB(t)
	ctx := context.Background()
	svc := newRealService(pool)

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	seedWallet(t, pool, from)
	seedWallet(t, pool, to)

	const goroutines = 10
	const sharedKey = "idempotent-concurrent-key"

	var (
		mu       sync.Mutex
		ids      []string
		errCount int
	)

	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			req := service.TransferRequest{
				IdempotencyKey: sharedKey,
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         100_00,
			}

			resp, err := svc.Execute(ctx, req)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errCount++
			} else {
				ids = append(ids, resp.TransferID.String())
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Fatalf("expected zero errors, got %d", errCount)
	}

	// All goroutines must have received the same transfer ID.
	first := ids[0]
	for _, id := range ids[1:] {
		if id != first {
			t.Fatalf("idempotency broken: got multiple transfer IDs: %v", ids)
		}
	}

	// Only one debit should have occurred.
	walletRepo := repository.NewWalletRepo(slog.Default())
	fromFinal, err := walletRepo.GetByID(ctx, pool, from.ID)
	if err != nil {
		t.Fatalf("get from wallet: %v", err)
	}

	if fromFinal.Balance != 500_00-100_00 {
		t.Fatalf("expected balance %d, got %d", 500_00-100_00, fromFinal.Balance)
	}
}
