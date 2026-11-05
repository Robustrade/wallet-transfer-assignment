package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
)

type fixture struct {
	t   *testing.T
	db  *sql.DB
	svc *service.TransferService
}

// setup gives each test a clean database with wallet_1 = 1000 and wallet_2 = 500.
func setup(t *testing.T) fixture {
	conn := testutil.DB(t)
	testutil.SeedWallet(t, conn, "wallet_1", 1000)
	testutil.SeedWallet(t, conn, "wallet_2", 500)
	return fixture{t: t, db: conn, svc: service.NewTransferService(conn, nil)}
}

func cmd(key string, amount int64, from, to string) domain.TransferCommand {
	return domain.TransferCommand{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

func (f fixture) transfer(key string, amount int64) (service.TransferResult, error) {
	return f.svc.CreateTransfer(context.Background(), cmd(key, amount, "wallet_1", "wallet_2"))
}

func (f fixture) mustTransfer(key string, amount int64) service.TransferResult {
	f.t.Helper()
	res, err := f.transfer(key, amount)
	if err != nil {
		f.t.Fatalf("transfer %s: %v", key, err)
	}
	return res
}

func (f fixture) wantBalance(id string, want int64) {
	f.t.Helper()
	if got := testutil.Balance(f.t, f.db, id); got != want {
		f.t.Fatalf("balance of %s = %d, want %d", id, got, want)
	}
}

func (f fixture) wantCount(table string, want int) {
	f.t.Helper()
	if got := testutil.Count(f.t, f.db, table); got != want {
		f.t.Fatalf("rows in %s = %d, want %d", table, got, want)
	}
}

// ---- transfer execution ----------------------------------------------------------------

func TestTransfer_MovesMoneyAndMarksProcessed(t *testing.T) {
	f := setup(t)
	res := f.mustTransfer("k1", 100)

	if res.Replayed {
		t.Fatal("first call must not be a replay")
	}
	if res.Transfer.Status != domain.StatusProcessed || res.Transfer.FailureReason != nil {
		t.Fatalf("got status %s, reason %v", res.Transfer.Status, res.Transfer.FailureReason)
	}
	f.wantBalance("wallet_1", 900)
	f.wantBalance("wallet_2", 600)
}

func TestTransfer_WritesOneDebitAndOneCredit(t *testing.T) {
	f := setup(t)
	res := f.mustTransfer("k1", 100)

	rows, err := f.db.Query(`SELECT wallet_id, type, amount FROM ledger_entries
		WHERE transfer_id = $1 ORDER BY type DESC`, res.Transfer.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var wallet, typ string
		var amount int64
		if err := rows.Scan(&wallet, &typ, &amount); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s %s %d", wallet, typ, amount))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := "wallet_1 DEBIT 100,wallet_2 CREDIT 100"
	if strings.Join(got, ",") != want {
		t.Fatalf("ledger = %v, want %s", got, want)
	}
}

func TestTransfer_CanSpendFullBalance(t *testing.T) {
	f := setup(t)
	res := f.mustTransfer("k1", 1000)
	if res.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("status = %s", res.Transfer.Status)
	}
	f.wantBalance("wallet_1", 0)
}

// ---- failure scenarios ----------------------------------------------------------------

func TestTransfer_InsufficientFundsFailsAndChangesNothing(t *testing.T) {
	f := setup(t)
	res := f.mustTransfer("k1", 1001)

	if res.Transfer.Status != domain.StatusFailed ||
		res.Transfer.FailureReason == nil || *res.Transfer.FailureReason != domain.ReasonInsufficientFunds {
		t.Fatalf("got %+v", res.Transfer)
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
	f.wantCount("ledger_entries", 0)
}

func TestTransfer_UnknownWalletStoresNothingAndKeepsKeyFree(t *testing.T) {
	f := setup(t)
	_, err := f.svc.CreateTransfer(context.Background(), cmd("k1", 100, "wallet_1", "ghost"))
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("want ErrWalletNotFound, got %v", err)
	}
	f.wantCount("transfers", 0)
	f.wantCount("idempotency_records", 0)

	// The key was not used up.
	if res := f.mustTransfer("k1", 100); res.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("status = %s", res.Transfer.Status)
	}
}

func TestTransfer_InvalidInputTouchesNothing(t *testing.T) {
	f := setup(t)
	for _, c := range []domain.TransferCommand{
		cmd("k1", 100, "wallet_1", "wallet_1"),
		cmd("k1", -1, "wallet_1", "wallet_2"),
		cmd("", 100, "wallet_1", "wallet_2"),
	} {
		if _, err := f.svc.CreateTransfer(context.Background(), c); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("%+v: want ErrValidation, got %v", c, err)
		}
	}
	f.wantCount("transfers", 0)
}

// ---- idempotency ----------------------------------------------------------------------

func TestIdempotency_ReplayReturnsOriginalAndMovesMoneyOnce(t *testing.T) {
	f := setup(t)
	first := f.mustTransfer("k1", 100)
	second := f.mustTransfer("k1", 100)

	if !second.Replayed {
		t.Fatal("second call must be a replay")
	}
	if !sameTransfer(first.Transfer, second.Transfer) {
		t.Fatalf("replay differs:\n first=%+v\nsecond=%+v", first.Transfer, second.Transfer)
	}
	f.wantBalance("wallet_1", 900)
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestIdempotency_ReplaysFailedResultEvenAfterTopUp(t *testing.T) {
	f := setup(t)
	first := f.mustTransfer("k1", 5000)
	testutil.MustExec(t, f.db, `UPDATE wallets SET balance = 10000 WHERE id = 'wallet_1'`)

	second := f.mustTransfer("k1", 5000)

	if second.Transfer.Status != domain.StatusFailed || !sameTransfer(first.Transfer, second.Transfer) {
		t.Fatalf("want original FAILED result, got %+v", second.Transfer)
	}
	f.wantBalance("wallet_1", 10000)
}

func TestIdempotency_SameKeyDifferentRequestIsRejected(t *testing.T) {
	f := setup(t)
	f.mustTransfer("k1", 100)

	if _, err := f.transfer("k1", 200); !errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatalf("want ErrIdempotencyKeyReused, got %v", err)
	}
	f.wantBalance("wallet_1", 900)
}

func TestIdempotency_DifferentKeysAreDifferentTransfers(t *testing.T) {
	f := setup(t)
	f.mustTransfer("k1", 100)
	f.mustTransfer("k2", 100)
	f.wantBalance("wallet_1", 800)
}

func TestIdempotency_RetryFinishesTransferLeftPendingByCrash(t *testing.T) {
	f := setup(t)
	// Simulate a crash after Step A: key and PENDING transfer saved, money not moved.
	id := "11111111-1111-4111-8111-111111111111"
	c := cmd("k1", 100, "wallet_1", "wallet_2")
	testutil.MustExec(t, f.db, `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, 'wallet_1', 'wallet_2', 100, 'PENDING')`, id)
	testutil.MustExec(t, f.db, `INSERT INTO idempotency_records (idempotency_key, request_hash, transfer_id)
		VALUES ($1, $2, $3)`, "k1", service.HashTransferRequest(c), id)

	res, err := f.svc.CreateTransfer(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replayed || res.Transfer.ID != id || res.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("got %+v replayed=%v", res.Transfer, res.Replayed)
	}
	f.wantBalance("wallet_1", 900)
	f.wantCount("ledger_entries", 2)
}

// ---- concurrency ----------------------------------------------------------------------

// runParallel starts n calls at the same moment and collects their results.
func runParallel(t *testing.T, n int, call func(i int) (service.TransferResult, error)) []service.TransferResult {
	t.Helper()
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		results = make([]service.TransferResult, n)
		errs    = make([]error, n)
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = call(i)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	return results
}

func TestConcurrency_SameRequestInParallelIsOneTransfer(t *testing.T) {
	f := setup(t)
	results := runParallel(t, 20, func(int) (service.TransferResult, error) {
		return f.transfer("same-key", 100)
	})

	ids := map[string]bool{}
	fresh := 0
	for _, r := range results {
		ids[r.Transfer.ID] = true
		if !r.Replayed {
			fresh++
		}
		if r.Transfer.Status != domain.StatusProcessed {
			t.Fatalf("status = %s", r.Transfer.Status)
		}
	}
	if len(ids) != 1 || fresh != 1 {
		t.Fatalf("distinct transfers = %d, non-replayed = %d; want 1 and 1", len(ids), fresh)
	}
	f.wantBalance("wallet_1", 900)
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestConcurrency_ParallelDebitsNeverOverdraw(t *testing.T) {
	f := setup(t)
	// wallet_1 has 1000. 30 transfers of 100 race; exactly 10 can succeed.
	results := runParallel(t, 30, func(i int) (service.TransferResult, error) {
		return f.transfer(fmt.Sprintf("k-%d", i), 100)
	})

	processed, failed := 0, 0
	for _, r := range results {
		switch r.Transfer.Status {
		case domain.StatusProcessed:
			processed++
		case domain.StatusFailed:
			failed++
		}
	}
	if processed != 10 || failed != 20 {
		t.Fatalf("processed=%d failed=%d, want 10 and 20", processed, failed)
	}
	f.wantBalance("wallet_1", 0)
	f.wantBalance("wallet_2", 1500)
	f.wantCount("ledger_entries", 20)
}

func TestConcurrency_OppositeDirectionsDoNotDeadlock(t *testing.T) {
	f := setup(t)
	results := runParallel(t, 40, func(i int) (service.TransferResult, error) {
		from, to := "wallet_1", "wallet_2"
		if i%2 == 1 {
			from, to = to, from
		}
		return f.svc.CreateTransfer(context.Background(), cmd(fmt.Sprintf("k-%d", i), 10, from, to))
	})

	for _, r := range results {
		if r.Transfer.Status != domain.StatusProcessed {
			t.Fatalf("status = %s", r.Transfer.Status)
		}
	}
	total := testutil.Balance(t, f.db, "wallet_1") + testutil.Balance(t, f.db, "wallet_2")
	if total != 1500 {
		t.Fatalf("money not conserved: total = %d, want 1500", total)
	}
}

// ---- ledger invariants ----------------------------------------------------------------

func TestLedger_DebitsEqualCreditsAndBalancesMatchLedger(t *testing.T) {
	f := setup(t)
	opening := map[string]int64{"wallet_1": 1000, "wallet_2": 500}

	runParallel(t, 30, func(i int) (service.TransferResult, error) {
		if i%3 == 0 {
			return f.svc.CreateTransfer(context.Background(), cmd(fmt.Sprintf("x-%d", i), 70, "wallet_2", "wallet_1"))
		}
		return f.svc.CreateTransfer(context.Background(), cmd(fmt.Sprintf("x-%d", i), 90, "wallet_1", "wallet_2"))
	})

	var debits, credits int64
	if err := f.db.QueryRow(`SELECT
		coalesce(sum(amount) FILTER (WHERE type = 'DEBIT'), 0),
		coalesce(sum(amount) FILTER (WHERE type = 'CREDIT'), 0) FROM ledger_entries`,
	).Scan(&debits, &credits); err != nil {
		t.Fatal(err)
	}
	if debits != credits {
		t.Fatalf("debits %d != credits %d", debits, credits)
	}

	for id, start := range opening {
		var net int64
		if err := f.db.QueryRow(`SELECT coalesce(sum(CASE type WHEN 'CREDIT' THEN amount ELSE -amount END), 0)
			FROM ledger_entries WHERE wallet_id = $1`, id).Scan(&net); err != nil {
			t.Fatal(err)
		}
		f.wantBalance(id, start+net)
	}
}

func TestLedger_DatabaseRejectsUnpairedEntry(t *testing.T) {
	f := setup(t)
	res := f.mustTransfer("k1", 5000) // FAILED, so no ledger rows
	_, err := f.db.Exec(`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		VALUES ($1, 'wallet_1', 'DEBIT', 5000)`, res.Transfer.ID)
	if err == nil || !strings.Contains(err.Error(), "unbalanced ledger") {
		t.Fatalf("want unbalanced ledger error, got %v", err)
	}
}

func TestLedger_DatabaseRejectsEditsToEntries(t *testing.T) {
	f := setup(t)
	f.mustTransfer("k1", 100)
	for _, q := range []string{`UPDATE ledger_entries SET amount = 1`, `DELETE FROM ledger_entries`} {
		if _, err := f.db.Exec(q); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s: want append-only error, got %v", q, err)
		}
	}
}

func TestLedger_DatabaseRejectsNegativeBalance(t *testing.T) {
	f := setup(t)
	_, err := f.db.Exec(`UPDATE wallets SET balance = balance - 5000 WHERE id = 'wallet_1'`)
	if err == nil || !strings.Contains(err.Error(), "check constraint") {
		t.Fatalf("want check constraint error, got %v", err)
	}
}

func sameTransfer(a, b domain.Transfer) bool {
	sameReason := (a.FailureReason == nil) == (b.FailureReason == nil) &&
		(a.FailureReason == nil || *a.FailureReason == *b.FailureReason)
	return a.ID == b.ID && a.FromWalletID == b.FromWalletID && a.ToWalletID == b.ToWalletID &&
		a.Amount == b.Amount && a.Status == b.Status && sameReason &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt)
}
