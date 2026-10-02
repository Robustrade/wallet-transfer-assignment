package service_test

import (
	"context"
	"testing"

	"wallet-service/internal/domain"
	"wallet-service/internal/service"
)

// MockRepository is a mock implementation of repository.Repository
type MockRepository struct {
	ExecuteTransferFn             func(ctx context.Context, transferReq *domain.Transfer) (*domain.Transfer, error)
	GetTransferByIdempotencyKeyFn func(ctx context.Context, key string) (*domain.Transfer, error)
}

func (m *MockRepository) ExecuteTransfer(ctx context.Context, transferReq *domain.Transfer) (*domain.Transfer, error) {
	if m.ExecuteTransferFn != nil {
		return m.ExecuteTransferFn(ctx, transferReq)
	}
	return nil, nil
}

func (m *MockRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	if m.GetTransferByIdempotencyKeyFn != nil {
		return m.GetTransferByIdempotencyKeyFn(ctx, key)
	}
	return nil, nil
}

func TestTransferService_Transfer_Idempotency(t *testing.T) {
	mockRepo := &MockRepository{
		GetTransferByIdempotencyKeyFn: func(ctx context.Context, key string) (*domain.Transfer, error) {
			if key == "existing-key" {
				return &domain.Transfer{
					ID:             "transfer-123",
					IdempotencyKey: key,
					State:          domain.TransferStateProcessed,
				}, nil
			}
			return nil, nil
		},
		ExecuteTransferFn: func(ctx context.Context, transferReq *domain.Transfer) (*domain.Transfer, error) {
			transferReq.ID = "new-transfer"
			transferReq.State = domain.TransferStateProcessed
			return transferReq, nil
		},
	}

	svc := service.NewTransferService(mockRepo)

	// Test 1: Existing Idempotency Key
	t.Run("Returns existing transfer for duplicate request", func(t *testing.T) {
		res, err := svc.Transfer(context.Background(), "existing-key", "w1", "w2", 100)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.ID != "transfer-123" {
			t.Errorf("expected transfer-123, got %s", res.ID)
		}
	})

	// Test 2: New Idempotency Key
	t.Run("Executes new transfer", func(t *testing.T) {
		res, err := svc.Transfer(context.Background(), "new-key", "w1", "w2", 100)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.ID != "new-transfer" {
			t.Errorf("expected new-transfer, got %s", res.ID)
		}
	})

	// Test 3: Same Wallet
	t.Run("Fails if same wallet", func(t *testing.T) {
		_, err := svc.Transfer(context.Background(), "key-3", "w1", "w1", 100)
		if err != domain.ErrSameWallet {
			t.Errorf("expected ErrSameWallet, got %v", err)
		}
	})

	t.Run("Fails if amount is less than or equal to zero", func(t *testing.T) {
		ctx := context.Background()
		_, err := svc.Transfer(ctx, "key-error", "w1", "w2", 0)
		if err != domain.ErrInvalidAmount {
			t.Errorf("expected ErrInvalidAmount, got %v", err)
		}

		_, err = svc.Transfer(ctx, "key-error-2", "w1", "w2", -50)
		if err != domain.ErrInvalidAmount {
			t.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})

	t.Run("Handles GetTransferByIdempotencyKey error", func(t *testing.T) {
		ctx := context.Background()
		mockRepo.GetTransferByIdempotencyKeyFn = func(ctx context.Context, key string) (*domain.Transfer, error) {
			return nil, domain.ErrWalletNotFound // using arbitrary error
		}
		_, err := svc.Transfer(ctx, "key", "w1", "w2", 100)
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("Handles ExecuteTransfer error", func(t *testing.T) {
		ctx := context.Background()
		mockRepo.GetTransferByIdempotencyKeyFn = func(ctx context.Context, key string) (*domain.Transfer, error) {
			return nil, nil
		}
		mockRepo.ExecuteTransferFn = func(ctx context.Context, req *domain.Transfer) (*domain.Transfer, error) {
			return nil, domain.ErrInsufficientFunds
		}
		_, err := svc.Transfer(ctx, "key", "w1", "w2", 100)
		if err != domain.ErrInsufficientFunds {
			t.Errorf("expected ErrInsufficientFunds, got %v", err)
		}
	})

	t.Run("Handles DuplicateTransfer race condition", func(t *testing.T) {
		ctx := context.Background()
		mockRepo.GetTransferByIdempotencyKeyFn = func(ctx context.Context, key string) (*domain.Transfer, error) {
			return &domain.Transfer{ID: "race-resolved"}, nil
		}
		mockRepo.ExecuteTransferFn = func(ctx context.Context, req *domain.Transfer) (*domain.Transfer, error) {
			return nil, domain.ErrDuplicateTransfer
		}
		res, err := svc.Transfer(ctx, "key", "w1", "w2", 100)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if res.ID != "race-resolved" {
			t.Errorf("expected race-resolved transfer, got %v", res)
		}
	})
}
