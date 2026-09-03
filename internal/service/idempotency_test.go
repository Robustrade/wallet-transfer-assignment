package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/dbtest"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func TestRepeatedRequestReplaysOriginalTransfer(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 500)
		f.wallet("bob", 0)

		first, err := f.transfer("key-1", "alice", "bob", 200)
		if err != nil {
			t.Fatalf("first transfer: %v", err)
		}

		for attempt := 2; attempt <= 4; attempt++ {
			again, err := f.transfer("key-1", "alice", "bob", 200)
			if err != nil {
				t.Fatalf("retry %d: %v", attempt, err)
			}
			if again.Transfer.ID != first.Transfer.ID {
				t.Fatalf("retry %d created transfer %s, want %s", attempt, again.Transfer.ID, first.Transfer.ID)
			}
			if !again.Replayed {
				t.Errorf("retry %d not reported as a replay", attempt)
			}
			if again.Transfer.Status != domain.StatusProcessed {
				t.Errorf("retry %d status = %s, want PROCESSED", attempt, again.Transfer.Status)
			}
		}

		if got := f.balance("alice"); got != 300 {
			t.Errorf("alice balance = %d, want 300: money moved more than once", got)
		}
		if got := f.balance("bob"); got != 200 {
			t.Errorf("bob balance = %d, want 200: money moved more than once", got)
		}
		if got := countRows(t, db, "transfers"); got != 1 {
			t.Errorf("transfers = %d, want 1", got)
		}
		if got := countRows(t, db, "ledger_entries"); got != 2 {
			t.Errorf("ledger entries = %d, want 2", got)
		}
	})
}

func TestReplayOutlivesTheProcessThatCreatedIt(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 500)
		f.wallet("bob", 0)

		first, err := f.transfer("key-1", "alice", "bob", 200)
		if err != nil {
			t.Fatalf("first transfer: %v", err)
		}

		restarted := service.New(repository.NewStore(db))
		replayed, err := restarted.CreateTransfer(context.Background(), domain.TransferRequest{
			IdempotencyKey: "key-1", FromWalletID: "alice", ToWalletID: "bob", Amount: 200,
		})
		if err != nil {
			t.Fatalf("replay after restart: %v", err)
		}
		if replayed.Transfer.ID != first.Transfer.ID || !replayed.Replayed {
			t.Fatalf("replay = %+v, want the original transfer %s", replayed.Transfer, first.Transfer.ID)
		}
		if got := f.balance("alice"); got != 300 {
			t.Errorf("alice balance = %d, want 300", got)
		}
	})
}

func TestKeyReusedWithDifferentPayloadIsRejected(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 500)
		f.wallet("bob", 0)
		f.wallet("carol", 0)

		if _, err := f.transfer("key-1", "alice", "bob", 100); err != nil {
			t.Fatalf("first transfer: %v", err)
		}

		cases := []struct {
			name string
			from string
			to   string
			amt  int64
		}{
			{"different amount", "alice", "bob", 250},
			{"different destination", "alice", "carol", 100},
			{"different source", "bob", "carol", 100},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := f.transfer("key-1", tc.from, tc.to, tc.amt)
				if !errors.Is(err, domain.ErrIdempotencyConflict) {
					t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
				}
			})
		}

		if got := f.balance("alice"); got != 400 {
			t.Errorf("alice balance = %d, want 400", got)
		}
		if got := countRows(t, db, "transfers"); got != 1 {
			t.Errorf("transfers = %d, want 1", got)
		}
	})
}

func TestFailedTransferIsReplayedAsFailed(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 10)
		f.wallet("bob", 0)

		first, err := f.transfer("key-1", "alice", "bob", 100)
		if err != nil {
			t.Fatalf("first transfer: %v", err)
		}

		if _, err := f.svc.CreateWallet(context.Background(), "funding", 0); err != nil {
			t.Fatalf("create funding wallet: %v", err)
		}
		if err := db.QueryRowContext(context.Background(),
			db.Rebind("UPDATE wallets SET balance = 1000 WHERE id = ?"), "alice").Err(); err != nil {
			t.Fatalf("top up alice: %v", err)
		}

		again, err := f.transfer("key-1", "alice", "bob", 100)
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if again.Transfer.ID != first.Transfer.ID {
			t.Fatalf("retry created a new transfer %s", again.Transfer.ID)
		}
		if again.Transfer.Status != domain.StatusFailed {
			t.Fatalf("retry status = %s, want FAILED", again.Transfer.Status)
		}
		if got := countRows(t, db, "ledger_entries"); got != 0 {
			t.Errorf("ledger entries = %d, want 0", got)
		}
	})
}

func TestConcurrentRequestsWithTheSameKeyTransferOnce(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 1000)
		f.wallet("bob", 0)

		const callers = 16
		results := make([]*service.TransferResult, callers)
		errs := make([]error, callers)

		var start sync.WaitGroup
		var done sync.WaitGroup
		start.Add(1)
		for i := range callers {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				results[i], errs[i] = f.transfer("key-1", "alice", "bob", 400)
			}()
		}
		start.Done()
		done.Wait()

		var transferID string
		for i, err := range errs {
			if err != nil {
				t.Fatalf("caller %d: %v", i, err)
			}
			if results[i].Transfer.Status != domain.StatusProcessed {
				t.Fatalf("caller %d status = %s, want PROCESSED", i, results[i].Transfer.Status)
			}
			if transferID == "" {
				transferID = results[i].Transfer.ID
			} else if results[i].Transfer.ID != transferID {
				t.Fatalf("caller %d settled transfer %s, but caller 0 settled %s",
					i, results[i].Transfer.ID, transferID)
			}
		}

		if got := countRows(t, db, "transfers"); got != 1 {
			t.Errorf("transfers = %d, want 1", got)
		}
		if got := countRows(t, db, "ledger_entries"); got != 2 {
			t.Errorf("ledger entries = %d, want 2", got)
		}
		if got := f.balance("alice"); got != 600 {
			t.Errorf("alice balance = %d, want 600: the transfer ran more than once", got)
		}
		if got := f.balance("bob"); got != 400 {
			t.Errorf("bob balance = %d, want 400", got)
		}
		assertLedgerConsistent(t, db, map[string]int64{"alice": 1000, "bob": 0})
	})
}

func TestInterruptedTransferIsCompletedByRetry(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 500)
		f.wallet("bob", 0)

		req := domain.TransferRequest{
			IdempotencyKey: "key-1", FromWalletID: "alice", ToWalletID: "bob", Amount: 200,
		}
		stranded := domain.NewTransfer(req, time.Now().UTC())
		err := f.store.WithTx(context.Background(), func(tx *repository.Tx) error {
			if err := tx.CreateTransfer(context.Background(), stranded); err != nil {
				return err
			}
			return tx.CreateIdempotencyRecord(context.Background(), &domain.IdempotencyRecord{
				Key:         req.IdempotencyKey,
				RequestHash: req.Hash(),
				TransferID:  stranded.ID,
				CreatedAt:   stranded.CreatedAt,
			})
		})
		if err != nil {
			t.Fatalf("stage interrupted transfer: %v", err)
		}

		result, err := f.svc.CreateTransfer(context.Background(), req)
		if err != nil {
			t.Fatalf("retry interrupted transfer: %v", err)
		}
		if result.Transfer.ID != stranded.ID {
			t.Fatalf("retry created transfer %s, want the stranded %s", result.Transfer.ID, stranded.ID)
		}
		if result.Transfer.Status != domain.StatusProcessed {
			t.Fatalf("status = %s, want PROCESSED", result.Transfer.Status)
		}
		if got := f.balance("alice"); got != 300 {
			t.Errorf("alice balance = %d, want 300", got)
		}
		assertLedgerConsistent(t, db, map[string]int64{"alice": 500, "bob": 0})
	})
}
