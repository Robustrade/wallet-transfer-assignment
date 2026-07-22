package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/tests/mock"
)

func dec(v string) decimal.Decimal { return decimal.RequireFromString(v) }

func processingTransfer(amount string) domain.Transfer {
	return domain.Transfer{
		ID:           uuid.New(),
		FromWalletID: uuid.New(),
		ToWalletID:   uuid.New(),
		Amount:       dec(amount),
		State:        domain.StateProcessing,
		Attempts:     1,
	}
}

func activeWallet(bal string) domain.Wallet {
	return domain.Wallet{ID: uuid.New(), Balance: dec(bal), IsActive: true}
}

func newProcessor(store repository.Store, rec service.Recorder) *service.Processor {
	return service.NewProcessor(store, zap.NewNop(), 5, rec)
}

func TestProcessor_Happy(t *testing.T) {
	tr := processingTransfer("30")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return activeWallet("100"), activeWallet("0"), nil
		},
	}
	rec := &mock.Recorder{}
	store := &mock.Store{TxStore: tx}

	require.NoError(t, newProcessor(store, rec).Process(context.Background(), tr))
	require.Len(t, tx.ProcessedIDs, 1)
	require.Len(t, tx.Ledger, 2)
	require.Equal(t, 1, rec.ProcessedCount("processed"))
}

func TestProcessor_SkipsWhenNotProcessing(t *testing.T) {
	tr := processingTransfer("10")
	done := tr
	done.State = domain.StateProcessed
	tx := &mock.TxStore{GetFn: func(uuid.UUID) (domain.Transfer, error) { return done, nil }}
	rec := &mock.Recorder{}

	require.NoError(t, newProcessor(&mock.Store{TxStore: tx}, rec).Process(context.Background(), tr))
	require.Empty(t, tx.ProcessedIDs)
	require.Empty(t, tx.FailedReasons)
	require.Equal(t, 0, rec.ProcessedCount("processed"))
}

func TestProcessor_GetForUpdateError(t *testing.T) {
	tr := processingTransfer("10")
	tx := &mock.TxStore{GetFn: func(uuid.UUID) (domain.Transfer, error) { return domain.Transfer{}, errors.New("db down") }}
	err := newProcessor(&mock.Store{TxStore: tx}, &mock.Recorder{}).Process(context.Background(), tr)
	require.Error(t, err)
}

func TestProcessor_LockBusinessError_CommitsFailed(t *testing.T) {
	tr := processingTransfer("10")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
		},
	}
	rec := &mock.Recorder{}
	require.NoError(t, newProcessor(&mock.Store{TxStore: tx}, rec).Process(context.Background(), tr))
	require.Len(t, tx.FailedReasons, 1)
	require.Equal(t, 1, rec.ProcessedCount("failed"))
}

func TestProcessor_LockTransientError_Returns(t *testing.T) {
	tr := processingTransfer("10")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return domain.Wallet{}, domain.Wallet{}, errors.New("conn reset")
		},
	}
	require.Error(t, newProcessor(&mock.Store{TxStore: tx}, &mock.Recorder{}).Process(context.Background(), tr))
	require.Empty(t, tx.FailedReasons)
}

func TestProcessor_InsufficientFunds_CommitsFailed(t *testing.T) {
	tr := processingTransfer("80")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return activeWallet("50"), activeWallet("0"), nil
		},
	}
	rec := &mock.Recorder{}
	require.NoError(t, newProcessor(&mock.Store{TxStore: tx}, rec).Process(context.Background(), tr))
	require.Len(t, tx.FailedReasons, 1)
	require.Empty(t, tx.Ledger)
	require.Equal(t, 1, rec.ProcessedCount("failed"))
}

func TestProcessor_LedgerDuplicate_Finalizes(t *testing.T) {
	tr := processingTransfer("10")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return activeWallet("100"), activeWallet("0"), nil
		},
		InsertFn: func(domain.LedgerEntry) error { return repository.ErrLedgerDuplicate },
	}
	rec := &mock.Recorder{}
	require.NoError(t, newProcessor(&mock.Store{TxStore: tx}, rec).Process(context.Background(), tr))
	require.Len(t, tx.ProcessedIDs, 1)                   // finalized, not double-applied
	require.Empty(t, tx.Deltas)                          // no balance change on duplicate
	require.Equal(t, 1, rec.ProcessedCount("processed")) // still counted as processed
}

func TestProcessor_ApplyBalanceError_Returns(t *testing.T) {
	tr := processingTransfer("10")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return activeWallet("100"), activeWallet("0"), nil
		},
		ApplyFn: func(uuid.UUID, decimal.Decimal) error { return errors.New("write failed") },
	}
	require.Error(t, newProcessor(&mock.Store{TxStore: tx}, &mock.Recorder{}).Process(context.Background(), tr))
}

func TestProcessor_SetProcessedError_Returns(t *testing.T) {
	tr := processingTransfer("10")
	tx := &mock.TxStore{
		GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
		LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
			return activeWallet("100"), activeWallet("0"), nil
		},
		SetProcessedErr: repository.ErrConcurrentModification,
	}
	require.Error(t, newProcessor(&mock.Store{TxStore: tx}, &mock.Recorder{}).Process(context.Background(), tr))
}

func TestProcessor_DeadLetter(t *testing.T) {
	tr := processingTransfer("10")
	tr.Attempts = 99 // exceeds maxAttempts
	tx := &mock.TxStore{GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil }}
	rec := &mock.Recorder{}
	require.NoError(t, newProcessor(&mock.Store{TxStore: tx}, rec).Process(context.Background(), tr))
	require.Len(t, tx.FailedReasons, 1)
	require.Contains(t, tx.FailedReasons[0], "max processing attempts")
	require.Equal(t, 1, rec.ProcessedCount("failed"))
}

func TestProcessor_DeadLetter_AlreadyTerminalNoop(t *testing.T) {
	tr := processingTransfer("10")
	tr.Attempts = 99
	done := tr
	done.State = domain.StateProcessed
	tx := &mock.TxStore{GetFn: func(uuid.UUID) (domain.Transfer, error) { return done, nil }}
	require.NoError(t, newProcessor(&mock.Store{TxStore: tx}, &mock.Recorder{}).Process(context.Background(), tr))
	require.Empty(t, tx.FailedReasons) // nothing to fail; already terminal
}

func TestProcessor_DeadLetter_GetError(t *testing.T) {
	tr := processingTransfer("10")
	tr.Attempts = 99
	tx := &mock.TxStore{GetFn: func(uuid.UUID) (domain.Transfer, error) { return domain.Transfer{}, errors.New("db down") }}
	require.Error(t, newProcessor(&mock.Store{TxStore: tx}, &mock.Recorder{}).Process(context.Background(), tr))
}

func TestNopRecorder(t *testing.T) {
	var r service.NopRecorder
	r.TransferEnqueued()
	r.TransferProcessed("processed", 0)
}

func TestProcessor_RetriesTransientTxError(t *testing.T) {
	tr := processingTransfer("10")
	calls := 0
	store := &mock.Store{TxFn: func(ctx context.Context, fn func(repository.TxStore) error) error {
		calls++
		if calls < 2 {
			return &pgconn.PgError{Code: "40001"}
		}
		return fn(&mock.TxStore{
			GetFn: func(uuid.UUID) (domain.Transfer, error) { return tr, nil },
			LockFn: func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error) {
				return activeWallet("100"), activeWallet("0"), nil
			},
		})
	}}
	require.NoError(t, newProcessor(store, &mock.Recorder{}).Process(context.Background(), tr))
	require.Equal(t, 2, calls)
}
