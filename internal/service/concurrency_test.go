package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
)

func setupTestDB(t *testing.T) (*sql.DB, *service.TransferService) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
	}

	db, err := postgres.Open(context.Background(), dbURL, postgres.PoolConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    10,
		ConnMaxLifetime: 5 * time.Minute,
	})
	if err != nil {
		t.Skip("skipping concurrency test: database not reachable")
	}

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	_, err = db.Exec("CREATE SCHEMA " + schema)
	if err != nil {
		t.Skip("skipping concurrency test: cannot create schema")
	}

	t.Cleanup(func() {
		_, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = db.Close()
	})

	testDB, err := postgres.Open(context.Background(), dbURL+"&search_path="+schema, postgres.PoolConfig{
		MaxOpenConns: 20,
		MaxIdleConns: 10,
	})
	require.NoError(t, err)

	_, err = postgres.Migrate(context.Background(), testDB, "../../migrations")
	require.NoError(t, err)

	txManager := postgres.NewTxManager(testDB)
	wallets := postgres.NewWalletRepository(txManager)
	transfers := postgres.NewTransferRepository(txManager)
	ledger := postgres.NewLedgerRepository(txManager)
	idempotency := postgres.NewIdempotencyRepository(txManager)

	svc := service.NewTransferService(txManager, wallets, transfers, ledger, idempotency)
	return testDB, svc
}

func TestConcurrentDebitsCannotOverdraw(t *testing.T) {
	db, svc := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO wallets (id, currency, balance) VALUES ('src_wallet', 'INR', 800), ('dst_wallet', 'INR', 0) ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance`)
	require.NoError(t, err)

	const attempts = 20
	const amount = domain.Amount(100)
	const affordable = 8

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		errs  = make([]error, attempts)
	)

	start.Add(1)
	done.Add(attempts)

	for i := 0; i < attempts; i++ {
		go func(idx int) {
			defer done.Done()
			start.Wait()
			_, errs[idx] = svc.Transfer(context.Background(), domain.TransferRequest{
				IdempotencyKey: fmt.Sprintf("key-concur-%d", idx),
				FromWalletID:   "src_wallet",
				ToWalletID:     "dst_wallet",
				Amount:         amount,
			})
		}(i)
	}

	start.Done()
	done.Wait()

	var succeeded, rejected int
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if errors.Is(err, domain.ErrInsufficientFunds) {
			rejected++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	require.Equal(t, affordable, succeeded)
	require.Equal(t, attempts-affordable, rejected)

	var srcBal, dstBal int64
	require.NoError(t, db.QueryRow("SELECT balance FROM wallets WHERE id = 'src_wallet'").Scan(&srcBal))
	require.NoError(t, db.QueryRow("SELECT balance FROM wallets WHERE id = 'dst_wallet'").Scan(&dstBal))
	require.Equal(t, int64(0), srcBal)
	require.Equal(t, int64(800), dstBal)
}

func TestConcurrentDuplicateTransfers(t *testing.T) {
	db, svc := setupTestDB(t)

	_, err := db.Exec(`INSERT INTO wallets (id, currency, balance) VALUES ('src_dup', 'INR', 10000), ('dst_dup', 'INR', 0) ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance`)
	require.NoError(t, err)

	const attempts = 10
	sameKey := "same-idempotency-key-" + uuid.NewString()

	var (
		start    sync.WaitGroup
		done     sync.WaitGroup
		outcomes = make([]service.TransferOutcome, attempts)
		errs     = make([]error, attempts)
	)

	start.Add(1)
	done.Add(attempts)

	for i := 0; i < attempts; i++ {
		go func(idx int) {
			defer done.Done()
			start.Wait()
			outcomes[idx], errs[idx] = svc.Transfer(context.Background(), domain.TransferRequest{
				IdempotencyKey: sameKey,
				FromWalletID:   "src_dup",
				ToWalletID:     "dst_dup",
				Amount:         100,
			})
		}(i)
	}

	start.Done()
	done.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	var replayedCount, originalCount int
	for _, o := range outcomes {
		if o.Replayed {
			replayedCount++
		} else {
			originalCount++
		}
	}
	require.Equal(t, 1, originalCount)
	require.Equal(t, attempts-1, replayedCount)

	var transferCount int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM transfers WHERE from_wallet_id = 'src_dup'").Scan(&transferCount))
	require.Equal(t, 1, transferCount)

	var srcBal, dstBal int64
	require.NoError(t, db.QueryRow("SELECT balance FROM wallets WHERE id = 'src_dup'").Scan(&srcBal))
	require.NoError(t, db.QueryRow("SELECT balance FROM wallets WHERE id = 'dst_dup'").Scan(&dstBal))
	require.Equal(t, int64(9900), srcBal)
	require.Equal(t, int64(100), dstBal)
}
