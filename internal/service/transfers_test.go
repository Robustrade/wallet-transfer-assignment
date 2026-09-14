package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/dbtest"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/repository"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/service"
)

func newService(t *testing.T) (*service.Transfers, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	return service.NewTransfers(repository.New(pool), nil), pool
}

func key() string { return "key_" + uuid.NewString() }

func request(from, to string, amount int64) domain.TransferRequest {
	return domain.TransferRequest{
		IdempotencyKey: key(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         amount,
	}
}

func TestTransferMovesMoneyAndRecordsBothLedgerEntries(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 500)

	result, err := svc.Create(context.Background(), request(from, to, 300))
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if result.Transfer.State != domain.StateProcessed {
		t.Errorf("state is %s, want PROCESSED", result.Transfer.State)
	}
	if result.Replayed {
		t.Error("a first request was reported as a replay")
	}
	if result.Transfer.FailureReason != "" {
		t.Errorf("processed transfer carries reason %q", result.Transfer.FailureReason)
	}

	if got := dbtest.Balance(t, pool, from); got != 700 {
		t.Errorf("source holds %d, want 700", got)
	}
	if got := dbtest.Balance(t, pool, to); got != 800 {
		t.Errorf("destination holds %d, want 800", got)
	}

	if got := dbtest.CountLedgerEntries(t, pool, result.Transfer.ID); got != 2 {
		t.Errorf("transfer produced %d ledger entries, want exactly 2", got)
	}
	if got := dbtest.LedgerNet(t, pool, from); got != -300 {
		t.Errorf("source ledger nets to %d, want -300", got)
	}
	if got := dbtest.LedgerNet(t, pool, to); got != 300 {
		t.Errorf("destination ledger nets to %d, want 300", got)
	}
}

// The core guarantee: the same key twice moves money once.
func TestDuplicateRequestReturnsTheOriginalAndMovesNoMoreMoney(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)
	req := request(from, to, 300)

	first, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}

	second, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("duplicate request: %v", err)
	}

	if second.Transfer.ID != first.Transfer.ID {
		t.Errorf("duplicate produced transfer %s, want the original %s",
			second.Transfer.ID, first.Transfer.ID)
	}
	if !second.Replayed {
		t.Error("duplicate was not reported as a replay")
	}
	if first.Replayed {
		t.Error("first request was reported as a replay")
	}

	if got := dbtest.Balance(t, pool, from); got != 700 {
		t.Errorf("source holds %d after a duplicate, want 700", got)
	}
	if got := dbtest.Balance(t, pool, to); got != 300 {
		t.Errorf("destination holds %d after a duplicate, want 300", got)
	}
	if got := dbtest.CountTransfers(t, pool, req.IdempotencyKey); got != 1 {
		t.Errorf("%d transfers exist for one key, want 1", got)
	}
	if got := dbtest.CountLedgerEntries(t, pool, first.Transfer.ID); got != 2 {
		t.Errorf("%d ledger entries exist, want 2", got)
	}
}

func TestSameKeyWithDifferentParametersIsRefused(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	original := request(from, to, 300)
	if _, err := svc.Create(context.Background(), original); err != nil {
		t.Fatalf("first request: %v", err)
	}

	tests := map[string]func(*domain.TransferRequest){
		"different amount":      func(r *domain.TransferRequest) { r.Amount = 301 },
		"different destination": func(r *domain.TransferRequest) { r.ToWalletID = dbtest.Wallet(t, pool, 0) },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			conflicting := original
			mutate(&conflicting)

			_, err := svc.Create(context.Background(), conflicting)
			if !errors.Is(err, domain.ErrIdempotencyKeyConflict) {
				t.Fatalf("got %v, want ErrIdempotencyKeyConflict", err)
			}

			// The stored transfer must be untouched by the refusal.
			if got := dbtest.Balance(t, pool, from); got != 700 {
				t.Errorf("source holds %d, want 700", got)
			}
			if got := dbtest.CountTransfers(t, pool, original.IdempotencyKey); got != 1 {
				t.Errorf("%d transfers exist for the key, want 1", got)
			}
		})
	}
}

// A shortfall is recorded and committed, not rolled back, so the key keeps
// reporting the same answer.
func TestInsufficientFundsIsRecordedAndMovesNoMoney(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 100)
	to := dbtest.Wallet(t, pool, 0)
	req := request(from, to, 101)

	result, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if result.Transfer.State != domain.StateFailed {
		t.Fatalf("state is %s, want FAILED", result.Transfer.State)
	}
	if result.Transfer.FailureReason != domain.FailureReasonInsufficientFunds {
		t.Errorf("reason is %q, want %q",
			result.Transfer.FailureReason, domain.FailureReasonInsufficientFunds)
	}

	if got := dbtest.Balance(t, pool, from); got != 100 {
		t.Errorf("source holds %d, want 100 — no money should have moved", got)
	}
	if got := dbtest.Balance(t, pool, to); got != 0 {
		t.Errorf("destination holds %d, want 0", got)
	}
	if got := dbtest.CountLedgerEntries(t, pool, result.Transfer.ID); got != 0 {
		t.Errorf("a failed transfer wrote %d ledger entries, want 0", got)
	}

	// The failure is durable: replaying the key reports it again rather than
	// re-attempting the debit.
	replayed, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("replaying a failed transfer: %v", err)
	}
	if replayed.Transfer.ID != result.Transfer.ID {
		t.Error("replay produced a different transfer")
	}
	if replayed.Transfer.State != domain.StateFailed {
		t.Errorf("replayed state is %s, want FAILED", replayed.Transfer.State)
	}
	if !replayed.Replayed {
		t.Error("replay was not reported as one")
	}
}

// Topping the wallet up does not revive the key: the answer is bound to it.
func TestAFailedKeyKeepsFailingEvenAfterTheWalletIsFunded(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 100)
	to := dbtest.Wallet(t, pool, 0)
	req := request(from, to, 500)

	if _, err := svc.Create(context.Background(), req); err != nil {
		t.Fatalf("first request: %v", err)
	}

	if _, err := pool.Exec(context.Background(),
		`UPDATE wallets SET balance = 10000 WHERE id = $1`, from); err != nil {
		t.Fatalf("top up: %v", err)
	}

	result, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("retry after top up: %v", err)
	}

	if result.Transfer.State != domain.StateFailed {
		t.Fatalf("state is %s, want FAILED — the key is bound to its answer", result.Transfer.State)
	}
	if got := dbtest.Balance(t, pool, to); got != 0 {
		t.Errorf("destination holds %d, want 0 — the retry must not move money", got)
	}
}

func TestUnknownWalletIsReportedAndWritesNothing(t *testing.T) {
	svc, pool := newService(t)
	real := dbtest.Wallet(t, pool, 1000)

	tests := map[string]struct{ from, to, missing string }{
		"unknown source":      {from: "wallet_absent_source", to: real, missing: "wallet_absent_source"},
		"unknown destination": {from: real, to: "wallet_absent_dest", missing: "wallet_absent_dest"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := request(tc.from, tc.to, 100)

			_, err := svc.Create(context.Background(), req)
			if !errors.Is(err, domain.ErrWalletNotFound) {
				t.Fatalf("got %v, want ErrWalletNotFound", err)
			}

			var notFound *domain.WalletNotFoundError
			if !errors.As(err, &notFound) || notFound.WalletID != tc.missing {
				t.Fatalf("error did not name %s: %v", tc.missing, err)
			}

			// The transaction aborted, so the key was never claimed.
			if got := dbtest.CountTransfers(t, pool, req.IdempotencyKey); got != 0 {
				t.Errorf("%d transfers exist for a request that failed, want 0", got)
			}
			if got := dbtest.Balance(t, pool, real); got != 1000 {
				t.Errorf("wallet holds %d, want 1000", got)
			}
		})
	}
}

// A key left unclaimed by a foreign key violation is free for later use.
func TestAKeyRejectedForAnUnknownWalletCanBeUsedAgain(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	rejected := request("wallet_does_not_exist", to, 100)
	if _, err := svc.Create(context.Background(), rejected); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("got %v, want ErrWalletNotFound", err)
	}

	reused := request(from, to, 100)
	reused.IdempotencyKey = rejected.IdempotencyKey

	result, err := svc.Create(context.Background(), reused)
	if err != nil {
		t.Fatalf("reusing an unclaimed key: %v", err)
	}
	if result.Transfer.State != domain.StateProcessed {
		t.Errorf("state is %s, want PROCESSED", result.Transfer.State)
	}
	if result.Replayed {
		t.Error("reusing an unclaimed key was reported as a replay")
	}
}

// Documented consequence of claiming the key before locking: once a key is
// held, a replay never reaches the foreign key, so an unknown wallet surfaces
// as a conflict rather than as not-found.
func TestKnownKeyNamingAnUnknownWalletConflictsRatherThanNotFound(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	original := request(from, to, 100)
	if _, err := svc.Create(context.Background(), original); err != nil {
		t.Fatalf("first request: %v", err)
	}

	replayed := original
	replayed.ToWalletID = "wallet_does_not_exist"

	_, err := svc.Create(context.Background(), replayed)
	if !errors.Is(err, domain.ErrIdempotencyKeyConflict) {
		t.Fatalf("got %v, want ErrIdempotencyKeyConflict", err)
	}
	if errors.Is(err, domain.ErrWalletNotFound) {
		t.Error("reported as a missing wallet; the key conflict answers first")
	}
}

func TestValidationFailuresNeverReachTheDatabase(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 1000)
	to := dbtest.Wallet(t, pool, 0)

	tests := map[string]struct {
		mutate func(*domain.TransferRequest)
		want   error
	}{
		"no idempotency key": {func(r *domain.TransferRequest) { r.IdempotencyKey = "" }, domain.ErrMissingIdempotencyKey},
		"zero amount":        {func(r *domain.TransferRequest) { r.Amount = 0 }, domain.ErrNonPositiveAmount},
		"negative amount":    {func(r *domain.TransferRequest) { r.Amount = -5 }, domain.ErrNonPositiveAmount},
		"self transfer":      {func(r *domain.TransferRequest) { r.ToWalletID = r.FromWalletID }, domain.ErrSameWallet},
		"no source wallet":   {func(r *domain.TransferRequest) { r.FromWalletID = "" }, domain.ErrMissingFromWallet},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := request(from, to, 100)
			tc.mutate(&req)

			if _, err := svc.Create(context.Background(), req); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if got := dbtest.CountTransfers(t, pool, req.IdempotencyKey); got != 0 {
				t.Errorf("an invalid request wrote %d transfers, want 0", got)
			}
			if got := dbtest.Balance(t, pool, from); got != 1000 {
				t.Errorf("source holds %d, want 1000", got)
			}
		})
	}
}

// The double-spend test. Ten concurrent transfers of 10 against a wallet
// holding 50: exactly five may succeed, and the wallet must never go negative.
func TestConcurrentDebitsCannotDoubleSpend(t *testing.T) {
	svc, pool := newService(t)
	from := dbtest.Wallet(t, pool, 50)
	to := dbtest.Wallet(t, pool, 0)

	const attempts = 10
	var wg sync.WaitGroup
	results := make(chan service.Result, attempts)
	errs := make(chan error, attempts)
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := svc.Create(context.Background(), request(from, to, 10))
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("a concurrent transfer errored: %v", err)
	}

	var processed, failed int
	for result := range results {
		switch result.Transfer.State {
		case domain.StateProcessed:
			processed++
		case domain.StateFailed:
			failed++
		default:
			t.Fatalf("transfer settled as %s", result.Transfer.State)
		}
	}

	if processed != 5 {
		t.Errorf("%d transfers succeeded, want exactly 5 — the wallet held 50", processed)
	}
	if failed != attempts-5 {
		t.Errorf("%d transfers failed, want %d", failed, attempts-5)
	}

	sourceBalance := dbtest.Balance(t, pool, from)
	if sourceBalance != 0 {
		t.Errorf("source holds %d, want 0", sourceBalance)
	}
	if sourceBalance < 0 {
		t.Errorf("source went negative: %d", sourceBalance)
	}
	if got := dbtest.Balance(t, pool, to); got != 50 {
		t.Errorf("destination holds %d, want 50 — no money may be created", got)
	}
}

// The invariant the ledger exists to provide: stored balance is always the
// seeded balance plus everything the ledger records.
func TestStoredBalanceAlwaysEqualsTheLedger(t *testing.T) {
	svc, pool := newService(t)
	const seeded = 1000
	first := dbtest.Wallet(t, pool, seeded)
	second := dbtest.Wallet(t, pool, seeded)
	third := dbtest.Wallet(t, pool, seeded)

	moves := []struct {
		from, to string
		amount   int64
	}{
		{first, second, 100},
		{second, third, 250},
		{third, first, 75},
		{first, third, 900},   // succeeds
		{second, first, 5000}, // fails: insufficient
	}

	for _, move := range moves {
		if _, err := svc.Create(context.Background(), request(move.from, move.to, move.amount)); err != nil {
			t.Fatalf("transfer %s -> %s: %v", move.from, move.to, err)
		}
	}

	for _, wallet := range []string{first, second, third} {
		balance := dbtest.Balance(t, pool, wallet)
		net := dbtest.LedgerNet(t, pool, wallet)

		if balance != seeded+net {
			t.Errorf("wallet %s holds %d but the ledger says %d (%d seeded %+d)",
				wallet, balance, seeded+net, seeded, net)
		}
	}
}
