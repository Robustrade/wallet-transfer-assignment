package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
)

// testDB is the shared package-level handle started once by TestMain. It is nil
// when Docker is unavailable, in which case every test skips.
var (
	testDB      *DB
	dockerErr   error
	testCleanup func()
)

// TestMain starts a single postgres container for the whole package, runs
// migrations, and shares the *DB. When Docker is unavailable the suite does not
// fail or hang: dockerErr is recorded and each test skips via requireDB.
// startContainer runs the postgres module, converting both errors and the
// panic testcontainers raises when no Docker host is reachable into a returned
// error so TestMain can skip instead of crashing.
func startContainer(ctx context.Context) (container *tcpostgres.PostgresContainer, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("docker unavailable: %v", p)
		}
	}()
	return tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("wallet"),
		tcpostgres.WithUsername("wallet"),
		tcpostgres.WithPassword("wallet"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second),
		),
	)
}

func TestMain(m *testing.M) {
	code := func() int {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		container, err := startContainer(ctx)
		if err != nil {
			dockerErr = fmt.Errorf("start postgres container: %w", err)
			return m.Run()
		}
		testCleanup = func() {
			_ = container.Terminate(context.Background())
		}
		defer testCleanup()

		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			dockerErr = fmt.Errorf("connection string: %w", err)
			return m.Run()
		}

		db, err := NewDB(ctx, dsn)
		if err != nil {
			dockerErr = fmt.Errorf("open db: %w", err)
			return m.Run()
		}
		defer db.Close()

		if err := db.Migrate(ctx); err != nil {
			dockerErr = fmt.Errorf("migrate: %w", err)
			return m.Run()
		}

		testDB = db
		return m.Run()
	}()
	os.Exit(code)
}

// requireDB skips the calling test when the shared DB is unavailable (Docker
// down). It returns a ready-to-use DB otherwise.
func requireDB(t *testing.T) *DB {
	t.Helper()
	if testDB == nil {
		t.Skipf("docker unavailable: %v", dockerErr)
	}
	return testDB
}

// truncateAll clears all tables so tests are independent.
func truncateAll(t *testing.T, db *DB) {
	t.Helper()
	_, err := db.pool.Exec(context.Background(),
		`TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

func newWallet(t *testing.T, db *DB, balance domain.Money) *domain.Wallet {
	t.Helper()
	w := &domain.Wallet{ID: uuid.NewString(), Balance: balance}
	require.NoError(t, NewWalletRepository(db).Create(context.Background(), w))
	return w
}

func TestMigrateIdempotent(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()

	// Already applied once in TestMain; applying again must be a no-op.
	require.NoError(t, db.Migrate(ctx))
	require.NoError(t, db.Migrate(ctx))

	var count int
	require.NoError(t, db.pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations WHERE version = '0001_init.sql'`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestWalletRoundTrip(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	repo := NewWalletRepository(db)

	w := &domain.Wallet{ID: uuid.NewString(), Balance: 1000}
	require.NoError(t, repo.Create(ctx, w))

	got, err := repo.Get(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)
	require.Equal(t, domain.Money(1000), got.Balance)
	require.False(t, got.CreatedAt.IsZero())

	require.NoError(t, repo.UpdateBalance(ctx, w.ID, 750))
	got, err = repo.Get(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, domain.Money(750), got.Balance)
}

func TestWalletGetMissing(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	repo := NewWalletRepository(db)

	_, err := repo.Get(ctx, uuid.NewString())
	require.ErrorIs(t, err, domain.ErrWalletNotFound)

	err = repo.UpdateBalance(ctx, uuid.NewString(), 10)
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestWalletNegativeBalanceRejected(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	repo := NewWalletRepository(db)

	w := newWallet(t, db, 100)
	err := repo.UpdateBalance(ctx, w.ID, -1)
	require.Error(t, err)

	// Insert violating the CHECK should also fail.
	err = repo.Create(ctx, &domain.Wallet{ID: uuid.NewString(), Balance: -5})
	require.Error(t, err)
}

// TestGetForUpdateLocks verifies SELECT ... FOR UPDATE actually serializes
// concurrent transactions on the same wallet row.
func TestGetForUpdateLocks(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	repo := NewWalletRepository(db)

	w := newWallet(t, db, 1000)

	tx1Locked := make(chan struct{})
	releaseTx1 := make(chan struct{})
	tx2Acquired := make(chan struct{})
	tx1Done := make(chan error, 1)
	tx2Done := make(chan error, 1)

	// Tx1 locks the row and holds it until released.
	go func() {
		tx1Done <- db.WithinTx(ctx, func(ctx context.Context) error {
			wl, err := repo.GetForUpdate(ctx, w.ID)
			if err != nil {
				return err
			}
			close(tx1Locked)
			<-releaseTx1
			return repo.UpdateBalance(ctx, wl.ID, wl.Balance-100)
		})
	}()

	<-tx1Locked

	// Tx2 attempts to lock the same row; it must block while Tx1 holds the lock.
	go func() {
		tx2Done <- db.WithinTx(ctx, func(ctx context.Context) error {
			wl, err := repo.GetForUpdate(ctx, w.ID)
			if err != nil {
				return err
			}
			close(tx2Acquired)
			return repo.UpdateBalance(ctx, wl.ID, wl.Balance-200)
		})
	}()

	// Tx2 should still be blocked.
	select {
	case <-tx2Acquired:
		t.Fatal("tx2 acquired lock while tx1 held it; FOR UPDATE did not block")
	case <-time.After(300 * time.Millisecond):
		// Expected: tx2 is blocked.
	}

	// Release tx1; tx2 should then proceed.
	close(releaseTx1)
	require.NoError(t, <-tx1Done)

	select {
	case <-tx2Acquired:
		// Good: tx2 unblocked after tx1 committed.
	case <-time.After(5 * time.Second):
		t.Fatal("tx2 never acquired lock after tx1 released")
	}
	require.NoError(t, <-tx2Done)

	// Both debits applied serially: 1000 - 100 - 200 = 700.
	got, err := repo.Get(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, domain.Money(700), got.Balance)
}

func TestTransferRoundTrip(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()

	from := newWallet(t, db, 1000)
	to := newWallet(t, db, 0)
	repo := NewTransferRepository(db)

	tr, err := domain.NewTransfer(uuid.NewString(), "key-1", from.ID, to.ID, 250)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, tr))

	got, err := repo.Get(ctx, tr.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusPending, got.Status)
	require.Equal(t, domain.Money(250), got.Amount)

	require.NoError(t, got.MarkProcessed())
	require.NoError(t, repo.Update(ctx, got))

	got2, err := repo.Get(ctx, tr.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusProcessed, got2.Status)

	_, err = repo.Get(ctx, uuid.NewString())
	require.ErrorIs(t, err, domain.ErrTransferNotFound)
}

func TestLedgerCreateAndList(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()

	from := newWallet(t, db, 1000)
	to := newWallet(t, db, 0)
	tr, err := domain.NewTransfer(uuid.NewString(), "key-ledger", from.ID, to.ID, 300)
	require.NoError(t, err)
	require.NoError(t, NewTransferRepository(db).Create(ctx, tr))

	ledger := NewLedgerRepository(db)
	entries := domain.NewDoubleEntry(uuid.NewString(), uuid.NewString(), tr.ID, from.ID, to.ID, 300)
	require.NoError(t, ledger.CreateEntries(ctx, entries))

	fromEntries, err := ledger.ListByWallet(ctx, from.ID)
	require.NoError(t, err)
	require.Len(t, fromEntries, 1)
	require.Equal(t, domain.EntryDebit, fromEntries[0].Type)
}

func TestLedgerUniquePerType(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()

	from := newWallet(t, db, 1000)
	to := newWallet(t, db, 0)
	tr, err := domain.NewTransfer(uuid.NewString(), "key-dup-ledger", from.ID, to.ID, 100)
	require.NoError(t, err)
	require.NoError(t, NewTransferRepository(db).Create(ctx, tr))

	ledger := NewLedgerRepository(db)
	first := []domain.LedgerEntry{{
		ID: uuid.NewString(), WalletID: from.ID, TransferID: tr.ID, Type: domain.EntryDebit, Amount: 100,
	}}
	require.NoError(t, ledger.CreateEntries(ctx, first))

	second := []domain.LedgerEntry{{
		ID: uuid.NewString(), WalletID: from.ID, TransferID: tr.ID, Type: domain.EntryDebit, Amount: 100,
	}}
	err = ledger.CreateEntries(ctx, second)
	require.Error(t, err, "second DEBIT for same transfer must violate UNIQUE (transfer_id, type)")
}

func TestIdempotencyInsertDuplicate(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	repo := NewIdempotencyRepository(db)

	rec := &domain.IdempotencyRecord{
		Key:         "idem-key",
		RequestHash: "hash-abc",
		Status:      domain.IdempotencyPending,
	}
	require.NoError(t, repo.Insert(ctx, rec))

	dup := &domain.IdempotencyRecord{
		Key:         "idem-key",
		RequestHash: "hash-abc",
		Status:      domain.IdempotencyPending,
	}
	err := repo.Insert(ctx, dup)
	require.ErrorIs(t, err, domain.ErrDuplicateIdempotencyKey)
}

func TestIdempotencyGetAndComplete(t *testing.T) {
	db := requireDB(t)
	truncateAll(t, db)
	ctx := context.Background()

	from := newWallet(t, db, 1000)
	to := newWallet(t, db, 0)
	tr, err := domain.NewTransfer(uuid.NewString(), "idem-complete", from.ID, to.ID, 100)
	require.NoError(t, err)
	require.NoError(t, NewTransferRepository(db).Create(ctx, tr))

	repo := NewIdempotencyRepository(db)
	require.NoError(t, repo.Insert(ctx, &domain.IdempotencyRecord{
		Key:         "idem-complete",
		RequestHash: "hash",
		Status:      domain.IdempotencyPending,
	}))

	body := []byte(`{"transfer_id":"x","status":"PROCESSED"}`)
	require.NoError(t, repo.Complete(ctx, "idem-complete", tr.ID, body))

	got, err := repo.Get(ctx, "idem-complete")
	require.NoError(t, err)
	require.Equal(t, domain.IdempotencyCompleted, got.Status)
	require.Equal(t, tr.ID, got.TransferID)
	require.JSONEq(t, string(body), string(got.ResponseBody))
}
