package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/repository"
	"wallet-transfer-assignment/internal/feature/wallet/service"

	"github.com/jackc/pgx/v5/pgconn"
)

func newService(s *fakeStore) service.TransferService {
	return service.NewTransferService(
		fakeWalletRepo{s}, fakeTransferRepo{s}, fakeLedgerRepo{s}, fakeIdempotencyRepo{s}, fakeTxManager{s},
	)
}

func req(key string, from, to, amount int64) service.CreateTransferRequest {
	return service.CreateTransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

func totalBalance(s *fakeStore) (sum int64) {
	for _, b := range s.wallets {
		sum += b
	}
	return
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response is not JSON: %s", b)
	}
	return m
}

// ledgerIsBalanced asserts that every transfer that touched money has exactly
// one DEBIT and one CREDIT of the same amount, and failed transfers have none.
func assertLedgerConsistent(t *testing.T, s *fakeStore) {
	t.Helper()
	byTransfer := map[int64][]domain.LedgerEntry{}
	for _, e := range s.ledger {
		byTransfer[e.TransferID] = append(byTransfer[e.TransferID], e)
	}
	for id, tr := range s.transfers {
		entries := byTransfer[id]
		switch tr.Status {
		case domain.TransferProcessed:
			if len(entries) != 2 {
				t.Errorf("processed transfer %d has %d ledger entries, want 2", id, len(entries))
				continue
			}
			var debit, credit int64
			for _, e := range entries {
				if e.EntryType == domain.LedgerDebit {
					debit += e.Amount
				} else {
					credit += e.Amount
				}
			}
			if debit != credit || debit != tr.Amount {
				t.Errorf("transfer %d ledger unbalanced: debit=%d credit=%d amount=%d", id, debit, credit, tr.Amount)
			}
		default:
			if len(entries) != 0 {
				t.Errorf("transfer %d is %s but has %d ledger entries", id, tr.Status, len(entries))
			}
		}
	}
}

// ---- transfer execution --------------------------------------------------

func TestCreateTransfer_Success(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 50})

	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 300))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	body := decode(t, res.Body)
	if body["status"] != "PROCESSED" {
		t.Errorf("body status = %v, want PROCESSED", body["status"])
	}
	transferID := int64(body["transferId"].(float64))
	if transferID <= 0 {
		t.Fatalf("response must carry a real transfer id, got %d", transferID)
	}

	if s.wallets[1] != 700 || s.wallets[2] != 350 {
		t.Errorf("balances = %d/%d, want 700/350", s.wallets[1], s.wallets[2])
	}

	tr := s.transfers[transferID]
	if tr == nil || tr.Status != domain.TransferProcessed {
		t.Fatalf("transfer not persisted as PROCESSED: %+v", tr)
	}

	// Double entry: debit sender, credit receiver, both pointing at the saved transfer.
	if len(s.ledger) != 2 {
		t.Fatalf("ledger has %d entries, want 2", len(s.ledger))
	}
	debit, credit := s.ledger[0], s.ledger[1]
	if debit.EntryType != domain.LedgerDebit || debit.WalletID != 1 || debit.Amount != 300 || debit.TransferID != transferID {
		t.Errorf("bad debit entry: %+v", debit)
	}
	if credit.EntryType != domain.LedgerCredit || credit.WalletID != 2 || credit.Amount != 300 || credit.TransferID != transferID {
		t.Errorf("bad credit entry: %+v", credit)
	}

	rec := s.idem["key-12345"]
	if rec == nil || rec.ResponseStatus == nil || *rec.ResponseStatus != 201 {
		t.Fatalf("response not stored for replay: %+v", rec)
	}
	assertLedgerConsistent(t, s)
}

// Sender has the higher id, so the service locks in swapped order. The correct
// wallet must still be debited.
func TestCreateTransfer_SenderWithHigherIDIsDebited(t *testing.T) {
	s := newStore(map[int64]int64{1: 0, 2: 500})

	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 2, 1, 200))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if s.wallets[2] != 300 || s.wallets[1] != 200 {
		t.Fatalf("balances 1/2 = %d/%d, want 200/300", s.wallets[1], s.wallets[2])
	}
	if s.ledger[0].WalletID != 2 || s.ledger[0].EntryType != domain.LedgerDebit {
		t.Errorf("debit should hit wallet 2: %+v", s.ledger[0])
	}
}

func TestCreateTransfer_ExactBalanceSucceeds(t *testing.T) {
	s := newStore(map[int64]int64{1: 100, 2: 0})

	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if s.wallets[1] != 0 || s.wallets[2] != 100 {
		t.Fatalf("balances = %d/%d", s.wallets[1], s.wallets[2])
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	s := newStore(map[int64]int64{1: 50, 2: 0})

	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil {
		t.Fatalf("insufficient funds is a business outcome, not an error: %v", err)
	}
	if res.StatusCode != 409 {
		t.Fatalf("status = %d, want 409", res.StatusCode)
	}
	if s.wallets[1] != 50 || s.wallets[2] != 0 {
		t.Errorf("balances must not move: %d/%d", s.wallets[1], s.wallets[2])
	}
	if len(s.ledger) != 0 {
		t.Errorf("failed transfer must not write ledger entries, got %d", len(s.ledger))
	}

	if len(s.transfers) != 1 {
		t.Fatalf("expected the FAILED transfer to be recorded, got %d transfers", len(s.transfers))
	}
	for _, tr := range s.transfers {
		if tr.Status != domain.TransferFailed {
			t.Errorf("status = %s, want FAILED", tr.Status)
		}
		if tr.FailureReason == nil || *tr.FailureReason != "INSUFFICIENT_FUNDS" {
			t.Errorf("failure reason = %v", tr.FailureReason)
		}
	}
	assertLedgerConsistent(t, s)
}

func TestCreateTransfer_InvalidRequestsTouchNothing(t *testing.T) {
	tests := map[string]service.CreateTransferRequest{
		"zero amount":     req("key-12345", 1, 2, 0),
		"negative amount": req("key-12345", 1, 2, -1),
		"same wallet":     req("key-12345", 1, 1, 10),
		"empty key":       req("", 1, 2, 10),
		"key too short":   req("short12", 1, 2, 10),
		"key too long":    req(string(make([]byte, 65)), 1, 2, 10),
	}
	for name, r := range tests {
		t.Run(name, func(t *testing.T) {
			s := newStore(map[int64]int64{1: 100, 2: 0})
			_, err := newService(s).CreateTransfer(context.Background(), r)

			if !errors.Is(err, service.ErrInvalidTransferRequest) {
				t.Fatalf("got %v, want ErrInvalidTransferRequest", err)
			}
			if s.txAttempts != 0 || len(s.idem) != 0 || len(s.transfers) != 0 {
				t.Errorf("invalid request must not open a transaction or persist anything")
			}
		})
	}
}

func TestCreateTransfer_UnknownWalletRollsBackEverything(t *testing.T) {
	for name, r := range map[string]service.CreateTransferRequest{
		"unknown receiver": req("key-12345", 1, 99, 10),
		"unknown sender":   req("key-12345", 99, 1, 10),
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(map[int64]int64{1: 100})

			_, err := newService(s).CreateTransfer(context.Background(), r)
			if !errors.Is(err, domain.ErrWalletNotFound) {
				t.Fatalf("got %v, want ErrWalletNotFound", err)
			}
			if s.wallets[1] != 100 {
				t.Errorf("balance changed: %d", s.wallets[1])
			}
			// The idempotency key must NOT stay claimed, otherwise the client could
			// never retry once the wallet exists.
			if len(s.idem) != 0 || len(s.transfers) != 0 || len(s.ledger) != 0 {
				t.Errorf("state leaked after rollback: idem=%d transfers=%d ledger=%d", len(s.idem), len(s.transfers), len(s.ledger))
			}
		})
	}
}

func TestCreateTransfer_ReceiverBalanceOverflowIsRejected(t *testing.T) {
	s := newStore(map[int64]int64{1: 10, 2: math.MaxInt64})

	_, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 1))
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if s.wallets[1] != 10 || s.wallets[2] != math.MaxInt64 || len(s.ledger) != 0 {
		t.Errorf("state changed despite overflow rejection")
	}
}

// ---- idempotency ---------------------------------------------------------

func TestCreateTransfer_SameKeySameRequestReturnsOriginalResultWithoutSecondTransfer(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	svc := newService(s)

	first, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		replay, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		if replay.StatusCode != first.StatusCode || string(replay.Body) != string(first.Body) {
			t.Fatalf("replay %d differs: %d %s vs %d %s", i, replay.StatusCode, replay.Body, first.StatusCode, first.Body)
		}
	}

	if s.wallets[1] != 900 || s.wallets[2] != 100 {
		t.Errorf("money moved more than once: %d/%d", s.wallets[1], s.wallets[2])
	}
	if len(s.transfers) != 1 || len(s.ledger) != 2 {
		t.Errorf("duplicates created: transfers=%d ledger=%d", len(s.transfers), len(s.ledger))
	}
}

// A failed (409) outcome is also "the original result": replaying must return 409
// again, even if the wallet has been topped up in between.
func TestCreateTransfer_ReplayOfInsufficientFundsStaysRejected(t *testing.T) {
	s := newStore(map[int64]int64{1: 10, 2: 0})
	svc := newService(s)

	first, _ := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if first.StatusCode != 409 {
		t.Fatalf("setup: status = %d", first.StatusCode)
	}

	s.wallets[1] = 10_000 // sender is topped up afterwards

	replay, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil {
		t.Fatal(err)
	}
	if replay.StatusCode != 409 || string(replay.Body) != string(first.Body) {
		t.Fatalf("replay = %d %s, want original 409", replay.StatusCode, replay.Body)
	}
	if s.wallets[1] != 10_000 || len(s.ledger) != 0 {
		t.Errorf("replay must not execute the transfer")
	}
}

func TestCreateTransfer_SameKeyDifferentPayloadIsRejected(t *testing.T) {
	tests := map[string]service.CreateTransferRequest{
		"different amount":   req("key-12345", 1, 2, 999),
		"different receiver": req("key-12345", 1, 3, 100),
		"different sender":   req("key-12345", 3, 2, 100),
		"reversed direction": req("key-12345", 2, 1, 100),
	}
	for name, r := range tests {
		t.Run(name, func(t *testing.T) {
			s := newStore(map[int64]int64{1: 1000, 2: 1000, 3: 1000})
			svc := newService(s)
			if _, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100)); err != nil {
				t.Fatal(err)
			}
			before := totalBalance(s)

			_, err := svc.CreateTransfer(context.Background(), r)
			if !errors.Is(err, domain.ErrIdempotencyConflict) {
				t.Fatalf("got %v, want ErrIdempotencyConflict", err)
			}
			if s.wallets[1] != 900 || s.wallets[2] != 1100 || s.wallets[3] != 1000 {
				t.Errorf("conflicting request moved money: %v", s.wallets)
			}
			if totalBalance(s) != before || len(s.transfers) != 1 || len(s.ledger) != 2 {
				t.Errorf("conflicting request left side effects")
			}
		})
	}
}

// A record that was claimed but never completed must never be treated as a
// finished result nor trigger a second execution.
func TestCreateTransfer_ClaimedButIncompleteRecordIsNotReExecuted(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	hash, _ := domain.TransferFingerprint(1, 2, 100)
	s.idem["key-12345"] = &domain.IdempotencyRecord{ID: 1, IdempotencyKey: "key-12345", RequestHash: hash}

	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if !errors.Is(err, service.ErrRequestInProgress) {
		t.Fatalf("got res=%+v err=%v, want ErrRequestInProgress", res, err)
	}
	if s.wallets[1] != 1000 || len(s.transfers) != 0 || len(s.ledger) != 0 {
		t.Errorf("incomplete record caused a transfer")
	}
}

// Postgres JSONB re-formats stored JSON (adds spaces). The replay must still be
// byte-identical to the first response.
func TestCreateTransfer_ReplayIsByteIdenticalEvenIfStoreReformatsJSON(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	svc := newService(s)

	first, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil {
		t.Fatal(err)
	}

	// Simulate JSONB output formatting.
	rec := s.idem["key-12345"]
	rec.ResponseBody = []byte(`{"status": "PROCESSED", "transferId": ` + fmt.Sprint(decode(t, first.Body)["transferId"]) + `}`)

	replay, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil {
		t.Fatal(err)
	}
	if string(replay.Body) != string(first.Body) {
		t.Fatalf("replay %s != original %s", replay.Body, first.Body)
	}
}

// ---- atomicity / failure handling ----------------------------------------

func TestCreateTransfer_LedgerFailureRollsBackBalancesTransferAndKey(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	s.ledgerErr = errors.New("disk full")

	_, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err == nil {
		t.Fatal("expected error")
	}
	if s.wallets[1] != 1000 || s.wallets[2] != 0 {
		t.Errorf("balances changed despite failure: %v", s.wallets)
	}
	if len(s.transfers) != 0 || len(s.ledger) != 0 || len(s.idem) != 0 {
		t.Errorf("partial state persisted: transfers=%d ledger=%d idem=%d", len(s.transfers), len(s.ledger), len(s.idem))
	}

	// Client retries with the same key after the fault clears: must succeed once.
	s.ledgerErr = nil
	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("retry after failure: res=%+v err=%v", res, err)
	}
	if s.wallets[1] != 900 || s.wallets[2] != 100 {
		t.Errorf("retry balances = %v", s.wallets)
	}
}

// ---- retry behaviour -----------------------------------------------------

func TestCreateTransfer_RetriesOnDeadlockAndSerializationFailure(t *testing.T) {
	for name, code := range map[string]string{"deadlock": "40P01", "serialization failure": "40001"} {
		t.Run(name, func(t *testing.T) {
			s := newStore(map[int64]int64{1: 1000, 2: 0})
			s.lockErrs = []error{&pgconn.PgError{Code: code}}

			res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
			if err != nil || res.StatusCode != 201 {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if s.txAttempts != 2 {
				t.Errorf("attempts = %d, want 2", s.txAttempts)
			}
			// The aborted first attempt must leave no trace: exactly one debit.
			if s.wallets[1] != 900 || s.wallets[2] != 100 || len(s.transfers) != 1 || len(s.ledger) != 2 {
				t.Errorf("retry duplicated effects: wallets=%v transfers=%d ledger=%d", s.wallets, len(s.transfers), len(s.ledger))
			}
			assertLedgerConsistent(t, s)
		})
	}
}

func TestCreateTransfer_GivesUpAfterThreeRetryableFailures(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	dl := &pgconn.PgError{Code: "40P01"}
	s.lockErrs = []error{dl, dl, dl, dl, dl, dl}

	_, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40P01" {
		t.Errorf("underlying deadlock error should be preserved, got %v", err)
	}
	if s.txAttempts != 3 {
		t.Errorf("attempts = %d, want 3", s.txAttempts)
	}
	if s.wallets[1] != 1000 || len(s.idem) != 0 {
		t.Errorf("state leaked after giving up")
	}
}

func TestCreateTransfer_DoesNotRetryNonRetryableErrors(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	s.lockErrs = []error{&pgconn.PgError{Code: "23503"}} // FK violation: retrying cannot help

	_, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err == nil {
		t.Fatal("expected error")
	}
	if s.txAttempts != 1 {
		t.Errorf("attempts = %d, want 1", s.txAttempts)
	}
}

func TestCreateTransfer_StopsRetryingWhenContextCancelled(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	s.lockErrs = []error{&pgconn.PgError{Code: "40P01"}, &pgconn.PgError{Code: "40P01"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newService(s).CreateTransfer(ctx, req("key-12345", 1, 2, 100))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if s.txAttempts != 1 {
		t.Errorf("attempts = %d, want 1", s.txAttempts)
	}
}

// ---- uncertain commit ----------------------------------------------------

// Commit reported an error but the DB actually persisted it (e.g. connection
// dropped after COMMIT). The service must recover the saved result, not fail
// and not run the transfer again.
func TestCreateTransfer_UncertainCommitThatSucceededReturnsSavedResponse(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	s.commitErr, s.commitApplied = errors.New("connection reset"), true

	res, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil {
		t.Fatalf("should recover saved response, got %v", err)
	}
	if res.StatusCode != 201 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if s.wallets[1] != 900 || len(s.transfers) != 1 {
		t.Errorf("transfer executed more than once: %v transfers=%d", s.wallets, len(s.transfers))
	}
}

// Commit failed and nothing was persisted: report "uncertain" so the client
// retries with the same key, and that retry must then work exactly once.
func TestCreateTransfer_UncertainCommitThatFailedIsRetrySafe(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	s.commitErr, s.commitApplied = errors.New("connection reset"), false
	svc := newService(s)

	_, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if !errors.Is(err, repository.ErrCommitUncertain) {
		t.Fatalf("got %v, want ErrCommitUncertain", err)
	}
	if s.wallets[1] != 1000 {
		t.Fatalf("nothing should be persisted yet: %v", s.wallets)
	}

	res, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("retry: res=%+v err=%v", res, err)
	}
	if s.wallets[1] != 900 || s.wallets[2] != 100 || len(s.transfers) != 1 {
		t.Errorf("retry applied wrong: %v transfers=%d", s.wallets, len(s.transfers))
	}
}

// ---- concurrency (logic level; DB locking is covered by integration tests) -

func TestCreateTransfer_ConcurrentDuplicateKeysExecuteOnce(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	svc := newService(s)

	const n = 25
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
		}(i)
	}
	wg.Wait()

	for i := range results {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if results[i].StatusCode != 201 || string(results[i].Body) != string(results[0].Body) {
			t.Fatalf("request %d got %d %s, want identical replay", i, results[i].StatusCode, results[i].Body)
		}
	}
	if s.wallets[1] != 900 || s.wallets[2] != 100 || len(s.transfers) != 1 || len(s.ledger) != 2 {
		t.Errorf("duplicates executed: wallets=%v transfers=%d ledger=%d", s.wallets, len(s.transfers), len(s.ledger))
	}
}

func TestCreateTransfer_ConcurrentDebitsNeverOverspend(t *testing.T) {
	s := newStore(map[int64]int64{1: 500, 2: 0}) // enough for exactly 5 x 100
	svc := newService(s)

	const n = 20
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), req("key-"+string(rune('a'+i))+"-padding", 1, 2, 100))
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			statuses[i] = res.StatusCode
		}(i)
	}
	wg.Wait()

	var ok, rejected int
	for _, st := range statuses {
		switch st {
		case 201:
			ok++
		case 409:
			rejected++
		}
	}
	if ok != 5 || rejected != n-5 {
		t.Errorf("succeeded=%d rejected=%d, want 5/%d", ok, rejected, n-5)
	}
	if s.wallets[1] != 0 || s.wallets[2] != 500 {
		t.Errorf("balances = %v, want 0/500", s.wallets)
	}
	assertLedgerConsistent(t, s)
}

// ---- failure at every step of the transaction ----------------------------

// Whatever step fails, nothing may be left behind, and once the fault clears
// the same idempotency key must be usable and execute exactly once.
func TestCreateTransfer_FailureAtAnyStepRollsBackEverythingAndKeyStaysUsable(t *testing.T) {
	steps := []struct {
		fault   string
		balance int64 // sender balance: 50 makes the insufficient-funds path run
		amount  int64
	}{
		{"createTransfer", 1000, 100},
		{"debit", 1000, 100},
		{"credit", 1000, 100},
		{"updateStatus", 1000, 100}, // PROCESSED update
		{"saveResponse", 1000, 100},
		{"updateStatus", 50, 100}, // FAILED update on the insufficient-funds path
		{"saveResponse", 50, 100},
	}

	for _, step := range steps {
		name := fmt.Sprintf("%s with balance %d", step.fault, step.balance)
		t.Run(name, func(t *testing.T) {
			s := newStore(map[int64]int64{1: step.balance, 2: 0})
			injected := errors.New("injected " + step.fault)
			s.faults = map[string]error{step.fault: injected}
			svc := newService(s)

			_, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, step.amount))
			if !errors.Is(err, injected) {
				t.Fatalf("got %v, want the injected error", err)
			}
			if s.txAttempts != 1 {
				t.Errorf("attempts = %d: a non-retryable error must not be retried", s.txAttempts)
			}
			if s.wallets[1] != step.balance || s.wallets[2] != 0 {
				t.Errorf("balances changed: %v", s.wallets)
			}
			if len(s.transfers) != 0 || len(s.ledger) != 0 || len(s.idem) != 0 {
				t.Errorf("state leaked: transfers=%d ledger=%d idem=%d", len(s.transfers), len(s.ledger), len(s.idem))
			}

			s.faults = nil
			res, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, step.amount))
			if err != nil {
				t.Fatalf("retry after fault cleared: %v", err)
			}
			wantStatus := 201
			if step.balance < step.amount {
				wantStatus = 409
			}
			if res.StatusCode != wantStatus {
				t.Errorf("retry status = %d, want %d", res.StatusCode, wantStatus)
			}
			assertLedgerConsistent(t, s)
		})
	}
}

func TestCreateTransfer_SecondWalletLockFailureRollsBack(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	injected := errors.New("lock timeout")
	s.lockErrs = []error{nil, injected} // first wallet locks, second fails

	_, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if !errors.Is(err, injected) {
		t.Fatalf("got %v", err)
	}
	if len(s.idem) != 0 || len(s.transfers) != 0 || s.wallets[1] != 1000 {
		t.Errorf("state leaked after lock failure")
	}
}

// A transfer row without an idempotency record (for example legacy data) must
// make the request fail cleanly instead of creating a second transfer.
func TestCreateTransfer_ExistingTransferWithSameKeyIsNotDuplicated(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	s.transfers[1] = &domain.Transfer{ID: 1, IdempotencyKey: "key-12345", FromWalletID: 1, ToWalletID: 2, Amount: 100, Status: domain.TransferProcessed}

	_, err := newService(s).CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err == nil {
		t.Fatal("expected an error")
	}
	if s.wallets[1] != 1000 || len(s.transfers) != 1 || len(s.ledger) != 0 || len(s.idem) != 0 {
		t.Errorf("duplicate execution: wallets=%v transfers=%d", s.wallets, len(s.transfers))
	}
}

// If the stored body is not valid JSON the replay still returns it untouched.
func TestCreateTransfer_ReplayOfNonJSONStoredBodyReturnsItUnchanged(t *testing.T) {
	s := newStore(map[int64]int64{1: 1000, 2: 0})
	svc := newService(s)
	if _, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100)); err != nil {
		t.Fatal(err)
	}
	s.idem["key-12345"].ResponseBody = []byte("not json")

	res, err := svc.CreateTransfer(context.Background(), req("key-12345", 1, 2, 100))
	if err != nil || string(res.Body) != "not json" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
