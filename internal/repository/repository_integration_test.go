package repository_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	migrations "github.com/Robustrade/wallet-transfer-assignment/migrations"
)

// newTestDB starts a throwaway PostgreSQL container, runs migrations, and
// returns a pool pointed at it. The container is cleaned up when the test ends.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("wallet_test"),
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

// ── Wallet repository ─────────────────────────────────────────────────────────

func TestWalletRepo_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	repo := repository.NewWalletRepo(slog.Default())

	w := domain.NewWallet()
	w.Balance = 500_00 // seed with 500.00

	if err := repo.Create(ctx, pool, w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, pool, w.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if got.ID != w.ID {
		t.Fatalf("ID mismatch: got %s, want %s", got.ID, w.ID)
	}

	if got.Balance != w.Balance {
		t.Fatalf("Balance mismatch: got %d, want %d", got.Balance, w.Balance)
	}
}

func TestWalletRepo_GetByID_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	repo := repository.NewWalletRepo(slog.Default())

	_, err := repo.GetByID(ctx, pool, uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown wallet, got nil")
	}
}

func TestWalletRepo_UpdateBalance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	repo := repository.NewWalletRepo(slog.Default())

	w := domain.NewWallet()
	w.Balance = 1000_00

	if err := repo.Create(ctx, pool, w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	if err := repo.UpdateBalance(ctx, tx, w.ID, 750_00); err != nil {
		t.Fatalf("UpdateBalance: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	updated, err := repo.GetByID(ctx, pool, w.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}

	if updated.Balance != 750_00 {
		t.Fatalf("balance after update: got %d, want %d", updated.Balance, 750_00)
	}

	if updated.Version != w.Version+1 {
		t.Fatalf("version must increment: got %d, want %d", updated.Version, w.Version+1)
	}
}

// ── Transfer repository ───────────────────────────────────────────────────────

func TestTransferRepo_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	walletRepo := repository.NewWalletRepo(slog.Default())
	transferRepo := repository.NewTransferRepo(slog.Default())

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	if err := walletRepo.Create(ctx, pool, from); err != nil {
		t.Fatalf("Create from wallet: %v", err)
	}

	if err := walletRepo.Create(ctx, pool, to); err != nil {
		t.Fatalf("Create to wallet: %v", err)
	}

	tr := domain.NewTransfer(from.ID, to.ID, "idem-repo-001", 100_00)
	if err := transferRepo.Create(ctx, pool, tr); err != nil {
		t.Fatalf("Create transfer: %v", err)
	}

	got, err := transferRepo.GetByID(ctx, pool, tr.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if got.ID != tr.ID {
		t.Fatalf("transfer ID mismatch")
	}

	if got.Status != domain.TransferStatusPending {
		t.Fatalf("expected PENDING, got %s", got.Status)
	}
}

func TestTransferRepo_UpdateStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	walletRepo := repository.NewWalletRepo(slog.Default())
	transferRepo := repository.NewTransferRepo(slog.Default())

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	if err := walletRepo.Create(ctx, pool, from); err != nil {
		t.Fatalf("Create from wallet: %v", err)
	}

	if err := walletRepo.Create(ctx, pool, to); err != nil {
		t.Fatalf("Create to wallet: %v", err)
	}

	tr := domain.NewTransfer(from.ID, to.ID, "idem-repo-002", 50_00)
	if err := transferRepo.Create(ctx, pool, tr); err != nil {
		t.Fatalf("Create transfer: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	if err := transferRepo.UpdateStatus(ctx, tx, tr.ID, domain.TransferStatusProcessed); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := transferRepo.GetByID(ctx, pool, tr.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}

	if got.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", got.Status)
	}
}

// ── Ledger repository ─────────────────────────────────────────────────────────

func TestLedgerRepo_CreateEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	walletRepo := repository.NewWalletRepo(slog.Default())
	transferRepo := repository.NewTransferRepo(slog.Default())
	ledgerRepo := repository.NewLedgerRepo(slog.Default())

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	if err := walletRepo.Create(ctx, pool, from); err != nil {
		t.Fatalf("Create from wallet: %v", err)
	}

	if err := walletRepo.Create(ctx, pool, to); err != nil {
		t.Fatalf("Create to wallet: %v", err)
	}

	tr := domain.NewTransfer(from.ID, to.ID, "idem-ledger-001", 200_00)
	if err := transferRepo.Create(ctx, pool, tr); err != nil {
		t.Fatalf("Create transfer: %v", err)
	}

	debit := domain.NewLedgerEntry(tr.ID, from.ID, domain.EntryTypeDebit, 200_00)
	credit := domain.NewLedgerEntry(tr.ID, to.ID, domain.EntryTypeCredit, 200_00)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	if err := ledgerRepo.CreateEntries(ctx, tx, debit, credit); err != nil {
		t.Fatalf("CreateEntries: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// ── Idempotency repository ────────────────────────────────────────────────────

func TestIdempotencyRepo_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	walletRepo := repository.NewWalletRepo(slog.Default())
	transferRepo := repository.NewTransferRepo(slog.Default())
	idemRepo := repository.NewIdempotencyRepo(slog.Default())

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	if err := walletRepo.Create(ctx, pool, from); err != nil {
		t.Fatalf("Create from wallet: %v", err)
	}

	if err := walletRepo.Create(ctx, pool, to); err != nil {
		t.Fatalf("Create to wallet: %v", err)
	}

	tr := domain.NewTransfer(from.ID, to.ID, "idem-cache-001", 100_00)
	if err := transferRepo.Create(ctx, pool, tr); err != nil {
		t.Fatalf("Create transfer: %v", err)
	}

	rec := repository.IdempotencyRecord{
		Key:          "idem-cache-001",
		TransferID:   tr.ID,
		ResponseJSON: `{"id":"` + tr.ID.String() + `","status":"PROCESSED"}`,
		StatusCode:   200,
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	defer tx.Rollback(ctx) //nolint:errcheck

	if err := idemRepo.Create(ctx, tx, rec); err != nil {
		t.Fatalf("Create idempotency record: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := idemRepo.Get(ctx, pool, "idem-cache-001")
	if err != nil {
		t.Fatalf("Get idempotency record: %v", err)
	}

	if got.Key != rec.Key {
		t.Fatalf("key mismatch: got %s, want %s", got.Key, rec.Key)
	}

	if got.ResponseJSON != rec.ResponseJSON {
		t.Fatalf("response JSON mismatch")
	}
}

func TestIdempotencyRepo_Get_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool := newTestDB(t)
	ctx := context.Background()
	repo := repository.NewIdempotencyRepo(slog.Default())

	_, err := repo.Get(ctx, pool, "missing-key")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}
