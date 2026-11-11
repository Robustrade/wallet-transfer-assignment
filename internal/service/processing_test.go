package service_test

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"
	"wallet-transfer-assignment/internal/appErrors"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

func TestProcess_MovesMoneyAndSavesTerminalResponse(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	result := f.mustProcess(id)
	if result.StatusCode != http.StatusCreated || result.Replayed {
		t.Fatalf("unexpected result: %+v", result)
	}
	f.wantBalance("wallet_1", 900)
	f.wantBalance("wallet_2", 600)
	f.wantState(id, domain.Processed)
	f.wantSnapshot(id, result)
}

func TestProcess_WritesOneMatchingDebitAndCredit(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	f.mustProcess(id)
	var entries []domain.LedgerEntry
	if err := f.db.WithContext(f.ctx).Where("transfer_id=?", id).Order("type DESC").Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(entries))
	}
	if entries[0].WalletID != "wallet_1" || entries[0].Type != domain.Debit || entries[0].Amount != 100 || entries[1].WalletID != "wallet_2" || entries[1].Type != domain.Credit || entries[1].Amount != 100 {
		t.Fatalf("unexpected ledger: %+v", entries)
	}
}

func TestProcess_CanSpendFullBalance(t *testing.T) {
	f := setup(t)
	f.mustProcess(f.pending("k1", 1000, "wallet_1", "wallet_2"))
	f.wantBalance("wallet_1", 0)
	f.wantBalance("wallet_2", 1500)
}

func TestProcess_InsufficientFundsSavesFailureWithoutMovingMoney(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 1001, "wallet_1", "wallet_2")
	result := f.mustProcess(id)
	if result.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", result.StatusCode)
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
	f.wantCount("ledger_entries", 0)
	f.wantState(id, domain.Failed)
	f.wantSnapshot(id, result)
}

func TestProcess_TerminalRetryReturnsSavedResponse(t *testing.T) {
	for _, amount := range []int64{100, 1001} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			f := setup(t)
			id := f.pending("k1", amount, "wallet_1", "wallet_2")
			first := f.mustProcess(id)
			second := f.mustProcess(id)
			if !second.Replayed || second.StatusCode != first.StatusCode || string(second.Body) != string(first.Body) {
				t.Fatal("terminal retry changed the saved result")
			}
			if amount == 100 {
				f.wantBalance("wallet_1", 900)
				f.wantCount("ledger_entries", 2)
			} else {
				f.wantBalance("wallet_1", 1000)
				f.wantCount("ledger_entries", 0)
			}
		})
	}
}

func TestProcess_DestinationOverflowLeavesTransferPending(t *testing.T) {
	f := setup(t)
	f.exec("UPDATE wallets SET balance=? WHERE id='wallet_2'", int64(math.MaxInt64))
	id := f.pending("k1", 1, "wallet_1", "wallet_2")
	result, err := f.svc.ProcessTransfer(f.ctx, id)
	if !errors.Is(err, appErrors.ErrBalanceOverflow) || result.StatusCode != 0 {
		t.Fatalf("expected overflow without a result, got %+v, %v", result, err)
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", math.MaxInt64)
	f.wantCount("ledger_entries", 0)
	f.wantPending(id)
}

func TestProcess_RollbackAfterDebitLeavesNoFinancialChanges(t *testing.T) {
	f := setup(t)
	f.exec(`
		CREATE FUNCTION reject_credit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.id='wallet_2' AND NEW.balance>OLD.balance THEN
				RAISE EXCEPTION 'injected credit failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_credit BEFORE UPDATE ON wallets
		FOR EACH ROW EXECUTE FUNCTION reject_credit();`)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	result, err := f.svc.ProcessTransfer(f.ctx, id)
	if err == nil || result.StatusCode != 0 {
		t.Fatal("injected failure must not return success")
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
	f.wantCount("ledger_entries", 0)
	f.wantPending(id)
	f.exec("DROP TRIGGER reject_credit ON wallets")
	f.mustProcess(id)
	f.wantBalance("wallet_1", 900)
}

// runParallel releases every caller together and reports failures on the test goroutine.
func runParallel(t *testing.T, ids []string, call func(string) (service.TransferResult, error)) []service.TransferResult {
	t.Helper()
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]service.TransferResult, len(ids))
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = call(id)
		}()
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

func TestProcess_ConcurrentDebitsCannotOverdraw(t *testing.T) {
	f := setup(t)
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = f.pending(fmt.Sprintf("k%d", i), 100, "wallet_1", "wallet_2")
	}
	results := runParallel(t, ids, func(id string) (service.TransferResult, error) { return f.svc.ProcessTransfer(f.ctx, id) })
	processed := 0
	for _, result := range results {
		if result.StatusCode == http.StatusCreated {
			processed++
		} else if result.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("unexpected status %d", result.StatusCode)
		}
	}
	if processed != 10 {
		t.Fatalf("processed = %d, want 10", processed)
	}
	f.wantBalance("wallet_1", 0)
	f.wantBalance("wallet_2", 1500)
	f.wantCount("ledger_entries", 20)
}

func TestProcess_ConcurrentCallsForSameTransferMoveMoneyOnce(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = id
	}
	results := runParallel(t, ids, func(id string) (service.TransferResult, error) {
		return f.svc.ProcessTransfer(f.ctx, id)
	})
	fresh := 0
	for _, result := range results {
		if result.StatusCode != http.StatusCreated || string(result.Body) != string(results[0].Body) {
			t.Fatal("concurrent callers must receive the same saved result")
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
	f.wantCount("ledger_entries", 2)
}

func TestProcess_OppositeDirectionsCompleteWithoutDeadlock(t *testing.T) {
	f := setup(t)
	ids := make([]string, 20)
	for i := range ids {
		from, to := "wallet_1", "wallet_2"
		if i%2 == 1 {
			from, to = to, from
		}
		ids[i] = f.pending(fmt.Sprintf("k%d", i), 10, from, to)
	}
	results := runParallel(t, ids, func(id string) (service.TransferResult, error) { return f.svc.ProcessTransfer(f.ctx, id) })
	for _, result := range results {
		if result.StatusCode != http.StatusCreated {
			t.Fatalf("unexpected status %d", result.StatusCode)
		}
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
}

func TestProcess_LockWaitIsBoundedAndRetryable(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	locker := f.db.WithContext(f.ctx).Begin()
	if locker.Error != nil {
		t.Fatal(locker.Error)
	}
	defer func() { _ = locker.Rollback().Error }()
	var wallet domain.Wallet
	if err := locker.Raw("SELECT * FROM wallets WHERE id='wallet_1' FOR UPDATE").Scan(&wallet).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewTransferService(f.db, 100*time.Millisecond, testMaxTransferAmount)
	_, err := svc.ProcessTransfer(f.ctx, id)
	if !errors.Is(err, appErrors.ErrTransferTemporarilyUnavailable) {
		t.Fatalf("expected retryable lock timeout, got %v", err)
	}
	f.wantPending(id)
	f.wantCount("ledger_entries", 0)
}

func TestProcess_CommitFailureNeverReturnsSuccess(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	f.exec(`
		CREATE FUNCTION reject_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected deferred failure';
		END;
		$$;
		CREATE CONSTRAINT TRIGGER reject_commit AFTER UPDATE ON transfers
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_commit();`)

	result, err := f.svc.ProcessTransfer(f.ctx, id)
	if !errors.Is(err, appErrors.ErrServiceUnavailable) || result.StatusCode != 0 {
		t.Fatalf("commit failure must return retryable error without success: %+v, %v", result, err)
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
	f.wantCount("ledger_entries", 0)
	f.wantPending(id)
}
