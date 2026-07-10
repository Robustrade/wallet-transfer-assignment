package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/service"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newWalletService() (*service.WalletService, *testutils.MockIWalletRepository, *testutils.MockITransferRepository) {
	mockWalletRepo := &testutils.MockIWalletRepository{}
	mockTransferRepo := &testutils.MockITransferRepository{}
	return &service.WalletService{
		WalletRepo:   mockWalletRepo,
		TransferRepo: mockTransferRepo,
	}, mockWalletRepo, mockTransferRepo
}

func TestWalletService_TransferFunds_Success(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(&domain.Transfer{ID: "txn_123", Status: domain.TransferProcessed}, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.NoError(t, err)
	assert.Equal(t, "txn_123", transactionID)
	assert.Equal(t, string(domain.TransferProcessed), status)
	assert.NotZero(t, timestamp)
	mockTransferRepo.AssertExpectations(t)
}

func TestWalletService_TransferFunds_Pending(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(&domain.Transfer{ID: "txn_123", Status: domain.TransferPending}, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.NoError(t, err)
	assert.Equal(t, "txn_123", transactionID)
	assert.Equal(t, string(domain.TransferPending), status)
	assert.NotZero(t, timestamp)
}

func TestWalletService_TransferFunds_ExecuteTransferStatusFailed(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(&domain.Transfer{ID: "txn_123", Status: domain.TransferFailed}, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrTransferFailed)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

// A transfer persisted as FAILED due to insufficient balance must surface
// the specific ErrInsufficientBalance, not the generic ErrTransferFailed,
// so the client still gets an accurate 422 rather than a 500.
func TestWalletService_TransferFunds_ExecuteTransferFailed_InsufficientBalance(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	reason := domain.ErrInsufficientBalance.Error()
	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(&domain.Transfer{ID: "txn_123", Status: domain.TransferFailed, FailureReason: &reason}, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrInsufficientBalance)
	assert.NotErrorIs(t, err, domain.ErrTransferFailed)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

func TestWalletService_TransferFunds_ExecuteTransferReturnsNil(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrSomethingWentWrong)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

func TestWalletService_TransferFunds_ExecuteTransferError(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrInsufficientBalance)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrInsufficientBalance)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

// A retry with the exact same parameters as the original transfer must
// return the original result instead of erroring.
func TestWalletService_TransferFunds_IdempotentReplay_SameParams(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	createdAt := time.Now().Add(-time.Minute)
	existing := &domain.Transfer{
		ID:           "txn_123",
		FromWalletID: "wallet1",
		ToWalletID:   "wallet2",
		Amount:       100.0,
		Status:       domain.TransferProcessed,
		CreatedAt:    createdAt,
	}

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrIdempotencyKeyConflict)
	mockTransferRepo.On("GetByIdempotencyKey", ctx, "unique-key-123").Return(existing, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.NoError(t, err)
	assert.Equal(t, "txn_123", transactionID)
	assert.Equal(t, string(domain.TransferProcessed), status)
	assert.Equal(t, createdAt.UnixMilli(), timestamp)
}

// A retry with the same key but different parameters is a genuine conflict.
func TestWalletService_TransferFunds_IdempotentReplay_DifferentParams(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	existing := &domain.Transfer{
		ID:           "txn_123",
		FromWalletID: "wallet1",
		ToWalletID:   "wallet2",
		Amount:       50.0, // different amount than the retried request
		Status:       domain.TransferProcessed,
		CreatedAt:    time.Now(),
	}

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrIdempotencyKeyConflict)
	mockTransferRepo.On("GetByIdempotencyKey", ctx, "unique-key-123").Return(existing, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyConflict)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

// A replayed original transfer that had failed should still surface as a failure.
func TestWalletService_TransferFunds_IdempotentReplay_OriginalFailed(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	existing := &domain.Transfer{
		ID:           "txn_123",
		FromWalletID: "wallet1",
		ToWalletID:   "wallet2",
		Amount:       100.0,
		Status:       domain.TransferFailed,
		CreatedAt:    time.Now(),
	}

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrIdempotencyKeyConflict)
	mockTransferRepo.On("GetByIdempotencyKey", ctx, "unique-key-123").Return(existing, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrTransferFailed)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

// A replay of a request that originally failed with insufficient balance
// must return that same specific error, not the generic ErrTransferFailed -
// the client should see identical, accurate outcomes on both the original
// attempt and any duplicate/retry.
func TestWalletService_TransferFunds_IdempotentReplay_OriginalInsufficientBalance(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	reason := domain.ErrInsufficientBalance.Error()
	existing := &domain.Transfer{
		ID:            "txn_123",
		FromWalletID:  "wallet1",
		ToWalletID:    "wallet2",
		Amount:        100.0,
		Status:        domain.TransferFailed,
		FailureReason: &reason,
		CreatedAt:     time.Now(),
	}

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrIdempotencyKeyConflict)
	mockTransferRepo.On("GetByIdempotencyKey", ctx, "unique-key-123").Return(existing, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrInsufficientBalance)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

func TestWalletService_TransferFunds_IdempotentReplay_LookupError(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrIdempotencyKeyConflict)
	mockTransferRepo.On("GetByIdempotencyKey", ctx, "unique-key-123").Return(nil, domain.ErrSomethingWentWrong)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrSomethingWentWrong)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}

func TestWalletService_GetWalletBalance_Success(t *testing.T) {
	ctx := context.Background()
	walletService, mockWalletRepo, _ := newWalletService()

	mockWalletRepo.On("GetByID", ctx, "wallet1").
		Return(&domain.Wallet{ID: "wallet1", OwnerName: "Alice", Balance: 250.5}, nil)

	ownerName, balance, err := walletService.GetWalletBalance(ctx, "wallet1")

	assert.NoError(t, err)
	assert.Equal(t, "Alice", ownerName)
	assert.Equal(t, 250.5, balance)
	mockWalletRepo.AssertExpectations(t)
}

func TestWalletService_GetWalletBalance_NotFound(t *testing.T) {
	ctx := context.Background()
	walletService, mockWalletRepo, _ := newWalletService()

	mockWalletRepo.On("GetByID", ctx, "missing-wallet").Return(nil, domain.ErrWalletNotFound)

	ownerName, balance, err := walletService.GetWalletBalance(ctx, "missing-wallet")

	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
	assert.Empty(t, ownerName)
	assert.Zero(t, balance)
}

func TestWalletService_GetTransferHistory_Success(t *testing.T) {
	ctx := context.Background()
	walletService, mockWalletRepo, mockTransferRepo := newWalletService()

	mockWalletRepo.On("GetByID", ctx, "wallet1").Return(&domain.Wallet{ID: "wallet1"}, nil)
	mockTransferRepo.On("ListByWallet", ctx, "wallet1", int32(20), int32(0)).
		Return([]*domain.Transfer{{ID: "txn_1", Status: domain.TransferProcessed}}, nil)

	transfers, limit, offset, err := walletService.GetTransferHistory(ctx, "wallet1", 0, 0)

	assert.NoError(t, err)
	assert.Len(t, transfers, 1)
	assert.Equal(t, "txn_1", transfers[0].ID)
	assert.Equal(t, int32(20), limit)
	assert.Equal(t, int32(0), offset)
	mockWalletRepo.AssertExpectations(t)
	mockTransferRepo.AssertExpectations(t)
}

func TestWalletService_GetTransferHistory_ClampsLimitAndOffset(t *testing.T) {
	ctx := context.Background()
	walletService, mockWalletRepo, mockTransferRepo := newWalletService()

	mockWalletRepo.On("GetByID", ctx, "wallet1").Return(&domain.Wallet{ID: "wallet1"}, nil)
	mockTransferRepo.On("ListByWallet", ctx, "wallet1", int32(100), int32(0)).
		Return([]*domain.Transfer{}, nil)

	_, limit, offset, err := walletService.GetTransferHistory(ctx, "wallet1", 1000, -5)

	assert.NoError(t, err)
	assert.Equal(t, int32(100), limit)
	assert.Equal(t, int32(0), offset)
	mockTransferRepo.AssertExpectations(t)
}

func TestWalletService_GetTransferHistory_WalletNotFound(t *testing.T) {
	ctx := context.Background()
	walletService, mockWalletRepo, _ := newWalletService()

	mockWalletRepo.On("GetByID", ctx, "missing-wallet").Return(nil, domain.ErrWalletNotFound)

	transfers, _, _, err := walletService.GetTransferHistory(ctx, "missing-wallet", 0, 0)

	assert.ErrorIs(t, err, domain.ErrWalletNotFound)
	assert.Nil(t, transfers)
}

func TestWalletService_TransferFunds_IdempotentReplay_NotFound(t *testing.T) {
	ctx := context.Background()
	walletService, _, mockTransferRepo := newWalletService()

	mockTransferRepo.On("ExecuteTransfer", ctx, "unique-key-123", "wallet1", "wallet2", 100.0, mock.AnythingOfType("int64")).
		Return(nil, domain.ErrIdempotencyKeyConflict)
	mockTransferRepo.On("GetByIdempotencyKey", ctx, "unique-key-123").Return(nil, nil)

	transactionID, status, timestamp, err := walletService.TransferFunds(ctx, "unique-key-123", "wallet1", "wallet2", 100.0)

	assert.ErrorIs(t, err, domain.ErrIdempotencyKeyConflict)
	assert.Empty(t, transactionID)
	assert.Empty(t, status)
	assert.Zero(t, timestamp)
}
