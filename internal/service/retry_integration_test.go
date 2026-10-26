package service_test

import (
	"context"
	"errors"
	"testing"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/repository"
	"wallettransfer/internal/service"
)

// The test-only trigger raises PostgreSQL's actual SQLSTATE 40P01 once.
// The error is synthetic (not a real deadlock cycle); it verifies that the
// application retries the *entire* aborted transaction instead of resuming
// from an intermediate financial write.
func TestPostgresTransientTransactionRetries(t *testing.T) {
	t.Run("deadlock SQLSTATE retries once and commits once", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 100})
		const inject = `CREATE SEQUENCE transient_attempt_counter;
CREATE FUNCTION fail_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF nextval('transient_attempt_counter') = 1 THEN
        RAISE EXCEPTION 'test synthetic deadlock' USING ERRCODE = '40P01';
    END IF;
    RETURN NEW;
END; $$;
CREATE TRIGGER test_fail_once BEFORE INSERT ON transfers
FOR EACH ROW EXECUTE FUNCTION fail_once();`
		if _, err := db.Exec(inject); err != nil {
			t.Fatal(err)
		}
		req := domain.TransferRequest{IdempotencyKey: "deadlock-once", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		result, err := service.New(repository.NewPostgres(db)).Transfer(context.Background(), req)
		if err != nil || result.ID == "" || result.Status != domain.StatusProcessed {
			t.Fatalf("retry result=%+v error=%v", result, err)
		}
		var attempts int64
		if err := db.QueryRow("SELECT last_value FROM transient_attempt_counter").Scan(&attempts); err != nil || attempts != 2 {
			t.Fatalf("actual database insert attempts=%d err=%v; want 2", attempts, err)
		}
		assertStoredTransfer(t, db, result.ID, req, domain.StatusProcessed, "")
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 200)
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertLedger(t, db, result.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertReconciles(t, db)
	})

	t.Run("persistent serialization failures stop after bounded attempts", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 100})
		const inject = `CREATE SEQUENCE retry_attempt_counter;
CREATE FUNCTION always_fail_serialization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM nextval('retry_attempt_counter');
    RAISE EXCEPTION 'test synthetic serialization failure' USING ERRCODE = '40001';
END; $$;
CREATE TRIGGER test_always_fail BEFORE INSERT ON transfers
FOR EACH ROW EXECUTE FUNCTION always_fail_serialization();`
		if _, err := db.Exec(inject); err != nil {
			t.Fatal(err)
		}
		req := domain.TransferRequest{IdempotencyKey: "serialization-exhaustion", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		result, err := service.New(repository.NewPostgres(db)).Transfer(context.Background(), req)
		if !errors.Is(err, domain.ErrRetryableTransaction) || result != (domain.TransferResult{}) {
			t.Fatalf("outcome=%+v err=%v, want bounded retryable error", result, err)
		}
		var attempts int64
		if err := db.QueryRow("SELECT last_value FROM retry_attempt_counter").Scan(&attempts); err != nil || attempts != 3 {
			t.Fatalf("actual database attempts=%d err=%v; want exactly 3", attempts, err)
		}
		assertBalance(t, db, "A", 500)
		assertBalance(t, db, "B", 100)
		assertTransferKeyCount(t, db, req.IdempotencyKey, 0)
		assertTransferCount(t, db, 0)
		assertTotalLedgerCount(t, db, 0)
		assertReconciles(t, db)
	})
}
