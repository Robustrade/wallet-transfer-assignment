package migrations_test

import (
	"context"
	"testing"
	"time"

	"wallet-transfer-assignment/migrations"
	"wallet-transfer-assignment/pkg/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrations_Apply(t *testing.T) {
	pool := testutil.SetupAdminDB(t)

	// Verify migrations apply cleanly and idempotently on an existing pool
	if err := migrations.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}
}

func TestMigrations_CanceledContextReleasesAdvisoryLock(t *testing.T) {
	pool := testutil.SetupAdminDB(t)
	ctx := context.Background()

	const migrationAdvisoryLockID = 888999

	// 1. Acquire connection 1 and acquire advisory lock 888999, creating an intentional lock contention
	blockerConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire blocker connection: %v", err)
	}
	defer blockerConn.Release()

	if _, err := blockerConn.Exec(ctx, "SELECT pg_advisory_lock($1);", migrationAdvisoryLockID); err != nil {
		t.Fatalf("failed to acquire blocker advisory lock: %v", err)
	}

	// 2. Launch Migrate with a short timeout context.
	// Migrate acquires a connection from the pool, attempts pg_advisory_lock(888999),
	// blocks because blockerConn holds it, and times out while waiting.
	timeoutCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()

	migrateErr := migrations.Migrate(timeoutCtx, pool)
	if migrateErr == nil {
		t.Fatalf("expected Migrate to fail due to context deadline exceeded, got nil")
	}

	// 3. Release the blocker advisory lock on connection 1
	if _, err := blockerConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1);", migrationAdvisoryLockID); err != nil {
		t.Fatalf("failed to release blocker advisory lock: %v", err)
	}

	// 4. Bound subsequent migration with a timeout.
	// If the timed-out Migrate leaked lock 888999 or its connection back to the pool in a locked state,
	// this call would block indefinitely until the timeout expires.
	followUpCtx, followUpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer followUpCancel()

	if err := migrations.Migrate(followUpCtx, pool); err != nil {
		t.Fatalf("subsequent migration failed; advisory lock was likely leaked to the pool: %v", err)
	}
}

func TestMigrations_SucceedsOnSingleConnectionPool(t *testing.T) {
	basePool := testutil.SetupAdminDB(t)
	cfg := basePool.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 1

	singleConnPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to create single-connection pool: %v", err)
	}
	defer singleConnPool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := migrations.Migrate(ctx, singleConnPool); err != nil {
		t.Fatalf("Migrate failed on a pool with MaxConns=1: %v", err)
	}
}

func TestMigrations_PostAcquisitionCancellationReleasesAdvisoryLock(t *testing.T) {
	pool := testutil.SetupAdminDB(t)
	ctx := context.Background()

	const migrationAdvisoryLockID = 888999

	// 1. Delete schema_migrations record so Migrate attempts to re-execute migration SQL
	if _, err := pool.Exec(ctx, "DELETE FROM schema_migrations;"); err != nil {
		t.Fatalf("failed to clear schema_migrations for test: %v", err)
	}

	// 2. Acquire a separate connection and hold ACCESS EXCLUSIVE lock on 'wallets' table
	// to deterministically pause Migrate *after* it acquires pg_advisory_lock(888999)
	// when it attempts to run the migration transaction.
	blockerConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire blocker connection: %v", err)
	}
	defer blockerConn.Release()

	blockerTx, err := blockerConn.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to start blocker transaction: %v", err)
	}
	defer func() {
		_ = blockerTx.Rollback(context.Background())
	}()

	if _, err := blockerTx.Exec(ctx, "LOCK TABLE wallets IN ACCESS EXCLUSIVE MODE;"); err != nil {
		t.Fatalf("failed to acquire ACCESS EXCLUSIVE lock on wallets: %v", err)
	}

	// 3. Launch Migrate in a goroutine with a cancellable context
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- migrations.Migrate(cancelCtx, pool)
	}()

	// 4. Poll pg_locks to verify pg_advisory_lock(888999) has actually been acquired
	// while the migration is blocked waiting for the table lock
	lockAcquired := false
	for i := 0; i < 50; i++ {
		var hasLock bool
		_ = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND objid = $1 AND granted = true);", migrationAdvisoryLockID).Scan(&hasLock)
		if hasLock {
			lockAcquired = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !lockAcquired {
		t.Fatalf("expected Migrate to acquire pg_advisory_lock(888999) before applying SQL")
	}

	// 5. Cancel the context *after* advisory lock acquisition
	cancel()

	// 6. Release the blocker table lock so the cancelled SQL execution unwinds
	_ = blockerTx.Rollback(context.Background())

	// 7. Verify Migrate returns an error
	select {
	case migrateErr := <-errCh:
		if migrateErr == nil {
			t.Fatalf("expected Migrate to fail after post-acquisition context cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for Migrate to exit after cancellation")
	}

	// 8. Verify advisory lock 888999 was released
	lockReleased := false
	for i := 0; i < 50; i++ {
		var hasLock bool
		_ = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND objid = $1 AND granted = true);", migrationAdvisoryLockID).Scan(&hasLock)
		if !hasLock {
			lockReleased = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !lockReleased {
		t.Fatalf("advisory lock 888999 was leaked after post-acquisition context cancellation")
	}

	// 9. Subsequent migration must succeed immediately
	followUpCtx, followUpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer followUpCancel()

	if err := migrations.Migrate(followUpCtx, pool); err != nil {
		t.Fatalf("subsequent migration failed after post-acquisition cancellation: %v", err)
	}
}
