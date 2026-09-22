//go:build integration

// Integration tests against a real PostgreSQL instance, verifying that the
// pgx-based repositories and row-level locking behave correctly under real
// concurrent transactions.
//
// By default, tests start a disposable PostgreSQL container with Testcontainers.
// Set TEST_DATABASE_URL to use an already-running PostgreSQL instance instead.
package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/migrations"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/repository/postgres"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		ctx := context.Background()
		container, err := postgres.Run(
			ctx,
			"postgres:16-alpine",
			postgres.WithDatabase("wallet_transfer"),
			postgres.WithUsername("wallet"),
			postgres.WithPassword("wallet"),
		)
		if err != nil {
			t.Fatalf("start PostgreSQL test container: %v", err)
		}
		t.Cleanup(func() {
			if err := container.Terminate(ctx); err != nil {
				t.Errorf("terminate PostgreSQL test container: %v", err)
			}
		})
		dsn, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("get PostgreSQL test container connection string: %v", err)
		}
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTestServices(t *testing.T, pool *pgxpool.Pool) (*service.TransferService, *service.WalletService) {
	t.Helper()
	uow := postgres.NewUnitOfWork(pool, 3)
	return service.NewTransferService(uow), service.NewWalletService(uow)
}

// TestPostgres_ConcurrentTransfers_NoLostUpdates fires many concurrent
// transfers between two real wallets and asserts the final balances match a
// serial execution exactly, proving SELECT ... FOR UPDATE row locking
// prevents lost updates and double spending under genuine concurrent
// Postgres transactions.
func TestPostgres_ConcurrentTransfers_NoLostUpdates(t *testing.T) {
	pool := newTestPool(t)
	transfers, wallets := newTestServices(t, pool)
	ctx := context.Background()

	fromID := "it-" + uuid.NewString()
	toID := "it-" + uuid.NewString()
	const startingBalance = 100_000
	const transferAmount = 10
	const numTransfers = 100

	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: fromID, InitialBalance: startingBalance}); err != nil {
		t.Fatalf("create from wallet: %v", err)
	}
	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: toID, InitialBalance: 0}); err != nil {
		t.Fatalf("create to wallet: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, numTransfers)
	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("it-concurrent-%s-%d", fromID, i),
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         transferAmount,
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d failed: %v", i, err)
		}
	}

	from, err := wallets.GetWallet(ctx, fromID)
	if err != nil {
		t.Fatalf("get from wallet: %v", err)
	}
	to, err := wallets.GetWallet(ctx, toID)
	if err != nil {
		t.Fatalf("get to wallet: %v", err)
	}

	wantFrom := int64(startingBalance - numTransfers*transferAmount)
	wantTo := int64(numTransfers * transferAmount)
	if from.Balance != wantFrom {
		t.Errorf("from wallet balance = %d, want %d", from.Balance, wantFrom)
	}
	if to.Balance != wantTo {
		t.Errorf("to wallet balance = %d, want %d", to.Balance, wantTo)
	}
}

// TestPostgres_OppositeDirectionTransfers_NoDeadlock verifies that concurrent
// transfers between the same wallets complete even when their directions are
// reversed. Each request must lock the wallets in the same database order.
func TestPostgres_OppositeDirectionTransfers_NoDeadlock(t *testing.T) {
	pool := newTestPool(t)
	transfers, wallets := newTestServices(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	firstID := "it-deadlock-a-" + uuid.NewString()
	secondID := "it-deadlock-b-" + uuid.NewString()
	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: firstID, InitialBalance: 1_000}); err != nil {
		t.Fatalf("create first wallet: %v", err)
	}
	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: secondID, InitialBalance: 1_000}); err != nil {
		t.Fatalf("create second wallet: %v", err)
	}

	results := make(chan error, 2)
	go func() {
		_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
			IdempotencyKey: "it-deadlock-a-to-b-" + uuid.NewString(),
			FromWalletID:   firstID,
			ToWalletID:     secondID,
			Amount:         10,
		})
		results <- err
	}()
	go func() {
		_, err := transfers.CreateTransfer(ctx, service.CreateTransferInput{
			IdempotencyKey: "it-deadlock-b-to-a-" + uuid.NewString(),
			FromWalletID:   secondID,
			ToWalletID:     firstID,
			Amount:         10,
		})
		results <- err
	}()

	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("opposite-direction transfer %d: %v", i+1, err)
		}
	}

	first, err := wallets.GetWallet(ctx, firstID)
	if err != nil {
		t.Fatalf("get first wallet: %v", err)
	}
	second, err := wallets.GetWallet(ctx, secondID)
	if err != nil {
		t.Fatalf("get second wallet: %v", err)
	}
	if first.Balance != 1_000 || second.Balance != 1_000 {
		t.Fatalf("balances = (%d, %d), want (1000, 1000)", first.Balance, second.Balance)
	}
}

// TestPostgres_IdempotentReplay_AcrossTransactions verifies that a
// duplicate request replays the original committed response rather than
// re-executing the transfer against the real database.
func TestPostgres_IdempotentReplay_AcrossTransactions(t *testing.T) {
	pool := newTestPool(t)
	transfers, wallets := newTestServices(t, pool)
	ctx := context.Background()

	fromID := "it-" + uuid.NewString()
	toID := "it-" + uuid.NewString()
	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: fromID, InitialBalance: 500}); err != nil {
		t.Fatalf("create from wallet: %v", err)
	}
	if _, err := wallets.CreateWallet(ctx, service.CreateWalletInput{ID: toID, InitialBalance: 0}); err != nil {
		t.Fatalf("create to wallet: %v", err)
	}

	in := service.CreateTransferInput{
		IdempotencyKey: "it-dup-" + uuid.NewString(),
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         100,
	}

	first, err := transfers.CreateTransfer(ctx, in)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	second, err := transfers.CreateTransfer(ctx, in)
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	if !second.Replayed {
		t.Error("expected second identical request to be replayed")
	}
	if second.Transfer.ID != first.Transfer.ID {
		t.Errorf("replayed transfer ID = %s, want %s", second.Transfer.ID, first.Transfer.ID)
	}

	from, err := wallets.GetWallet(ctx, fromID)
	if err != nil {
		t.Fatalf("get from wallet: %v", err)
	}
	if from.Balance != 400 {
		t.Errorf("from wallet balance = %d, want 400 (duplicate must not double-debit)", from.Balance)
	}
}
