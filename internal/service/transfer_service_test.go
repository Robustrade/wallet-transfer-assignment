package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

type mockRepository struct {
	transfers         map[string]*model.Transfer
	wallets           map[string]*model.Wallet
	executeTxFunc     func(ctx context.Context, transfer *model.Transfer) error
	getIdempotentFunc func(ctx context.Context, key string) (*model.Transfer, error)
}

func (m *mockRepository) ExecuteTransferTx(ctx context.Context, transfer *model.Transfer) error {
	if m.executeTxFunc != nil {
		return m.executeTxFunc(ctx, transfer)
	}
	return nil
}

func (m *mockRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error) {
	if m.getIdempotentFunc != nil {
		return m.getIdempotentFunc(ctx, key)
	}
	return nil, repository.ErrNotFound
}

func (m *mockRepository) GetWallet(ctx context.Context, id string) (*model.Wallet, error) {
	return nil, nil
}

func (m *mockRepository) CreateWallet(ctx context.Context, wallet *model.Wallet) error {
	return nil
}

func (m *mockRepository) GetTransfersByWalletID(ctx context.Context, walletID string) ([]*model.Transfer, error) {
	return nil, nil
}

func TestTransferService_Transfer_Validation(t *testing.T) {
	svc := NewTransferService(&mockRepository{})

	tests := []struct {
		name    string
		req     TransferRequest
		wantErr error
	}{
		{
			name:    "negative amount",
			req:     TransferRequest{Amount: -10, FromWalletID: "w1", ToWalletID: "w2", IdempotencyKey: "key1"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "same wallet",
			req:     TransferRequest{Amount: 10, FromWalletID: "w1", ToWalletID: "w1", IdempotencyKey: "key2"},
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "missing idempotency key",
			req:     TransferRequest{Amount: 10, FromWalletID: "w1", ToWalletID: "w2", IdempotencyKey: ""},
			wantErr: ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Transfer(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestTransferService_Transfer_Idempotency(t *testing.T) {
	existingTransfer := &model.Transfer{
		ID:             "t1",
		IdempotencyKey: "idem_123",
		Amount:         100,
	}

	mockRepo := &mockRepository{
		getIdempotentFunc: func(ctx context.Context, key string) (*model.Transfer, error) {
			if key == "idem_123" {
				return existingTransfer, nil
			}
			return nil, repository.ErrNotFound
		},
		executeTxFunc: func(ctx context.Context, transfer *model.Transfer) error {
			return nil // Should not be called for idempotency cache hit
		},
	}

	svc := NewTransferService(mockRepo)

	req := TransferRequest{
		IdempotencyKey: "idem_123",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	res, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ID != "t1" {
		t.Errorf("expected existing transfer to be returned, got %v", res.ID)
	}
}

func TestTransferService_Transfer_InsufficientFunds(t *testing.T) {
	mockRepo := &mockRepository{
		getIdempotentFunc: func(ctx context.Context, key string) (*model.Transfer, error) {
			return nil, repository.ErrNotFound
		},
		executeTxFunc: func(ctx context.Context, transfer *model.Transfer) error {
			return repository.ErrInsufficientFunds
		},
	}

	svc := NewTransferService(mockRepo)

	req := TransferRequest{
		IdempotencyKey: "idem_999",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         1000000,
	}

	_, err := svc.Transfer(context.Background(), req)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestTransferService_Transfer_FailedIdempotency(t *testing.T) {
	existingTransfer := &model.Transfer{
		ID:             "t1",
		IdempotencyKey: "idem_fail",
		Amount:         100,
		Status:         model.StatusFailed,
	}
	mockRepo := &mockRepository{
		getIdempotentFunc: func(ctx context.Context, key string) (*model.Transfer, error) {
			if key == "idem_fail" {
				return existingTransfer, nil
			}
			return nil, repository.ErrNotFound
		},
		executeTxFunc: func(ctx context.Context, transfer *model.Transfer) error {
			return repository.ErrIdempotencyConflict
		},
	}
	svc := NewTransferService(mockRepo)
	req := TransferRequest{IdempotencyKey: "idem_fail", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}

	_, err := svc.Transfer(context.Background(), req)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestTransferService_Transfer_WalletNotFound(t *testing.T) {
	mockRepo := &mockRepository{
		getIdempotentFunc: func(ctx context.Context, key string) (*model.Transfer, error) {
			return nil, repository.ErrNotFound
		},
		executeTxFunc: func(ctx context.Context, transfer *model.Transfer) error {
			return repository.ErrNotFound
		},
	}
	svc := NewTransferService(mockRepo)
	req := TransferRequest{IdempotencyKey: "idem_404", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}

	_, err := svc.Transfer(context.Background(), req)
	if !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("expected ErrWalletNotFound, got %v", err)
	}
}
