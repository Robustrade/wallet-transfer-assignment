package service_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/db"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/service"
)

// TestCreateTransfer_ConcurrentSameIdempotencyKey exercises the real
// idempotency race against a real Postgres, not fakes: many concurrent
// CreateTransfer calls with the same idempotencyKey must all resolve to
// exactly one PROCESSED transfer, with exactly one debit/credit applied —
// the unit tests in transfer_service_test.go assert this behavior against
// fakes standing in for both sides of the race, which proves the service's
// logic is correct but can't prove the database actually enforces it.
func TestCreateTransfer_ConcurrentSameIdempotencyKey(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	fromID := "test_" + uuid.NewString()
	toID := "test_" + uuid.NewString()
	_, err = pool.Exec(context.Background(), `INSERT INTO wallets (id, balance) VALUES ($1, 1000), ($2, 0)`, fromID, toID)
	require.NoError(t, err)
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM ledger_entries WHERE wallet_id IN ($1, $2)`, fromID, toID)                             //nolint:errcheck
		pool.Exec(bg, `DELETE FROM transfers WHERE from_wallet_id IN ($1, $2) OR to_wallet_id IN ($1, $2)`, fromID, toID) //nolint:errcheck
		pool.Exec(bg, `DELETE FROM wallets WHERE id IN ($1, $2)`, fromID, toID)                                           //nolint:errcheck
	})

	wallets := postgres.NewWalletRepository(pool)
	transfers := postgres.NewTransferRepository(pool)
	executor := postgres.NewTransferExecutor(pool)
	svc := service.NewTransferService(wallets, transfers, executor)

	const concurrency = 20
	idempotencyKey := "race-" + uuid.NewString()

	results := make([]struct {
		id    string
		state domain.TransferState
		err   error
	}, concurrency)

	var ready, done sync.WaitGroup
	ready.Add(concurrency)
	done.Add(concurrency)
	start := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			transfer, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   fromID,
				ToWalletID:     toID,
				Amount:         100,
			})
			results[i].err = err
			if transfer != nil {
				results[i].id = transfer.ID
				results[i].state = transfer.State
			}
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()

	// Every caller must see the same transfer, and none may be told it
	// succeeded unless it is actually PROCESSED. A caller that observed an
	// in-flight winner gets ErrTransferInProgress, never a silent PENDING
	// dressed up as success.
	firstID := ""
	for i, r := range results {
		if errors.Is(r.err, domain.ErrTransferInProgress) {
			assert.Equalf(t, domain.TransferPending, r.state, "call %d", i)
			continue
		}
		require.NoErrorf(t, r.err, "call %d", i)
		assert.Equalf(t, domain.TransferProcessed, r.state,
			"call %d reported success, so the transfer must be PROCESSED", i)
		if firstID == "" {
			firstID = r.id
		}
		assert.Equalf(t, firstID, r.id, "call %d returned a different transfer than the others", i)
	}
	require.NotEmpty(t, firstID, "at least one caller must observe the processed transfer")

	fromWallet, err := wallets.Get(context.Background(), fromID)
	require.NoError(t, err)
	assert.EqualValues(t, 900, fromWallet.Balance, "the transfer must have been applied exactly once")

	toWallet, err := wallets.Get(context.Background(), toID)
	require.NoError(t, err)
	assert.EqualValues(t, 100, toWallet.Balance)

	// Exactly one transfer row and exactly one balanced ledger pair.
	var transferRows, debits, credits int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM transfers WHERE idempotency_key = $1`, idempotencyKey).Scan(&transferRows))
	assert.Equal(t, 1, transferRows)

	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FILTER (WHERE type = 'DEBIT'), count(*) FILTER (WHERE type = 'CREDIT')
		 FROM ledger_entries l JOIN transfers t ON t.id = l.transfer_id
		 WHERE t.idempotency_key = $1`, idempotencyKey).Scan(&debits, &credits))
	assert.Equal(t, 1, debits, "exactly one debit row")
	assert.Equal(t, 1, credits, "exactly one credit row")
}

func TestCreateTransfer_ResumesATransferStrandedInPending(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	bg := context.Background()
	fromID := "test_" + uuid.NewString()
	toID := "test_" + uuid.NewString()
	_, err = pool.Exec(bg, `INSERT INTO wallets (id, balance) VALUES ($1, 1000), ($2, 0)`, fromID, toID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(bg, `DELETE FROM ledger_entries WHERE wallet_id IN ($1, $2)`, fromID, toID)                             //nolint:errcheck
		pool.Exec(bg, `DELETE FROM transfers WHERE from_wallet_id IN ($1, $2) OR to_wallet_id IN ($1, $2)`, fromID, toID) //nolint:errcheck
		pool.Exec(bg, `DELETE FROM wallets WHERE id IN ($1, $2)`, fromID, toID)                                           //nolint:errcheck
	})

	wallets := postgres.NewWalletRepository(pool)
	transfers := postgres.NewTransferRepository(pool)
	svc := service.NewTransferService(wallets, transfers, postgres.NewTransferExecutor(pool))

	key := "stranded-" + uuid.NewString()
	strandedID := uuid.NewString()
	_, err = pool.Exec(bg,
		`INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount, state)
		 VALUES ($1, $2, $3, $4, $5, 100, 'PENDING')`,
		strandedID, key, domain.RequestFingerprint(fromID, toID, 100), fromID, toID)
	require.NoError(t, err)

	got, err := svc.CreateTransfer(bg, service.CreateTransferInput{
		IdempotencyKey: key, FromWalletID: fromID, ToWalletID: toID, Amount: 100,
	})
	require.NoError(t, err, "the retry must finish the stranded transfer")
	assert.Equal(t, strandedID, got.ID, "it must resume the original transfer, not create a new one")
	assert.Equal(t, domain.TransferProcessed, got.State)

	fromWallet, err := wallets.Get(bg, fromID)
	require.NoError(t, err)
	assert.EqualValues(t, 900, fromWallet.Balance)

	toWallet, err := wallets.Get(bg, toID)
	require.NoError(t, err)
	assert.EqualValues(t, 100, toWallet.Balance)

	var debits, credits int
	require.NoError(t, pool.QueryRow(bg,
		`SELECT count(*) FILTER (WHERE type='DEBIT'), count(*) FILTER (WHERE type='CREDIT')
		 FROM ledger_entries WHERE transfer_id = $1`, strandedID).Scan(&debits, &credits))
	assert.Equal(t, 1, debits)
	assert.Equal(t, 1, credits)
}

// An idempotency key is bound to the request it was first used with. Reusing
// it for a different transfer must be refused, not silently answered with the
// original result for a transfer the caller never asked for.
func TestCreateTransfer_ReusedKeyIsBoundToItsPayload(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	bg := context.Background()
	fromID := "test_" + uuid.NewString()
	toID := "test_" + uuid.NewString()
	otherID := "test_" + uuid.NewString()
	_, err = pool.Exec(bg, `INSERT INTO wallets (id, balance) VALUES ($1, 1000), ($2, 0), ($3, 0)`, fromID, toID, otherID)
	require.NoError(t, err)
	t.Cleanup(func() {
		ids := []any{fromID, toID, otherID}
		pool.Exec(bg, `DELETE FROM ledger_entries WHERE wallet_id IN ($1,$2,$3)`, ids...)                               //nolint:errcheck
		pool.Exec(bg, `DELETE FROM transfers WHERE from_wallet_id IN ($1,$2,$3) OR to_wallet_id IN ($1,$2,$3)`, ids...) //nolint:errcheck
		pool.Exec(bg, `DELETE FROM wallets WHERE id IN ($1,$2,$3)`, ids...)                                             //nolint:errcheck
	})

	wallets := postgres.NewWalletRepository(pool)
	svc := service.NewTransferService(wallets, postgres.NewTransferRepository(pool), postgres.NewTransferExecutor(pool))

	key := "bound-" + uuid.NewString()
	first, err := svc.CreateTransfer(bg, service.CreateTransferInput{
		IdempotencyKey: key, FromWalletID: fromID, ToWalletID: toID, Amount: 100,
	})
	require.NoError(t, err)
	require.Equal(t, domain.TransferProcessed, first.State)

	// Same key, different destination and amount.
	_, err = svc.CreateTransfer(bg, service.CreateTransferInput{
		IdempotencyKey: key, FromWalletID: fromID, ToWalletID: otherID, Amount: 500,
	})
	assert.ErrorIs(t, err, domain.ErrPayloadMismatch)

	// The refusal must not have moved anything.
	fromWallet, err := wallets.Get(bg, fromID)
	require.NoError(t, err)
	assert.EqualValues(t, 900, fromWallet.Balance, "only the original transfer may have applied")

	other, err := wallets.Get(bg, otherID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, other.Balance, "the mismatched request must not have executed")

	// An identical retry of the original still replays cleanly.
	again, err := svc.CreateTransfer(bg, service.CreateTransferInput{
		IdempotencyKey: key, FromWalletID: fromID, ToWalletID: toID, Amount: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
}
