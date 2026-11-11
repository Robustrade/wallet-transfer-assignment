package service_test

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/appErrors"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

func transferRequest(key string, amount int64) domain.TransferRequest {
	return domain.TransferRequest{IdempotencyKey: key, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: amount}
}

func (f fixture) mustCreate(key string, amount int64) service.TransferResult {
	f.t.Helper()
	result, err := f.svc.CreateTransfer(f.ctx, transferRequest(key, amount))
	if err != nil {
		f.t.Fatalf("create transfer: %v", err)
	}
	return result
}

func TestCreate_SequentialRetryReplaysOriginalResult(t *testing.T) {
	f := setup(t)
	first := f.mustCreate("k1", 100)
	second := f.mustCreate("k1", 100)
	if first.StatusCode != http.StatusCreated || first.Replayed || !second.Replayed || second.StatusCode != first.StatusCode || string(second.Body) != string(first.Body) {
		t.Fatal("duplicate must replay the original response")
	}
	f.wantBalance("wallet_1", 900)
	f.wantBalance("wallet_2", 600)
	f.wantCount("transfers", 1)
	f.wantCount("idempotency_records", 1)
	f.wantCount("ledger_entries", 2)
}

func TestCreate_ChangedPayloadConflictsBeforeWalletLookup(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*domain.TransferRequest)
	}{
		{"amount", func(r *domain.TransferRequest) { r.Amount = 200 }},
		{"source", func(r *domain.TransferRequest) { r.FromWalletID = "unknown_source" }},
		{"destination", func(r *domain.TransferRequest) { r.ToWalletID = "unknown_destination" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := setup(t)
			f.mustCreate("k1", 100)
			request := transferRequest("k1", 100)
			change.mutate(&request)
			if _, err := f.svc.CreateTransfer(f.ctx, request); !errors.Is(err, appErrors.ErrIdempotencyConflict) {
				t.Fatalf("want key conflict, got %v", err)
			}
			f.wantBalance("wallet_1", 900)
			f.wantCount("transfers", 1)
			f.wantCount("ledger_entries", 2)
		})
	}
}

func TestCreate_PendingKeyRejectsChangedPayload(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	if _, err := f.svc.CreateTransfer(f.ctx, transferRequest("k1", 200)); !errors.Is(err, appErrors.ErrIdempotencyConflict) {
		t.Fatalf("want pending key conflict, got %v", err)
	}
	f.wantPending(id)
	f.wantCount("ledger_entries", 0)
}

func TestCreate_FailedResultReplaysAfterFundingChanges(t *testing.T) {
	f := setup(t)
	first := f.mustCreate("k1", 1001)
	f.exec("UPDATE wallets SET balance=10000 WHERE id='wallet_1'")
	second := f.mustCreate("k1", 1001)
	if first.StatusCode != 422 || !second.Replayed || second.StatusCode != 422 || string(first.Body) != string(second.Body) {
		t.Fatal("failed result changed after funding")
	}
	f.wantBalance("wallet_1", 10000)
	f.wantCount("ledger_entries", 0)
}

func TestCreate_ConcurrentDuplicatesLeaveNoOrphanTransfers(t *testing.T) {
	f := setup(t)
	keys := make([]string, 20)
	for i := range keys {
		keys[i] = "same-key"
	}
	results := runParallel(t, keys, func(key string) (service.TransferResult, error) {
		return f.svc.CreateTransfer(f.ctx, transferRequest(key, 100))
	})
	fresh := 0
	for _, result := range results {
		if result.StatusCode != 201 || string(result.Body) != string(results[0].Body) {
			t.Fatal("duplicates returned different results")
		}
		if !result.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh results = %d, want 1", fresh)
	}
	f.wantBalance("wallet_1", 900)
	f.wantBalance("wallet_2", 600)
	f.wantCount("transfers", 1)
	f.wantCount("idempotency_records", 1)
	f.wantCount("ledger_entries", 2)
}

func TestCreate_ConcurrentDifferentPayloadsHaveOneWinner(t *testing.T) {
	f := setup(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = f.svc.CreateTransfer(f.ctx, transferRequest("same-key", int64(100+i*100)))
		}()
	}
	close(start)
	wg.Wait()
	winners, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, appErrors.ErrIdempotencyConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	var amount int64
	if err := f.db.WithContext(f.ctx).Raw("SELECT amount FROM transfers").Row().Scan(&amount); err != nil {
		t.Fatal(err)
	}
	f.wantBalance("wallet_1", 1000-amount)
	f.wantBalance("wallet_2", 500+amount)
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestCreate_MissingWalletLeavesKeyAvailable(t *testing.T) {
	f := setup(t)
	request := transferRequest("k1", 100)
	request.ToWalletID = "missing"
	if _, err := f.svc.CreateTransfer(f.ctx, request); !errors.Is(err, appErrors.ErrWalletNotFound) {
		t.Fatalf("want missing wallet, got %v", err)
	}
	f.wantCount("transfers", 0)
	f.wantCount("idempotency_records", 0)
	f.mustCreate("k1", 100)
	f.wantCount("transfers", 1)
}

func TestCreate_InvalidRequestsNeverReserveKeys(t *testing.T) {
	f := setup(t)
	for _, request := range []domain.TransferRequest{transferRequest("", 100), transferRequest("k1", 0), transferRequest("k2", testMaxTransferAmount+1)} {
		if _, err := f.svc.CreateTransfer(f.ctx, request); !errors.Is(err, appErrors.ErrInvalidTransfer) {
			t.Fatalf("want validation error, got %v", err)
		}
	}
	f.wantCount("transfers", 0)
	f.wantCount("idempotency_records", 0)
}

func TestCreate_NewPoolResumesPendingReservation(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	restarted := service.NewTransferService(openTestDB(t, f.ctx, f.dsn), 5*time.Second, testMaxTransferAmount)
	result, err := restarted.CreateTransfer(f.ctx, transferRequest("k1", 100))
	if err != nil || result.StatusCode != 201 {
		t.Fatalf("resume pending: %+v, %v", result, err)
	}
	f.wantState(id, domain.Processed)
	f.wantSnapshot(id, result)
	f.wantCount("transfers", 1)
	f.wantBalance("wallet_1", 900)
}

func TestCreate_NewPoolReplaysCommittedResponse(t *testing.T) {
	f := setup(t)
	first := f.mustCreate("k1", 100)
	restarted := service.NewTransferService(openTestDB(t, f.ctx, f.dsn), 5*time.Second, testMaxTransferAmount)
	second, err := restarted.CreateTransfer(f.ctx, transferRequest("k1", 100))
	if err != nil || !second.Replayed || second.StatusCode != first.StatusCode || string(second.Body) != string(first.Body) {
		t.Fatalf("restart replay: %+v, %v", second, err)
	}
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestCreate_TransientFailureKeepsReservationForRetry(t *testing.T) {
	f := setup(t)
	f.exec(`CREATE FUNCTION reject_processing_commit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected commit failure'; END; $$;
CREATE CONSTRAINT TRIGGER reject_processing_commit AFTER UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_processing_commit();`)
	result, err := f.svc.CreateTransfer(f.ctx, transferRequest("k1", 100))
	if !errors.Is(err, appErrors.ErrServiceUnavailable) || result.StatusCode != 0 {
		t.Fatalf("want retryable unknown result, got %+v, %v", result, err)
	}
	var id string
	if err := f.db.WithContext(f.ctx).Raw("SELECT id FROM transfers").Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.wantPending(id)
	f.wantCount("ledger_entries", 0)
	f.wantBalance("wallet_1", 1000)
	f.exec("DROP TRIGGER reject_processing_commit ON transfers")
	f.mustCreate("k1", 100)
	f.wantState(id, domain.Processed)
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestCreate_DifferentKeysAuthorizeDifferentTransfers(t *testing.T) {
	f := setup(t)
	for i := range 2 {
		f.mustCreate(fmt.Sprintf("key-%d", i), 100)
	}
	f.wantBalance("wallet_1", 800)
	f.wantCount("transfers", 2)
	f.wantCount("ledger_entries", 4)
}
