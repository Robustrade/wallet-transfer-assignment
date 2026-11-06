package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// Atomicity proof: a transfer is all or nothing. The fault-injecting store below
// wraps the real Postgres store, lets the chosen write actually happen, and then
// simulates a crash right after it (an error, or a panic). Whatever step fails,
// no trace of the transfer may remain, and the same key must then work normally.

var errInjected = errors.New("injected fault")

type faultPoint string

const (
	afterClaim        faultPoint = "idempotency.Claim"
	afterCreate       faultPoint = "transfers.Create"
	afterDebit        faultPoint = "wallets.Debit"
	afterCredit       faultPoint = "wallets.Credit"
	afterLedgerAppend faultPoint = "ledger.Append"
	afterFinalize     faultPoint = "transfers.UpdateStatus"
)

type fault struct {
	at      faultPoint
	panics  bool
	tripped bool
}

// fire is called after the real write succeeded; it decides whether to "crash" here.
func (f *fault) fire(p faultPoint) error {
	if f.at != p {
		return nil
	}
	f.tripped = true
	if f.panics {
		panic("injected panic after " + string(p))
	}
	return errInjected
}

type faultyDB struct {
	service.Database
	fault *fault
}

func (d faultyDB) InTx(ctx context.Context, fn func(context.Context, service.Store) error) error {
	return d.Database.InTx(ctx, func(ctx context.Context, tx service.Store) error {
		return fn(ctx, faultyStore{Store: tx, fault: d.fault})
	})
}

type faultyStore struct {
	service.Store
	fault *fault
}

func (s faultyStore) Wallets() service.WalletRepository {
	return faultyWallets{s.Store.Wallets(), s.fault}
}
func (s faultyStore) Transfers() service.TransferRepository {
	return faultyTransfers{s.Store.Transfers(), s.fault}
}
func (s faultyStore) Ledger() service.LedgerRepository {
	return faultyLedger{s.Store.Ledger(), s.fault}
}
func (s faultyStore) Idempotency() service.IdempotencyRepository {
	return faultyIdempotency{s.Store.Idempotency(), s.fault}
}

type faultyWallets struct {
	service.WalletRepository
	fault *fault
}

func (w faultyWallets) Debit(ctx context.Context, id string, amount int64) error {
	if err := w.WalletRepository.Debit(ctx, id, amount); err != nil {
		return err
	}
	return w.fault.fire(afterDebit)
}

func (w faultyWallets) Credit(ctx context.Context, id string, amount int64) error {
	if err := w.WalletRepository.Credit(ctx, id, amount); err != nil {
		return err
	}
	return w.fault.fire(afterCredit)
}

type faultyTransfers struct {
	service.TransferRepository
	fault *fault
}

func (r faultyTransfers) Create(ctx context.Context, t domain.Transfer) error {
	if err := r.TransferRepository.Create(ctx, t); err != nil {
		return err
	}
	return r.fault.fire(afterCreate)
}

func (r faultyTransfers) UpdateStatus(ctx context.Context, t domain.Transfer, from domain.TransferStatus) error {
	if err := r.TransferRepository.UpdateStatus(ctx, t, from); err != nil {
		return err
	}
	return r.fault.fire(afterFinalize)
}

type faultyLedger struct {
	service.LedgerRepository
	fault *fault
}

func (l faultyLedger) Append(ctx context.Context, entries []domain.LedgerEntry) error {
	if err := l.LedgerRepository.Append(ctx, entries); err != nil {
		return err
	}
	return l.fault.fire(afterLedgerAppend)
}

type faultyIdempotency struct {
	service.IdempotencyRepository
	fault *fault
}

func (r faultyIdempotency) Claim(ctx context.Context, rec service.IdempotencyRecord) (bool, error) {
	claimed, err := r.IdempotencyRepository.Claim(ctx, rec)
	if err != nil {
		return claimed, err
	}
	return claimed, r.fault.fire(afterClaim)
}

func TestAtomicity_AFaultAfterAnyWriteLeavesNoTraceAndTheKeyStillWorks(t *testing.T) {
	tests := []struct {
		name       string
		at         faultPoint
		amount     int64 // the wallet holds 100: 40 is processed, 500 fails for funds
		wantStatus domain.TransferStatus
	}{
		{"processed: crash after claiming the key", afterClaim, 40, domain.StatusProcessed},
		{"processed: crash after inserting the transfer", afterCreate, 40, domain.StatusProcessed},
		{"processed: crash after the debit", afterDebit, 40, domain.StatusProcessed},
		{"processed: crash after the credit", afterCredit, 40, domain.StatusProcessed},
		{"processed: crash after the ledger pair", afterLedgerAppend, 40, domain.StatusProcessed},
		{"processed: crash after the final status update", afterFinalize, 40, domain.StatusProcessed},
		{"failed: crash after inserting the transfer", afterCreate, 500, domain.StatusFailed},
		{"failed: crash after the final status update", afterFinalize, 500, domain.StatusFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})
			ctx := context.Background()
			flt := &fault{at: tc.at}
			faulty := service.NewTransferService(faultyDB{
				Database: postgres.New(f.pool, postgres.Options{LockTimeout: 10 * time.Second}),
				fault:    flt,
			})
			req := request(newKey(), "wallet_1", "wallet_2", tc.amount)

			_, err := faulty.CreateTransfer(ctx, req)

			require.True(t, flt.tripped, "the fault point must be reached")
			require.ErrorIs(t, err, errInjected)
			f.requireNoTrace(t)

			// The client retries the same key once the system is healthy again.
			res, err := f.svc.CreateTransfer(ctx, req)
			require.NoError(t, err)
			require.False(t, res.Replayed, "the failed attempt must not have consumed the key")
			require.Equal(t, tc.wantStatus, res.Transfer.Status)
			f.requireLedgerConsistent(t)
		})
	}
}

func TestAtomicity_APanicMidTransferRollsBackEverything(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})
	flt := &fault{at: afterDebit, panics: true}
	faulty := service.NewTransferService(faultyDB{
		Database: postgres.New(f.pool, postgres.Options{LockTimeout: 10 * time.Second}),
		fault:    flt,
	})

	require.Panics(t, func() {
		_, _ = faulty.CreateTransfer(context.Background(), request(newKey(), "wallet_1", "wallet_2", 40))
	})

	require.True(t, flt.tripped)
	f.requireNoTrace(t)
}

// requireNoTrace asserts the database is exactly as it was before the attempt.
func (f *fixture) requireNoTrace(t *testing.T) {
	t.Helper()
	require.Zero(t, f.count(t, "transfers"), "no transfer row")
	require.Zero(t, f.count(t, "ledger_entries"), "no ledger entries")
	require.Zero(t, f.count(t, "idempotency_records"), "no idempotency claim")
	for id, opening := range f.opening {
		require.Equalf(t, opening, f.balance(t, id), "wallet %s balance unchanged", id)
	}
}
