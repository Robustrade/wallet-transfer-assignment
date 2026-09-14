package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/dbtest"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/repository"
)

func newStore(t *testing.T) (*repository.Store, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	return repository.New(pool), pool
}

// pendingTransfer builds a transfer ready to be claimed, between two freshly
// seeded wallets.
func pendingTransfer(from, to string, amount int64) domain.Transfer {
	return domain.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: "key_" + uuid.NewString(),
		RequestHash:    "hash",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         amount,
		State:          domain.StatePending,
	}
}

func TestClaimTakesAFreeIdempotencyKey(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	var stored domain.Transfer
	var claimed bool

	err := store.WithTx(context.Background(), func(ctx context.Context, tx *repository.Tx) error {
		var err error
		stored, claimed, err = tx.ClaimTransfer(ctx, pendingTransfer(from, to, 100))
		return err
	})
	if err != nil {
		t.Fatalf("claiming a free key: %v", err)
	}

	if !claimed {
		t.Fatal("a free idempotency key was not claimed")
	}
	if stored.CreatedAt.IsZero() {
		t.Error("claim did not return the stored creation time")
	}
}

// The first of a pair. This covers a duplicate arriving after the original has
// committed — the ordinary case, where the row is visible. Its sibling,
// TestConcurrentClaimsOfOneKeyBlockRatherThanRace, covers the duplicate that
// arrives while the first claim is still in flight, which is the case only
// ON CONFLICT DO NOTHING handles correctly. This one is kept alongside it
// because it is instant and has no timing window: if the concurrent test ever
// goes flaky, the basic guarantee still has a guard, and when both fail this one
// says immediately that the fault is not concurrency.
func TestClaimRefusesACommittedIdempotencyKey(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	transfer := pendingTransfer(from, to, 100)

	ctx := context.Background()
	if err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		_, _, err := tx.ClaimTransfer(ctx, transfer)
		return err
	}); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	second := pendingTransfer(from, to, 100)
	second.IdempotencyKey = transfer.IdempotencyKey

	var claimed bool
	if err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		var err error
		_, claimed, err = tx.ClaimTransfer(ctx, second)
		return err
	}); err != nil {
		t.Fatalf("second claim: %v", err)
	}

	if claimed {
		t.Fatal("a held idempotency key was claimed a second time")
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM transfers WHERE idempotency_key = $1`,
		transfer.IdempotencyKey).Scan(&count); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d transfers exist for one idempotency key, want 1", count)
	}
}

func TestClaimReportsWhichWalletIsMissing(t *testing.T) {
	store, pool := newStore(t)
	real := dbtest.Wallet(t, pool, 1000)

	tests := map[string]struct {
		from, to string
		missing  string
	}{
		"source wallet":      {from: "wallet_missing_source", to: real, missing: "wallet_missing_source"},
		"destination wallet": {from: real, to: "wallet_missing_destination", missing: "wallet_missing_destination"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := store.WithTx(context.Background(), func(ctx context.Context, tx *repository.Tx) error {
				_, _, err := tx.ClaimTransfer(ctx, pendingTransfer(tc.from, tc.to, 100))
				return err
			})

			if !errors.Is(err, domain.ErrWalletNotFound) {
				t.Fatalf("got %v, want ErrWalletNotFound", err)
			}

			var notFound *domain.WalletNotFoundError
			if !errors.As(err, &notFound) {
				t.Fatalf("error does not carry the wallet id: %v", err)
			}
			if notFound.WalletID != tc.missing {
				t.Fatalf("blamed wallet %q, want %q", notFound.WalletID, tc.missing)
			}
		})
	}
}

func TestTransferByIdempotencyKeyReturnsTheStoredTransfer(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	transfer := pendingTransfer(from, to, 250)

	ctx := context.Background()
	if err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		_, _, err := tx.ClaimTransfer(ctx, transfer)
		return err
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	var read domain.Transfer
	if err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		var err error
		read, err = tx.TransferByIdempotencyKey(ctx, transfer.IdempotencyKey)
		return err
	}); err != nil {
		t.Fatalf("read by key: %v", err)
	}

	if read.ID != transfer.ID {
		t.Errorf("id is %s, want %s", read.ID, transfer.ID)
	}
	if read.FromWalletID != from || read.ToWalletID != to {
		t.Errorf("wallets are %s -> %s, want %s -> %s", read.FromWalletID, read.ToWalletID, from, to)
	}
	if read.Amount != 250 {
		t.Errorf("amount is %d, want 250", read.Amount)
	}
	if read.State != domain.StatePending {
		t.Errorf("state is %s, want PENDING", read.State)
	}
	if read.FailureReason != "" {
		t.Errorf("failure reason is %q, want empty", read.FailureReason)
	}
}

func TestBalancesMoveByTheTransferredAmount(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 500)

	err := store.WithTx(context.Background(), func(ctx context.Context, tx *repository.Tx) error {
		if err := tx.DebitWallet(ctx, from, 300); err != nil {
			return err
		}
		return tx.CreditWallet(ctx, to, 300)
	})
	if err != nil {
		t.Fatalf("moving money: %v", err)
	}

	if got := dbtest.Balance(t, pool, from); got != 700 {
		t.Errorf("source balance is %d, want 700", got)
	}
	if got := dbtest.Balance(t, pool, to); got != 800 {
		t.Errorf("destination balance is %d, want 800", got)
	}
}

// The schema's last line of defence. Callers check sufficiency under the lock,
// so reaching this means a bug — and the constraint means the bug cannot cost
// anyone money.
func TestOverdraftIsRejectedByTheDatabase(t *testing.T) {
	store, pool := newStore(t)
	wallet := dbtest.Wallet(t, pool, 100)

	err := store.WithTx(context.Background(), func(ctx context.Context, tx *repository.Tx) error {
		return tx.DebitWallet(ctx, wallet, 101)
	})
	if err == nil {
		t.Fatal("debiting more than the balance succeeded")
	}

	if got := dbtest.Balance(t, pool, wallet); got != 100 {
		t.Fatalf("balance is %d after a rejected debit, want 100", got)
	}
}

func TestLedgerEntriesAreWrittenAsAPair(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	transfer := pendingTransfer(from, to, 100)

	ctx := context.Background()
	err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		stored, _, err := tx.ClaimTransfer(ctx, transfer)
		if err != nil {
			return err
		}
		return tx.InsertLedgerEntries(ctx, domain.LedgerEntriesFor(stored))
	})
	if err != nil {
		t.Fatalf("writing ledger entries: %v", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT wallet_id, type, amount FROM ledger_entries
		 WHERE transfer_id = $1 ORDER BY type`, transfer.ID)
	if err != nil {
		t.Fatalf("read ledger entries: %v", err)
	}
	defer rows.Close()

	type entry struct {
		wallet string
		kind   string
		amount int64
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.wallet, &e.kind, &e.amount); err != nil {
			t.Fatalf("scan entry: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d ledger entries, want exactly 2", len(entries))
	}
	if entries[0] != (entry{wallet: to, kind: "CREDIT", amount: 100}) {
		t.Errorf("credit entry is %+v, want a 100 credit on %s", entries[0], to)
	}
	if entries[1] != (entry{wallet: from, kind: "DEBIT", amount: 100}) {
		t.Errorf("debit entry is %+v, want a 100 debit on %s", entries[1], from)
	}
}

func TestATransferSettlesExactlyOnce(t *testing.T) {
	tests := map[string]func(ctx context.Context, tx *repository.Tx, id string) error{
		"processed": func(ctx context.Context, tx *repository.Tx, id string) error {
			return tx.MarkTransferProcessed(ctx, id)
		},
		"failed": func(ctx context.Context, tx *repository.Tx, id string) error {
			return tx.MarkTransferFailed(ctx, id, "insufficient funds")
		},
	}

	for name, settle := range tests {
		t.Run(name, func(t *testing.T) {
			store, pool := newStore(t)
			from := dbtest.Wallet(t, pool, 1000)
			to := dbtest.Wallet(t, pool, 0)
			transfer := pendingTransfer(from, to, 100)

			ctx := context.Background()
			err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
				if _, _, err := tx.ClaimTransfer(ctx, transfer); err != nil {
					return err
				}
				return settle(ctx, tx, transfer.ID)
			})
			if err != nil {
				t.Fatalf("settling: %v", err)
			}

			// A second attempt must be refused: the guarded UPDATE matches no
			// row once the transfer has left PENDING.
			err = store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
				return settle(ctx, tx, transfer.ID)
			})
			if !errors.Is(err, domain.ErrInvalidStateTransition) {
				t.Fatalf("got %v on a second settlement, want ErrInvalidStateTransition", err)
			}
		})
	}
}

func TestFailedTransferRecordsItsReason(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	transfer := pendingTransfer(from, to, 100)

	ctx := context.Background()
	err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		if _, _, err := tx.ClaimTransfer(ctx, transfer); err != nil {
			return err
		}
		return tx.MarkTransferFailed(ctx, transfer.ID, "insufficient funds")
	})
	if err != nil {
		t.Fatalf("failing the transfer: %v", err)
	}

	var read domain.Transfer
	if err := store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		var err error
		read, err = tx.TransferByIdempotencyKey(ctx, transfer.IdempotencyKey)
		return err
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}

	if read.State != domain.StateFailed {
		t.Errorf("state is %s, want FAILED", read.State)
	}
	if read.FailureReason != "insufficient funds" {
		t.Errorf("failure reason is %q, want %q", read.FailureReason, "insufficient funds")
	}
}

// Establishes that the hazard is real, so the ordering that prevents it is not
// cargo cult: two transactions taking the same pair of locks in opposite orders
// deadlock, and PostgreSQL kills one of them.
//
// The interleaving is forced rather than raced — each transaction takes its
// first lock before either reaches for its second — so this is deterministic.
// Deadlock detection fires at deadlock_timeout (1s by default), comfortably
// inside the 3s lock_timeout, so the error is a deadlock and not a timeout.
func TestOpposingLockOrdersDeadlockInPostgres(t *testing.T) {
	_, pool := newStore(t)
	lower, higher := orderedWallets(t, pool)
	ctx := context.Background()

	lockRow := func(tx pgx.Tx, walletID string) error {
		_, err := tx.Exec(ctx, `SELECT 1 FROM wallets WHERE id = $1 FOR UPDATE`, walletID)
		return err
	}

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin first transaction: %v", err)
	}
	defer func() { _ = first.Rollback(ctx) }()

	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin second transaction: %v", err)
	}
	defer func() { _ = second.Rollback(ctx) }()

	// Each takes one lock, in opposite order, before either takes its second.
	if err := lockRow(first, lower); err != nil {
		t.Fatalf("first transaction locking the lower id: %v", err)
	}
	if err := lockRow(second, higher); err != nil {
		t.Fatalf("second transaction locking the higher id: %v", err)
	}

	errs := make(chan error, 2)
	go func() { errs <- lockRow(first, higher) }()
	go func() { errs <- lockRow(second, lower) }()

	var deadlocked bool
	for i := 0; i < 2; i++ {
		var pgErr *pgconn.PgError
		if err := <-errs; errors.As(err, &pgErr) && pgErr.Code == "40P01" {
			deadlocked = true
		}
	}

	if !deadlocked {
		t.Fatal("opposing lock orders did not deadlock; the ordering guarantee is untested")
	}
}

// Proves the ordering is actually applied: asked for the higher id first,
// LockWalletsInOrder still reaches for the lower one, and blocks there.
//
// Observable without reading internals — while it is blocked on the lower
// wallet, the higher wallet is still free for anyone else to lock. Had it taken
// the wallets in the order given, the higher one would be held instead.
func TestLockWalletsInOrderTakesTheLowestIdFirst(t *testing.T) {
	store, pool := newStore(t)
	lower, higher := orderedWallets(t, pool)
	ctx := context.Background()

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()

	if _, err := holder.Exec(ctx,
		`SELECT 1 FROM wallets WHERE id = $1 FOR UPDATE`, lower); err != nil {
		t.Fatalf("hold the lower wallet: %v", err)
	}
	holderPID := backendPID(t, holder)

	// Deliberately names the higher wallet first.
	blocked := make(chan error, 1)
	go func() {
		blocked <- store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
			_, err := tx.LockWalletsInOrder(ctx, higher, lower)
			return err
		})
	}()

	waitForBlockerToBlockSomeone(t, pool, holderPID)

	// The higher wallet must still be free.
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin probe: %v", err)
	}
	defer func() { _ = probe.Rollback(ctx) }()

	if _, err := probe.Exec(ctx,
		`SELECT 1 FROM wallets WHERE id = $1 FOR UPDATE NOWAIT`, higher); err != nil {
		t.Fatalf("higher wallet was already locked, so the lower one was not taken first: %v", err)
	}

	_ = probe.Rollback(ctx)
	_ = holder.Rollback(ctx)
	<-blocked
}

// orderedWallets seeds two funded wallets and returns them sorted by id, since
// the lock order is defined by the ids rather than by the transfer direction.
func orderedWallets(t *testing.T, pool *pgxpool.Pool) (lower, higher string) {
	t.Helper()

	first := dbtest.Wallet(t, pool, 1000)
	second := dbtest.Wallet(t, pool, 1000)
	if second < first {
		return second, first
	}
	return first, second
}

// waitForBlockerToBlockSomeone waits until some session is blocked by the
// session holding blockerPID, so the assertion that follows runs at a known
// point rather than after a guessed sleep.
//
// It asks whether anyone is blocked *by our holder* rather than whether anyone
// in the database is blocked at all. The database is shared: `go test ./...`
// runs one process per package concurrently, and several tests manufacture lock
// waits of their own, so a broad question would be answered by unrelated
// contention and let this test probe before its own goroutine had blocked —
// passing for the wrong reason.
func waitForBlockerToBlockSomeone(t *testing.T, pool *pgxpool.Pool, blockerPID int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND $1 = ANY(pg_blocking_pids(pid))`, blockerPID).Scan(&waiting); err != nil {
			t.Fatalf("check for sessions blocked by %d: %v", blockerPID, err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("no session ever blocked on the lock held by backend %d", blockerPID)
}

// backendPID reports the PostgreSQL backend serving this transaction, so other
// sessions' waits can be attributed to it.
func backendPID(t *testing.T, tx pgx.Tx) int {
	t.Helper()

	var pid int
	if err := tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read backend pid: %v", err)
	}

	return pid
}

// Pessimistic locking's failure mode: a transfer held up by another
// transaction gives up in bounded time and says so, rather than hanging.
func TestLockContentionSurfacesAsWalletBusy(t *testing.T) {
	store, pool := newStore(t)
	wallet := dbtest.Wallet(t, pool, 1000)
	ctx := context.Background()

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()

	if _, err := holder.Exec(ctx,
		`SELECT 1 FROM wallets WHERE id = $1 FOR UPDATE`, wallet); err != nil {
		t.Fatalf("hold the row lock: %v", err)
	}

	start := time.Now()
	err = store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		_, err := tx.LockWallet(ctx, wallet)
		return err
	})
	waited := time.Since(start)

	if !errors.Is(err, domain.ErrWalletBusy) {
		t.Fatalf("got %v, want ErrWalletBusy", err)
	}
	if waited > 10*time.Second {
		t.Errorf("waited %v before giving up, want roughly the 3s lock timeout", waited)
	}
}

// TestConcurrentClaimsOfOneKeyBlockRatherThanRace covers the case the sequential
// duplicate test cannot reach: two requests carrying the same idempotency key
// where the second arrives while the first is still uncommitted.
//
// The distinction matters because the two cases succeed for different reasons. A
// duplicate arriving after the first commits finds a visible row, which any
// implementation would notice — including a check-then-insert, which is the
// design this one was chosen over. A duplicate arriving mid-flight finds nothing
// visible, and only ON CONFLICT DO NOTHING handles it, by waiting on the first
// transaction's outcome. Without this test a regression to check-then-insert
// would keep the suite green while letting two concurrent requests both insert.
func TestConcurrentClaimsOfOneKeyBlockRatherThanRace(t *testing.T) {
	store, pool := newStore(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	ctx := context.Background()

	first := pendingTransfer(from, to, 100)
	second := pendingTransfer(from, to, 100)
	second.IdempotencyKey = first.IdempotencyKey

	claimHeld := make(chan struct{})    // first has claimed but not committed
	releaseFirst := make(chan struct{}) // tells the first to commit
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
			_, won, err := tx.ClaimTransfer(ctx, first)

			// Signalled on every path, including failure. Closing this only on
			// success would leave the test blocked below rather than failing,
			// and a test that hangs is worse than one that fails.
			close(claimHeld)

			if err != nil {
				return err
			}
			if !won {
				return errors.New("the first request failed to claim a free key")
			}
			<-releaseFirst
			return nil // committing
		})
	}()

	select {
	case <-claimHeld:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never reached its claim")
	}

	type outcome struct {
		won      bool
		existing domain.Transfer
		at       time.Time
		err      error
	}
	secondDone := make(chan outcome, 1)

	go func() {
		var result outcome
		result.err = store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
			_, won, err := tx.ClaimTransfer(ctx, second)
			if err != nil {
				return err
			}
			result.won = won
			if !won {
				result.existing, err = tx.TransferByIdempotencyKey(ctx, second.IdempotencyKey)
			}
			return err
		})
		result.at = time.Now()
		secondDone <- result
	}()

	// While the first holds the key uncommitted, the second must wait. Anything
	// that returns here has decided without knowing the first's outcome.
	select {
	case result := <-secondDone:
		t.Fatalf("the second claim finished while the first was still open: won=%v err=%v",
			result.won, result.err)
	case <-time.After(300 * time.Millisecond):
	}

	released := time.Now()
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first request: %v", err)
	}

	result := <-secondDone
	if result.err != nil {
		t.Fatalf("second request: %v", result.err)
	}
	if !result.at.After(released) {
		t.Error("the second claim returned before the first committed")
	}
	if result.won {
		t.Fatal("both requests claimed the same idempotency key")
	}
	if result.existing.ID != first.ID {
		t.Errorf("the second request read transfer %s, want the first's %s",
			result.existing.ID, first.ID)
	}
	if got := dbtest.CountTransfers(t, pool, first.IdempotencyKey); got != 1 {
		t.Errorf("%d transfers exist for one key, want 1", got)
	}
}
