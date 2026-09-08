package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

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
}

func newTransferFixture(t *testing.T) *transferFixture {
	ctrl := gomock.NewController(t)
	f := &transferFixture{
		txMgr:    mocks.NewMockTransactor(ctrl),
		wallet:   mocks.NewMockWalletRepository(ctrl),
		transfer: mocks.NewMockTransferRepository(ctrl),
		ledger:   mocks.NewMockLedgerRepository(ctrl),
	}
	f.svc = service.NewTransferService(f.txMgr, f.wallet, f.transfer, f.ledger)
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
	assert.Equal(t, domain.StatusProcessed, result.Status)
	assert.Equal(t, int64(300), result.Amount)
}

func TestTransferService_InsufficientFunds(t *testing.T) {
	f := newTransferFixture(t)

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

	req := domain.CreateTransferRequest{
		IdempotencyKey: "key_insuf",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}

	_, err := f.svc.ExecuteTransfer(context.Background(), req)
	require.ErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestTransferService_InsufficientFunds_RecordFailureError(t *testing.T) {
	f := newTransferFixture(t)

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
