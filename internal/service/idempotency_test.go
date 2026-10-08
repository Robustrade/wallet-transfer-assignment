package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"wallet-transfer-assignment/internal/repository"
)

func TestRetryAfterDiscardedCommittedResult(t *testing.T) {
	m := fixture()
	s := testService(m)
	ctx := context.Background()
	// Deliberately discard the caller's response, as if delivery was lost.
	if _, err := s.Transfer(ctx, request()); err != nil {
		t.Fatal(err)
	}
	original := *m.records["key"].Result
	wallets := cloneMap(m.wallets)
	transfers := cloneMap(m.transfers)
	replay, err := testService(m).Transfer(ctx, request()) // A fresh service, no process cache.
	if err != nil || replay != original {
		t.Fatalf("original result not replayed: %+v %v", replay, err)
	}
	if !reflect.DeepEqual(wallets, m.wallets) || !reflect.DeepEqual(transfers, m.transfers) || len(m.ledger) != 2 || len(m.records) != 1 {
		t.Fatal("retry repeated financial side effects")
	}
}

func TestInvalidUTF8CannotAliasRequestFingerprint(t *testing.T) {
	for _, bad := range []string{string([]byte{0xff}), string([]byte{0xfe})} {
		m := fixture()
		req := request()
		req.FromWalletID = bad
		m.wallets[bad] = m.wallets["a"]
		_, err := testService(m).Transfer(context.Background(), req)
		if !errors.Is(err, ErrInvalidRequest) || m.begun != 0 {
			t.Fatalf("invalid UTF-8 must fail before claiming key: %v", err)
		}
	}
}

func TestIncompleteCommittedRecordDoesNotReexecute(t *testing.T) {
	m := fixture()
	s := testService(m)
	if _, err := s.Transfer(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	record := m.records["key"]
	record.Result = nil
	m.records["key"] = record
	before := cloneMap(m.wallets)
	result, err := s.Transfer(context.Background(), request())
	if !errors.Is(err, ErrIncompleteResult) || result != (repository.Result{}) || len(m.transfers) != 1 || len(m.ledger) != 2 || !reflect.DeepEqual(before, m.wallets) {
		t.Fatal("incomplete record permitted reexecution")
	}
	req := request()
	req.Amount++
	if _, err := s.Transfer(context.Background(), req); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("different payload must conflict even for incomplete record")
	}
}

func TestNULIdentifiersRejectedBeforeTransaction(t *testing.T) {
	for _, field := range []string{"source", "destination", "key"} {
		t.Run(field, func(t *testing.T) {
			m := fixture()
			req := request()
			switch field {
			case "source":
				req.FromWalletID = "a\x00"
			case "destination":
				req.ToWalletID = "b\x00"
			case "key":
				req.IdempotencyKey = "key\x00"
			}
			_, err := testService(m).Transfer(context.Background(), req)
			if !errors.Is(err, ErrInvalidRequest) || m.begun != 0 {
				t.Fatalf("invalid identifier reached PostgreSQL: %v", err)
			}
		})
	}
}

// Model a commit that persisted successfully but whose acknowledgement was lost.
// PostgreSQL/network behavior still requires integration testing; this verifies
// the service returns no tentative result and resolves the next attempt by key.
type lostCommitAcknowledgement struct{ repository.UnitOfWork }

func (w lostCommitAcknowledgement) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	if err := w.UnitOfWork.WithinTransaction(ctx, fn); err != nil {
		return err
	}
	return injected
}
func TestRetryAfterUnknownCommitOutcome(t *testing.T) {
	m := fixture()
	s := testService(m)
	s.work = lostCommitAcknowledgement{m}
	result, err := s.Transfer(context.Background(), request())
	if !errors.Is(err, injected) || result != (repository.Result{}) {
		t.Fatalf("commit error returned a claimed result: %+v %v", result, err)
	}
	durable := *m.records["key"].Result
	result, err = testService(m).Transfer(context.Background(), request())
	if err != nil || result != durable || len(m.transfers) != 1 || len(m.ledger) != 2 || m.wallets["a"].Balance != 70 || m.wallets["b"].Balance != 50 {
		t.Fatalf("unknown-commit retry repeated work: %+v %v", result, err)
	}
}
