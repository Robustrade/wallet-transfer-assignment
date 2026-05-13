package services

import (
	"context"
	"errors"
	"testing"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
)

// MockWalletRepository is a mock for WalletRepository
type MockWalletRepository struct {
	mock.Mock
}

func (m *MockWalletRepository) GetForUpdate(ctx context.Context, tx *gorm.DB, walletID string) (*models.Wallet, error) {
	args := m.Called(ctx, tx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Wallet), args.Error(1)
}

func (m *MockWalletRepository) UpdateBalance(ctx context.Context, tx *gorm.DB, walletID string, delta int64) error {
	args := m.Called(ctx, tx, walletID, delta)
	return args.Error(0)
}

func (m *MockWalletRepository) GetBalance(ctx context.Context, walletID string) (int64, error) {
	args := m.Called(ctx, walletID)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockWalletRepository) Create(ctx context.Context, tx *gorm.DB, walletID string, initialBalance int64) (*models.Wallet, error) {
	args := m.Called(ctx, tx, walletID, initialBalance)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Wallet), args.Error(1)
}

// MockTransferRepository is a mock for TransferRepository
type MockTransferRepository struct {
	mock.Mock
}

func (m *MockTransferRepository) Create(ctx context.Context, tx *gorm.DB, fromWalletID, toWalletID string, amount int64, transferID string) (*models.Transfer, error) {
	args := m.Called(ctx, tx, fromWalletID, toWalletID, amount, transferID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Transfer), args.Error(1)
}

func (m *MockTransferRepository) UpdateStatus(ctx context.Context, tx *gorm.DB, transferID, newStatus string) error {
	args := m.Called(ctx, tx, transferID, newStatus)
	return args.Error(0)
}

func (m *MockTransferRepository) GetByID(ctx context.Context, transferID string) (*models.Transfer, error) {
	args := m.Called(ctx, transferID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Transfer), args.Error(1)
}

func (m *MockTransferRepository) GetTransferHistory(ctx context.Context, walletID string, limit, offset int) ([]*models.Transfer, error) {
	args := m.Called(ctx, walletID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Transfer), args.Error(1)
}

// MockLedgerRepository is a mock for LedgerRepository
type MockLedgerRepository struct {
	mock.Mock
}

func (m *MockLedgerRepository) Create(ctx context.Context, tx *gorm.DB, walletID, transferID, entryType string, amount int64, entryID string) (*models.LedgerEntry, error) {
	args := m.Called(ctx, tx, walletID, transferID, entryType, amount, entryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.LedgerEntry), args.Error(1)
}

func (m *MockLedgerRepository) GetByTransfer(ctx context.Context, transferID string) ([]*models.LedgerEntry, error) {
	args := m.Called(ctx, transferID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.LedgerEntry), args.Error(1)
}

func (m *MockLedgerRepository) VerifyBalance(ctx context.Context, transferID string) (bool, error) {
	args := m.Called(ctx, transferID)
	return args.Get(0).(bool), args.Error(1)
}

func (m *MockLedgerRepository) GetLedgerBalance(ctx context.Context, walletID string) (int64, error) {
	args := m.Called(ctx, walletID)
	return args.Get(0).(int64), args.Error(1)
}

// MockIdempotencyRepository is a mock for IdempotencyRepository
type MockIdempotencyRepository struct {
	mock.Mock
}

func (m *MockIdempotencyRepository) GetByKey(ctx context.Context, idempotencyKey string) (*models.IdempotencyRecord, error) {
	args := m.Called(ctx, idempotencyKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.IdempotencyRecord), args.Error(1)
}

func (m *MockIdempotencyRepository) Record(ctx context.Context, tx *gorm.DB, idempotencyKey, transferID string, response interface{}) error {
	args := m.Called(ctx, tx, idempotencyKey, transferID, response)
	return args.Error(0)
}

func TestTransferService_ExecuteTransfer_Validation(t *testing.T) {
	mockWalletRepo := new(MockWalletRepository)
	mockTransferRepo := new(MockTransferRepository)
	mockLedgerRepo := new(MockLedgerRepository)
	mockIdempotencyRepo := new(MockIdempotencyRepository)

	service := NewTransferService(nil, mockWalletRepo, mockTransferRepo, mockLedgerRepo, mockIdempotencyRepo)

	ctx := context.Background()

	t.Run("zero amount validation", func(t *testing.T) {
		_, err := service.ExecuteTransfer(ctx, "key-1", "wallet-1", "wallet-2", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	t.Run("negative amount validation", func(t *testing.T) {
		_, err := service.ExecuteTransfer(ctx, "key-1", "wallet-1", "wallet-2", -100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "amount must be positive")
	})

	t.Run("missing from wallet validation", func(t *testing.T) {
		_, err := service.ExecuteTransfer(ctx, "key-1", "", "wallet-2", 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "both wallet IDs must be provided")
	})

	t.Run("missing to wallet validation", func(t *testing.T) {
		_, err := service.ExecuteTransfer(ctx, "key-1", "wallet-1", "", 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "both wallet IDs must be provided")
	})

	t.Run("same wallet validation", func(t *testing.T) {
		_, err := service.ExecuteTransfer(ctx, "key-1", "wallet-1", "wallet-1", 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot transfer to the same wallet")
	})
}

func TestTransferService_ExecuteTransfer_Idempotency(t *testing.T) {
	// Initialize mocks locally so they are in scope
	mockWalletRepo := new(MockWalletRepository)
	mockTransferRepo := new(MockTransferRepository)
	mockLedgerRepo := new(MockLedgerRepository)
	mockIdempotencyRepo := new(MockIdempotencyRepository)

	service := NewTransferService(nil, mockWalletRepo, mockTransferRepo, mockLedgerRepo, mockIdempotencyRepo)
	ctx := context.Background()

	t.Run("idempotent request returns cached response", func(t *testing.T) {

		mockIdempotencyRepo.On("GetByKey", ctx, "existing-key").Return(&models.IdempotencyRecord{
			IdempotencyKey: "existing-key",
			Response:       []byte(`{"transfer_id":"existing-transfer","status":"PROCESSED","from_wallet_id":"wallet-1","to_wallet_id":"wallet-2","amount":100}`),
		}, nil).Once()

		response, err := service.ExecuteTransfer(ctx, "existing-key", "wallet-1", "wallet-2", 100)
		assert.NoError(t, err)
		assert.Equal(t, "existing-transfer", response.TransferID)
		assert.Equal(t, models.TransferStatusProcessed, response.Status)

		mockIdempotencyRepo.AssertExpectations(t)
	})

	t.Run("idempotency check error", func(t *testing.T) {
		mockIdempotencyRepo.On("GetByKey", ctx, "key-1").Return(nil, errors.New("database error")).Once()

		_, err := service.ExecuteTransfer(ctx, "key-1", "wallet-1", "wallet-2", 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check idempotency")

		mockIdempotencyRepo.AssertExpectations(t)
	})
}

func TestTransferService_GetBalance(t *testing.T) {
	mockWalletRepo := new(MockWalletRepository)
	mockTransferRepo := new(MockTransferRepository)
	mockLedgerRepo := new(MockLedgerRepository)
	mockIdempotencyRepo := new(MockIdempotencyRepository)

	service := NewTransferService(nil, mockWalletRepo, mockTransferRepo, mockLedgerRepo, mockIdempotencyRepo)
	ctx := context.Background()

	t.Run("successful balance retrieval", func(t *testing.T) {
		mockWalletRepo.On("GetBalance", ctx, "wallet-1").Return(int64(500), nil).Once()

		balance, err := service.GetBalance(ctx, "wallet-1")
		assert.NoError(t, err)
		assert.Equal(t, int64(500), balance)

		mockWalletRepo.AssertExpectations(t)
	})

	t.Run("balance retrieval error", func(t *testing.T) {
		mockWalletRepo.On("GetBalance", ctx, "wallet-1").Return(int64(0), errors.New("wallet not found")).Once()

		_, err := service.GetBalance(ctx, "wallet-1")
		assert.Error(t, err)

		mockWalletRepo.AssertExpectations(t)
	})
}

func TestTransferService_GetTransferHistory(t *testing.T) {
	mockWalletRepo := new(MockWalletRepository)
	mockTransferRepo := new(MockTransferRepository)
	mockLedgerRepo := new(MockLedgerRepository)
	mockIdempotencyRepo := new(MockIdempotencyRepository)

	service := NewTransferService(nil, mockWalletRepo, mockTransferRepo, mockLedgerRepo, mockIdempotencyRepo)
	ctx := context.Background()

	t.Run("successful transfer history retrieval", func(t *testing.T) {
		transfers := []*models.Transfer{
			{TransferID: "transfer-1", Amount: 100, Status: models.TransferStatusProcessed},
			{TransferID: "transfer-2", Amount: 200, Status: models.TransferStatusProcessed},
		}

		mockTransferRepo.On("GetTransferHistory", ctx, "wallet-1", 10, 0).Return(transfers, nil).Once()

		history, err := service.GetTransferHistory(ctx, "wallet-1", 10, 0)
		assert.NoError(t, err)
		assert.Len(t, history, 2)
		assert.Equal(t, "transfer-1", history[0].TransferID)

		mockTransferRepo.AssertExpectations(t)
	})

	t.Run("transfer history retrieval error", func(t *testing.T) {
		mockTransferRepo.On("GetTransferHistory", ctx, "wallet-1", 10, 0).Return(nil, errors.New("database error")).Once()

		_, err := service.GetTransferHistory(ctx, "wallet-1", 10, 0)
		assert.Error(t, err)

		mockTransferRepo.AssertExpectations(t)
	})
}

func TestTransferService_CreateWallet(t *testing.T) {
	mockWalletRepo := new(MockWalletRepository)
	mockTransferRepo := new(MockTransferRepository)
	mockLedgerRepo := new(MockLedgerRepository)
	mockIdempotencyRepo := new(MockIdempotencyRepository)

	service := NewTransferService(nil, mockWalletRepo, mockTransferRepo, mockLedgerRepo, mockIdempotencyRepo)
	ctx := context.Background()

	t.Run("successful wallet creation", func(t *testing.T) {
		expectedWallet := &models.Wallet{WalletID: "new-wallet", Balance: 100}
		mockWalletRepo.On("Create", ctx, mock.Anything, mock.AnythingOfType("string"), int64(100)).
			Return(expectedWallet, nil).Once()

		wallet, err := service.CreateWallet(ctx, 100)
		assert.NoError(t, err)
		assert.Equal(t, "new-wallet", wallet.WalletID)
		assert.Equal(t, int64(100), wallet.Balance)

		mockWalletRepo.AssertExpectations(t)
	})

	t.Run("negative initial balance", func(t *testing.T) {
		_, err := service.CreateWallet(ctx, -10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "initial balance cannot be negative")
	})
}

func TestTransferResponse_Structure(t *testing.T) {
	response := TransferResponse{
		TransferID:   "transfer-123",
		Status:       models.TransferStatusProcessed,
		FromWalletID: "wallet-1",
		ToWalletID:   "wallet-2",
		Amount:       100,
	}

	assert.Equal(t, "transfer-123", response.TransferID)
	assert.Equal(t, models.TransferStatusProcessed, response.Status)
	assert.Equal(t, "wallet-1", response.FromWalletID)
	assert.Equal(t, "wallet-2", response.ToWalletID)
	assert.Equal(t, int64(100), response.Amount)
}
