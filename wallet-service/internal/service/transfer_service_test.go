package service

import (
	"context"
	"testing"
	"time"

	"github.com/shubh/wallet-service/internal/domain"
)

// MockTxManager implements TxManager without a real DB.
type MockTxManager struct{}

func (m *MockTxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	// Directly execute the function without a real transaction
	return fn(ctx)
}

// MockWalletRepository
type MockWalletRepository struct {
	LockWalletsFn func(ctx context.Context, w1, w2 string) (*domain.Wallet, *domain.Wallet, error)
	UpdateBalanceFn func(ctx context.Context, id string, bal int64) error
}

func (m *MockWalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	return nil, nil // Not heavily used in CreateTransfer
}

func (m *MockWalletRepository) LockWalletsForTransfer(ctx context.Context, w1, w2 string) (*domain.Wallet, *domain.Wallet, error) {
	if m.LockWalletsFn != nil {
		return m.LockWalletsFn(ctx, w1, w2)
	}
	return &domain.Wallet{ID: w1, Balance: 1000}, &domain.Wallet{ID: w2, Balance: 1000}, nil
}

func (m *MockWalletRepository) UpdateBalance(ctx context.Context, id string, bal int64) error {
	if m.UpdateBalanceFn != nil {
		return m.UpdateBalanceFn(ctx, id, bal)
	}
	return nil
}

// MockTransferRepository
type MockTransferRepository struct {
	FindByIdempotencyKeyFn func(ctx context.Context, key string) (*domain.Transfer, error)
	CreateFn               func(ctx context.Context, t *domain.Transfer) error
}

func (m *MockTransferRepository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	if m.FindByIdempotencyKeyFn != nil {
		return m.FindByIdempotencyKeyFn(ctx, key)
	}
	return nil, nil
}

func (m *MockTransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, t)
	}
	t.ID = "transfer_123"
	return nil
}

func (m *MockTransferRepository) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	return nil, nil
}

// MockLedgerRepository
type MockLedgerRepository struct {
	CreatePairFn func(ctx context.Context, transferID string, w1, w2 string, amt int64) error
}

func (m *MockLedgerRepository) CreatePair(ctx context.Context, transferID string, w1, w2 string, amt int64) error {
	if m.CreatePairFn != nil {
		return m.CreatePairFn(ctx, transferID, w1, w2, amt)
	}
	return nil
}

func (m *MockLedgerRepository) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return nil, nil
}

func TestCreateTransfer_Success(t *testing.T) {
	req := domain.TransferRequest{
		IdempotencyKey: "idem_1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         500,
	}

	mockWalletRepo := &MockWalletRepository{}
	mockTransferRepo := &MockTransferRepository{}
	mockLedgerRepo := &MockLedgerRepository{}
	txManager := &MockTxManager{}

	service := NewTransferService(txManager, mockWalletRepo, mockTransferRepo, mockLedgerRepo)

	transfer, err := service.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if transfer == nil {
		t.Fatalf("expected transfer, got nil")
	}
	if transfer.Status != domain.TransferStatusProcessed {
		t.Errorf("expected status %s, got %s", domain.TransferStatusProcessed, transfer.Status)
	}
}

func TestCreateTransfer_IdempotentReplaySuccess(t *testing.T) {
	req := domain.TransferRequest{
		IdempotencyKey: "idem_1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         500,
	}

	mockTransferRepo := &MockTransferRepository{
		FindByIdempotencyKeyFn: func(ctx context.Context, key string) (*domain.Transfer, error) {
			return &domain.Transfer{
				ID:             "transfer_123",
				IdempotencyKey: key,
				Status:         domain.TransferStatusProcessed,
				CreatedAt:      time.Now(),
			}, nil
		},
	}

	service := NewTransferService(&MockTxManager{}, &MockWalletRepository{}, mockTransferRepo, &MockLedgerRepository{})

	transfer, err := service.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if transfer.ID != "transfer_123" {
		t.Errorf("expected original transfer ID, got %s", transfer.ID)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	req := domain.TransferRequest{
		IdempotencyKey: "idem_1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1500, // greater than balance (1000)
	}

	mockWalletRepo := &MockWalletRepository{
		LockWalletsFn: func(ctx context.Context, w1, w2 string) (*domain.Wallet, *domain.Wallet, error) {
			return &domain.Wallet{ID: w1, Balance: 1000}, &domain.Wallet{ID: w2, Balance: 1000}, nil
		},
	}

	mockTransferRepo := &MockTransferRepository{
		CreateFn: func(ctx context.Context, t *domain.Transfer) error {
			t.ID = "failed_123"
			return nil
		},
	}

	service := NewTransferService(&MockTxManager{}, mockWalletRepo, mockTransferRepo, &MockLedgerRepository{})

	transfer, err := service.CreateTransfer(context.Background(), req)
	if err != domain.ErrInsufficientFunds {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	if transfer == nil || transfer.Status != domain.TransferStatusFailed {
		t.Errorf("expected returned transfer to be FAILED, got %v", transfer)
	}
}
