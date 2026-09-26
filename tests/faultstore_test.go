package tests

import (
	"context"
	"fmt"

	"wallet/internal/domain"
	"wallet/internal/service"
	"wallet/internal/store/postgres"
)

// faultStore wraps the real PostgreSQL store to simulate infrastructure
// failures at exact points of the transfer transaction. It is the fault
// injection seam of the failure suite; production code knows nothing about it.
//
// A fault fires at most once ("one-shot"), so a test can watch the failure and
// then prove the identical request succeeds on retry through the same server.
// faultStore is not safe for concurrent use; every test using it is
// single-threaded.
type faultStore struct {
	*postgres.Store

	mode faultMode
	// method and err configure faultTxMethod: the first call to the named Tx
	// method fails with err.
	method string
	err    error

	fired bool
}

var _ service.Store = (*faultStore)(nil)

type faultMode int

const (
	// faultTxMethod fails one transaction step; everything else is real.
	faultTxMethod faultMode = iota
	// faultCommitLanded runs the whole transaction (COMMIT included) but still
	// reports ErrCommitOutcomeUnknown: the commit succeeded, its response was
	// lost on the way back.
	faultCommitLanded
	// faultNothingLanded reports ErrCommitOutcomeUnknown without touching the
	// database: the commit never reached the server.
	faultNothingLanded
)

// InTx implements service.Store. The first transaction of the store's life
// carries the fault; every later one runs untouched.
func (f *faultStore) InTx(ctx context.Context, fn func(ctx context.Context, tx service.Tx) error) error {
	if f.fired {
		return f.Store.InTx(ctx, fn)
	}
	switch f.mode {
	case faultCommitLanded:
		f.fired = true
		if err := f.Store.InTx(ctx, fn); err != nil {
			return err
		}
		return fmt.Errorf("%w: injected: the commit succeeded but its outcome was lost", domain.ErrCommitOutcomeUnknown)
	case faultNothingLanded:
		f.fired = true
		return fmt.Errorf("%w: injected: the commit never reached the server", domain.ErrCommitOutcomeUnknown)
	default:
		return f.Store.InTx(ctx, func(ctx context.Context, tx service.Tx) error {
			return fn(ctx, &faultTx{Tx: tx, store: f})
		})
	}
}

// faultTx fails the first call to one chosen Tx method.
type faultTx struct {
	service.Tx
	store *faultStore
}

var _ service.Tx = (*faultTx)(nil)

func (t *faultTx) inject(method string) error {
	if t.store.fired || t.store.method != method {
		return nil
	}
	t.store.fired = true
	return t.store.err
}

func (t *faultTx) DebitWallet(ctx context.Context, walletID string, amount int64) (bool, error) {
	if err := t.inject("DebitWallet"); err != nil {
		return false, err
	}
	return t.Tx.DebitWallet(ctx, walletID, amount)
}

func (t *faultTx) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	if err := t.inject("CreditWallet"); err != nil {
		return err
	}
	return t.Tx.CreditWallet(ctx, walletID, amount)
}

func (t *faultTx) InsertLedgerEntries(ctx context.Context, transferID, fromWalletID, toWalletID string, amount int64) error {
	if err := t.inject("InsertLedgerEntries"); err != nil {
		return err
	}
	return t.Tx.InsertLedgerEntries(ctx, transferID, fromWalletID, toWalletID, amount)
}

func (t *faultTx) MarkProcessed(ctx context.Context, transferID string) error {
	if err := t.inject("MarkProcessed"); err != nil {
		return err
	}
	return t.Tx.MarkProcessed(ctx, transferID)
}

func (t *faultTx) MarkFailed(ctx context.Context, transferID, reason string) error {
	if err := t.inject("MarkFailed"); err != nil {
		return err
	}
	return t.Tx.MarkFailed(ctx, transferID, reason)
}

func (t *faultTx) CompleteIdempotency(ctx context.Context, key string, transferID *string, status int, body []byte) error {
	if err := t.inject("CompleteIdempotency"); err != nil {
		return err
	}
	return t.Tx.CompleteIdempotency(ctx, key, transferID, status, body)
}

// cancelOnCommitStore simulates the client's disappearance at the worst
// moment: the transaction commits for real, then the request's context is
// cancelled before the commit's outcome can be observed. The service must
// still resolve the true outcome - the read-back may not depend on a context
// the client already killed.
type cancelOnCommitStore struct {
	*postgres.Store
	cancel context.CancelFunc
}

var _ service.Store = (*cancelOnCommitStore)(nil)

func (s *cancelOnCommitStore) InTx(ctx context.Context, fn func(ctx context.Context, tx service.Tx) error) error {
	if err := s.Store.InTx(ctx, fn); err != nil {
		return err
	}
	s.cancel()
	return fmt.Errorf("%w: injected: the commit succeeded, the client is gone", domain.ErrCommitOutcomeUnknown)
}
