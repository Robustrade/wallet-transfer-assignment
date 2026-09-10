package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/mocks"
	"wallet-transfer-assignment/internal/service"
)

type transferFixture struct {
	svc      *service.TransferService
	txMgr    *mocks.MockTransactor
	wallet   *mocks.MockWalletRepository
	transfer *mocks.MockTransferRepository
	ledger   *mocks.MockLedgerRepository
	idemp    *mocks.MockIdempotencyRepository
}

func newTransferFixture(t *testing.T) *transferFixture {
	ctrl := gomock.NewController(t)
	f := &transferFixture{
		txMgr:    mocks.NewMockTransactor(ctrl),
		wallet:   mocks.NewMockWalletRepository(ctrl),
		transfer: mocks.NewMockTransferRepository(ctrl),
		ledger:   mocks.NewMockLedgerRepository(ctrl),
		idemp:    mocks.NewMockIdempotencyRepository(ctrl),
	}
	f.svc = service.NewTransferService(f.txMgr, f.wallet, f.transfer, f.ledger, f.idemp)
	return f
}

func TestTransferRequest_Validation(t *testing.T) {
	tests := []struct {
		name        string
		req         domain.CreateTransferRequest
		expectedErr error
	}{
		{
			name:        "empty idempotency key",
			req:         domain.CreateTransferRequest{IdempotencyKey: "", FromWalletID: "w1", ToWalletID: "w2", Amount: 100},
			expectedErr: domain.ErrEmptyIdempotencyKey,
		},
		{
			name:        "empty sender wallet",
			req:         domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "", ToWalletID: "w2", Amount: 100},
			expectedErr: domain.ErrEmptyWalletID,
		},
		{
			name:        "empty receiver wallet",
			req:         domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "", Amount: 100},
			expectedErr: domain.ErrEmptyWalletID,
		},
		{
			name:        "self transfer",
			req:         domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w1", Amount: 100},
			expectedErr: domain.ErrSameWalletTransfer,
		},
		{
			name:        "zero amount",
			req:         domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 0},
			expectedErr: domain.ErrInvalidAmount,
		},
		{
			name:        "negative amount",
			req:         domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: -50},
			expectedErr: domain.ErrInvalidAmount,
		},
		{
			name:        "valid request",
			req:         domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 100},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTransferService_SuccessfulTransfer(t *testing.T) {
	f := newTransferFixture(t)

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_success").
		Return(nil, nil)

	f.idemp.EXPECT().
		CreateIdempotencyRecord(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rec domain.IdempotencyRecord) (*domain.IdempotencyRecord, error) {
			assert.Equal(t, "key_success", rec.Key)
			assert.Equal(t, 200, rec.ResponseCode)
			assert.NotEmpty(t, rec.RequestHash)
			assert.NotEmpty(t, rec.ResponseBody)
			return &rec, nil
		})

	// Transparently execute inside transaction callback
	f.txMgr.EXPECT().
		WithinTransaction(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	// Postgres returns wallets ordered by id ASC ("w_receiver" before "w_sender")
	f.wallet.EXPECT().
		GetWalletsForUpdate(gomock.Any(), "w_sender", "w_receiver").
		Return([]domain.Wallet{
			{ID: "w_receiver", Balance: 200},
			{ID: "w_sender", Balance: 1000},
		}, nil)

	// Assert sender balance updated from 1000 -> 700
	f.wallet.EXPECT().
		UpdateWalletBalance(gomock.Any(), "w_sender", int64(700)).
		Return(nil)

	// Assert receiver balance updated from 200 -> 500
	f.wallet.EXPECT().
		UpdateWalletBalance(gomock.Any(), "w_receiver", int64(500)).
		Return(nil)

	// Assert transfer record created with PROCESSED status
	f.transfer.EXPECT().
		CreateTransfer(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, tr domain.Transfer) (*domain.Transfer, error) {
			tr.ID = "tr_generated_123"
			tr.CreatedAt = time.Now()
			tr.UpdatedAt = time.Now()
			return &tr, nil
		})

	// Assert double-entry ledger debit & credit entries created
	f.ledger.EXPECT().
		CreateLedgerEntry(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, e domain.LedgerEntry) (*domain.LedgerEntry, error) {
			if e.Type != domain.LedgerEntryDebit || e.WalletID != "w_sender" || e.Amount != 300 {
				return nil, errors.New("unexpected debit entry")
			}
			return &e, nil
		})

	f.ledger.EXPECT().
		CreateLedgerEntry(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, e domain.LedgerEntry) (*domain.LedgerEntry, error) {
			if e.Type != domain.LedgerEntryCredit || e.WalletID != "w_receiver" || e.Amount != 300 {
				return nil, errors.New("unexpected credit entry")
			}
			return &e, nil
		})

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_success",
		FromWalletID:   "w_sender",
		ToWalletID:     "w_receiver",
		Amount:         300,
	}

	result, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsReplay)
	assert.Equal(t, domain.StatusProcessed, result.Transfer.Status)
	assert.Equal(t, int64(300), result.Transfer.Amount)
}

func TestTransferService_InsufficientFunds(t *testing.T) {
	f := newTransferFixture(t)

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_insuf").
		Return(nil, nil)

	f.txMgr.EXPECT().
		WithinTransaction(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	// Sender has only 200 paise, transfer requires 500 paise
	f.wallet.EXPECT().
		GetWalletsForUpdate(gomock.Any(), "w1", "w2").
		Return([]domain.Wallet{
			{ID: "w1", Balance: 200},
			{ID: "w2", Balance: 500},
		}, nil)

	// Assert transfer record created with FAILED status and error reason
	f.transfer.EXPECT().
		CreateTransfer(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, tr domain.Transfer) (*domain.Transfer, error) {
			assert.Equal(t, domain.StatusFailed, tr.Status)
			require.NotNil(t, tr.ErrorReason)
			assert.Equal(t, domain.ErrReasonInsufficientFunds, *tr.ErrorReason)
			assert.Equal(t, "key_insuf", tr.IdempotencyKey)
			assert.Equal(t, "w1", tr.FromWalletID)
			assert.Equal(t, "w2", tr.ToWalletID)
			assert.Equal(t, int64(500), tr.Amount)
			return &tr, nil
		})

	f.idemp.EXPECT().
		CreateIdempotencyRecord(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, rec domain.IdempotencyRecord) (*domain.IdempotencyRecord, error) {
			assert.Equal(t, "key_insuf", rec.Key)
			assert.Equal(t, 422, rec.ResponseCode)
			assert.Contains(t, rec.ResponseBody, domain.ErrReasonInsufficientFunds)
			return &rec, nil
		})

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_insuf",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}

	_, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.ErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestTransferService_InsufficientFunds_IdempotentReplay(t *testing.T) {
	f := newTransferFixture(t)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_insuf_replay",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}

	cachedBody := `{"error":"INSUFFICIENT_FUNDS","message":"insufficient wallet balance for transfer"}`

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_insuf_replay").
		Return(&domain.IdempotencyRecord{
			Key:          "key_insuf_replay",
			RequestHash:  req.CanonicalHash(),
			ResponseCode: 422,
			ResponseBody: cachedBody,
		}, nil)

	// WithinTransaction, wallet, and transfer should NOT be called!
	result, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsReplay)
	assert.Equal(t, 422, result.ResponseCode)
	assert.Equal(t, cachedBody, string(result.ResponseBody))
	assert.Nil(t, result.Transfer)
}

func TestTransferService_InsufficientFunds_RecordFailureError(t *testing.T) {
	f := newTransferFixture(t)

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_insuf").
		Return(nil, nil)

	f.txMgr.EXPECT().
		WithinTransaction(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	f.wallet.EXPECT().
		GetWalletsForUpdate(gomock.Any(), "w1", "w2").
		Return([]domain.Wallet{
			{ID: "w1", Balance: 200},
			{ID: "w2", Balance: 500},
		}, nil)

	dbErr := errors.New("db disk full")
	f.transfer.EXPECT().
		CreateTransfer(gomock.Any(), gomock.Any()).
		Return(nil, dbErr)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_insuf",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}

	_, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.ErrorIs(t, err, dbErr)
}

func TestTransferService_WalletNotFound(t *testing.T) {
	f := newTransferFixture(t)

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_missing").
		Return(nil, nil)

	f.txMgr.EXPECT().
		WithinTransaction(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	// Query returns only 1 wallet instead of 2
	f.wallet.EXPECT().
		GetWalletsForUpdate(gomock.Any(), "w1", "w2").
		Return([]domain.Wallet{{ID: "w1", Balance: 1000}}, nil)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_missing",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	_, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.ErrorIs(t, err, domain.ErrWalletNotFound)
}

func TestTransferService_DatabaseErrorPropagation(t *testing.T) {
	f := newTransferFixture(t)

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_err").
		Return(nil, nil)

	dbErr := errors.New("connection reset by peer")
	f.txMgr.EXPECT().
		WithinTransaction(gomock.Any(), gomock.Any()).
		Return(dbErr)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_err",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	_, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.ErrorIs(t, err, dbErr)
}

func TestTransferService_IdempotentReplay(t *testing.T) {
	f := newTransferFixture(t)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_replay",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         250,
	}

	cachedBody := `{"id":"tr_cached_999","idempotencyKey":"key_replay","fromWalletId":"w1","toWalletId":"w2","amount":250,"status":"PROCESSED","createdAt":"2026-09-09T03:00:00Z","updatedAt":"2026-09-09T03:00:00Z"}`

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_replay").
		Return(&domain.IdempotencyRecord{
			Key:          "key_replay",
			RequestHash:  req.CanonicalHash(),
			ResponseCode: 201,
			ResponseBody: cachedBody,
		}, nil)

	// WithinTransaction, wallet, transfer, and ledger should NOT be called!
	result, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsReplay)
	assert.Equal(t, 201, result.ResponseCode)
	require.NotNil(t, result.Transfer)
	assert.Equal(t, "tr_cached_999", result.Transfer.ID)
	assert.Equal(t, int64(250), result.Transfer.Amount)
	assert.Equal(t, domain.StatusProcessed, result.Transfer.Status)
}

func TestTransferService_IdempotentConflict(t *testing.T) {
	f := newTransferFixture(t)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_conflict",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}

	tamperedReq := domain.CreateTransferRequest{
		IdempotencyKey: "key_conflict",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100, // different amount
	}

	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_conflict").
		Return(&domain.IdempotencyRecord{
			Key:          "key_conflict",
			RequestHash:  tamperedReq.CanonicalHash(),
			ResponseCode: 201,
			ResponseBody: `{}`,
		}, nil)

	// WithinTransaction, wallet, transfer, and ledger should NOT be called!
	_, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
}

func TestTransferService_ConcurrentDuplicateKeyRecovery(t *testing.T) {
	f := newTransferFixture(t)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_race",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         250,
	}

	// 1. Initial lookup misses (both requests arrive simultaneously)
	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_race").
		Return(nil, nil)

	// 2. Transaction fails with PostgreSQL unique violation 23505
	pgErr := &pgconn.PgError{Code: "23505"}
	f.txMgr.EXPECT().
		WithinTransaction(gomock.Any(), gomock.Any()).
		Return(pgErr)

	// 3. Race recovery: re-reads the winner's committed record
	cachedBody := `{"id":"tr_winner_1","idempotencyKey":"key_race","fromWalletId":"w1","toWalletId":"w2","amount":250,"status":"PROCESSED","createdAt":"2026-09-09T03:00:00Z","updatedAt":"2026-09-09T03:00:00Z"}`
	f.idemp.EXPECT().
		GetIdempotencyRecord(gomock.Any(), "key_race").
		Return(&domain.IdempotencyRecord{
			Key:          "key_race",
			RequestHash:  req.CanonicalHash(),
			ResponseCode: 201,
			ResponseBody: cachedBody,
		}, nil)

	result, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsReplay)
	assert.Equal(t, "tr_winner_1", result.Transfer.ID)
	assert.Equal(t, int64(250), result.Transfer.Amount)
}

func TestTransferService_GetTransfer(t *testing.T) {
	t.Run("empty transfer id", func(t *testing.T) {
		f := newTransferFixture(t)
		_, err := f.svc.GetTransfer(context.Background(), "  ")
		require.ErrorIs(t, err, service.ErrInvalidTransferID)
	})

	t.Run("transfer not found", func(t *testing.T) {
		f := newTransferFixture(t)
		f.transfer.EXPECT().
			GetTransfer(gomock.Any(), "tr_missing").
			Return(nil, domain.ErrTransferNotFound)

		_, err := f.svc.GetTransfer(context.Background(), "tr_missing")
		require.ErrorIs(t, err, domain.ErrTransferNotFound)
	})

	t.Run("transfer repository error", func(t *testing.T) {
		f := newTransferFixture(t)
		dbErr := errors.New("database connection failed")
		f.transfer.EXPECT().
			GetTransfer(gomock.Any(), "tr_err").
			Return(nil, dbErr)

		_, err := f.svc.GetTransfer(context.Background(), "tr_err")
		require.ErrorIs(t, err, dbErr)
	})

	t.Run("successful transfer retrieval", func(t *testing.T) {
		f := newTransferFixture(t)
		expected := &domain.Transfer{
			ID:             "tr_123",
			IdempotencyKey: "key_1",
			FromWalletID:   "w1",
			ToWalletID:     "w2",
			Amount:         500,
			Status:         domain.StatusProcessed,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		f.transfer.EXPECT().
			GetTransfer(gomock.Any(), "tr_123").
			Return(expected, nil)

		actual, err := f.svc.GetTransfer(context.Background(), "tr_123")
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	})
}

func TestTransferService_ListWalletTransfers(t *testing.T) {
	t.Run("empty wallet id", func(t *testing.T) {
		f := newTransferFixture(t)
		_, err := f.svc.ListWalletTransfers(context.Background(), "  ", 10, 0)
		require.ErrorIs(t, err, service.ErrInvalidWalletID)
	})

	t.Run("wallet not found", func(t *testing.T) {
		f := newTransferFixture(t)
		f.wallet.EXPECT().
			GetWallet(gomock.Any(), "w_missing").
			Return(nil, domain.ErrWalletNotFound)

		_, err := f.svc.ListWalletTransfers(context.Background(), "w_missing", 10, 0)
		require.ErrorIs(t, err, domain.ErrWalletNotFound)
	})

	t.Run("default pagination when non-positive limit and negative offset", func(t *testing.T) {
		f := newTransferFixture(t)
		f.wallet.EXPECT().
			GetWallet(gomock.Any(), "w1").
			Return(&domain.Wallet{ID: "w1", Balance: 1000}, nil)

		f.transfer.EXPECT().
			ListTransfersByWalletID(gomock.Any(), "w1", int32(20), int32(0)).
			Return([]domain.Transfer{}, nil)

		results, err := f.svc.ListWalletTransfers(context.Background(), "w1", 0, -5)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("cap limit to max pagination 100", func(t *testing.T) {
		f := newTransferFixture(t)
		f.wallet.EXPECT().
			GetWallet(gomock.Any(), "w1").
			Return(&domain.Wallet{ID: "w1", Balance: 1000}, nil)

		f.transfer.EXPECT().
			ListTransfersByWalletID(gomock.Any(), "w1", int32(100), int32(10)).
			Return([]domain.Transfer{}, nil)

		results, err := f.svc.ListWalletTransfers(context.Background(), "w1", 500, 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("successful transfer list", func(t *testing.T) {
		f := newTransferFixture(t)
		f.wallet.EXPECT().
			GetWallet(gomock.Any(), "w1").
			Return(&domain.Wallet{ID: "w1", Balance: 1000}, nil)

		expected := []domain.Transfer{
			{
				ID:             "tr_2",
				IdempotencyKey: "key_2",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         200,
				Status:         domain.StatusProcessed,
			},
			{
				ID:             "tr_1",
				IdempotencyKey: "key_1",
				FromWalletID:   "w3",
				ToWalletID:     "w1",
				Amount:         500,
				Status:         domain.StatusProcessed,
			},
		}

		f.transfer.EXPECT().
			ListTransfersByWalletID(gomock.Any(), "w1", int32(10), int32(0)).
			Return(expected, nil)

		results, err := f.svc.ListWalletTransfers(context.Background(), "w1", 10, 0)
		require.NoError(t, err)
		assert.Equal(t, expected, results)
	})
}
