package service_test

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/repository"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/service"
)

// Behavioural integration tests against a real Postgres (Neon). They run only
// when DATABASE_URL is set (`make itest`); unit tests in this package run
// regardless. Each test uses unique ids and cleans up its own rows.

var itestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := strings.Trim(os.Getenv("DATABASE_URL"), `"'`)
	if dsn != "" {
		p, err := repository.NewPool(context.Background(), dsn)
		if err != nil {
			panic("connect itest pool: " + err.Error())
		}
		itestPool = p
	}
	code := m.Run()
	if itestPool != nil {
		itestPool.Close()
	}
	os.Exit(code)
}

func requireDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if itestPool == nil {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	return itestPool
}

func buildRealService(pool *pgxpool.Pool) *service.TransferService {
	return service.NewTransferService(
		repository.NewPgxTransactor(pool),
		repository.NewWalletRepository(),
		repository.NewTransferRepository(),
		repository.NewLedgerRepository(),
	)
}

var itestSeq atomic.Int64

func newID(prefix string) string {
	return prefix + "_" + time.Now().Format("150405.000000") + "_" +
		decimal.NewFromInt(itestSeq.Add(1)).String()
}

func seedWallet(t *testing.T, pool *pgxpool.Pool, balance string) string {
	t.Helper()
	id := newID("w")
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO wallets (id, name, balance) VALUES ($1, $2, $3)`,
		id, "itest", decimal.RequireFromString(balance)); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM ledger_entries WHERE wallet_id = $1`, id)
		_, _ = pool.Exec(ctx,
			`DELETE FROM transfers WHERE from_wallet_id = $1 OR to_wallet_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM wallets WHERE id = $1`, id)
	})
	return id
}

func walletBal(t *testing.T, pool *pgxpool.Pool, id string) decimal.Decimal {
	t.Helper()
	var b decimal.Decimal
	if err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, id).Scan(&b); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return b
}

func ledgerCount(t *testing.T, pool *pgxpool.Pool, transferID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, transferID).Scan(&n); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	return n
}

// signedLedgerSum returns SUM(CREDIT:+amount, DEBIT:-amount) for a wallet.
func signedLedgerSum(t *testing.T, pool *pgxpool.Pool, walletID string) decimal.Decimal {
	t.Helper()
	var s decimal.Decimal
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)
		 FROM ledger_entries WHERE wallet_id = $1`, walletID).Scan(&s)
	if err != nil {
		t.Fatalf("signed ledger sum: %v", err)
	}
	return s
}

func mustEqual(t *testing.T, got, want decimal.Decimal, label string) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", label, got, want)
	}
}

func TestIntegration_HappyPath(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")

	tr, created, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: newID("k"), FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("30.00"),
	})
	if err != nil || !created || tr.Status != domain.TransferProcessed {
		t.Fatalf("got tr=%+v created=%v err=%v, want PROCESSED/created", tr, created, err)
	}

	mustEqual(t, walletBal(t, pool, from), decimal.RequireFromString("70.00"), "from balance")
	mustEqual(t, walletBal(t, pool, to), decimal.RequireFromString("30.00"), "to balance")
	if n := ledgerCount(t, pool, tr.ID); n != 2 {
		t.Fatalf("ledger entries = %d, want exactly 2", n)
	}
	// Ledger <-> cached-balance invariant: signed sum equals net balance change.
	mustEqual(t, signedLedgerSum(t, pool, from), decimal.RequireFromString("-30.00"), "from signed ledger")
	mustEqual(t, signedLedgerSum(t, pool, to), decimal.RequireFromString("30.00"), "to signed ledger")
}

func TestIntegration_InsufficientFunds(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	from := seedWallet(t, pool, "10.00")
	to := seedWallet(t, pool, "0.00")

	tr, created, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: newID("k"), FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("50.00"),
	})
	if err != nil {
		t.Fatalf("insufficient funds must not error: %v", err)
	}
	if !created || tr.Status != domain.TransferFailed || tr.FailureReason != "insufficient funds" {
		t.Fatalf("got created=%v status=%s reason=%q, want created/FAILED/insufficient funds",
			created, tr.Status, tr.FailureReason)
	}
	mustEqual(t, walletBal(t, pool, from), decimal.RequireFromString("10.00"), "from balance")
	mustEqual(t, walletBal(t, pool, to), decimal.RequireFromString("0.00"), "to balance")
	if n := ledgerCount(t, pool, tr.ID); n != 0 {
		t.Fatalf("ledger entries = %d, want 0 on FAILED", n)
	}
}

func TestIntegration_IdempotentReplay(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")
	in := service.CreateTransferInput{
		IdempotencyKey: newID("k"), FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("40.00"),
	}

	first, c1, err := svc.CreateTransfer(context.Background(), in)
	if err != nil || !c1 {
		t.Fatalf("first: created=%v err=%v", c1, err)
	}
	second, c2, err := svc.CreateTransfer(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if c2 || second.ID != first.ID {
		t.Fatalf("replay: created=%v id=%s, want false / %s", c2, second.ID, first.ID)
	}
	// Side effects applied exactly once.
	mustEqual(t, walletBal(t, pool, from), decimal.RequireFromString("60.00"), "from balance")
	mustEqual(t, walletBal(t, pool, to), decimal.RequireFromString("40.00"), "to balance")
	if n := ledgerCount(t, pool, first.ID); n != 2 {
		t.Fatalf("ledger entries = %d, want 2 (once)", n)
	}
}

func TestIntegration_DuplicateAgainstPending(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	tr := repository.NewTransferRepository()
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")
	key := newID("k")

	// A PENDING transfer already exists for this key (Phase 1 committed,
	// Phase 2 not yet run).
	pending, err := tr.InsertPending(context.Background(), pool, domain.Transfer{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("25.00"),
	})
	if err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	got, created, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("25.00"),
	})
	if err != nil {
		t.Fatalf("duplicate against pending: %v", err)
	}
	if created || got.ID != pending.ID || got.Status != domain.TransferPending {
		t.Fatalf("got created=%v id=%s status=%s, want false / %s / PENDING",
			created, got.ID, got.Status, pending.ID)
	}
	// Phase 2 must not have run.
	mustEqual(t, walletBal(t, pool, from), decimal.RequireFromString("100.00"), "from balance")
	if n := ledgerCount(t, pool, pending.ID); n != 0 {
		t.Fatalf("ledger entries = %d, want 0 (Phase 2 not re-run)", n)
	}
}

func TestIntegration_LedgerBalanceInvariant_TwoTransfers(t *testing.T) {
	pool := requireDB(t)
	svc := buildRealService(pool)
	a := seedWallet(t, pool, "100.00")
	b := seedWallet(t, pool, "50.00")
	ctx := context.Background()

	for _, amt := range []string{"10.00", "5.50"} {
		if _, _, err := svc.CreateTransfer(ctx, service.CreateTransferInput{
			IdempotencyKey: newID("k"), FromWalletID: a, ToWalletID: b,
			Amount: decimal.RequireFromString(amt),
		}); err != nil {
			t.Fatalf("transfer %s: %v", amt, err)
		}
	}

	// a: 100 - 15.50 = 84.50 ; b: 50 + 15.50 = 65.50
	mustEqual(t, walletBal(t, pool, a), decimal.RequireFromString("84.50"), "a balance")
	mustEqual(t, walletBal(t, pool, b), decimal.RequireFromString("65.50"), "b balance")

	// Invariant: cached balance == initial + signed ledger sum.
	mustEqual(t, signedLedgerSum(t, pool, a),
		walletBal(t, pool, a).Sub(decimal.RequireFromString("100.00")), "a invariant")
	mustEqual(t, signedLedgerSum(t, pool, b),
		walletBal(t, pool, b).Sub(decimal.RequireFromString("50.00")), "b invariant")
}
