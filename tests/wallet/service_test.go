package wallet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
)

// mockRepository implements wallet.Repository for testing.
type mockRepository struct {
	getByIDFunc         func(ctx context.Context, id string) (*wallet.Wallet, error)
	validateWalletsFunc func(ctx context.Context, ids []string) error
	createFunc          func(ctx context.Context, w *wallet.Wallet) error
	listFunc            func(ctx context.Context, ownerID string) ([]*wallet.Wallet, error)
}

func (m *mockRepository) GetByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockRepository) ValidateWallets(ctx context.Context, ids []string) error {
	if m.validateWalletsFunc != nil {
		return m.validateWalletsFunc(ctx, ids)
	}
	return nil
}

func (m *mockRepository) Create(ctx context.Context, w *wallet.Wallet) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, w)
	}
	return nil
}

func (m *mockRepository) List(ctx context.Context, ownerID string) ([]*wallet.Wallet, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, ownerID)
	}
	return nil, nil
}

func TestService_GetByID(t *testing.T) {
	mockWallet := &wallet.Wallet{ID: "wallet_123", OwnerID: "user_1", Currency: "USD", Status: wallet.StatusActive}
	mockErr := errors.New("db error")

	tests := []struct {
		name    string
		id      string
		mockRes *wallet.Wallet
		mockErr error
		wantErr bool
	}{
		{
			name:    "success",
			id:      "wallet_123",
			mockRes: mockWallet,
			mockErr: nil,
			wantErr: false,
		},
		{
			name:    "repository error",
			id:      "wallet_123",
			mockRes: nil,
			mockErr: mockErr,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepository{
				getByIDFunc: func(ctx context.Context, id string) (*wallet.Wallet, error) {
					if id != tt.id {
						t.Errorf("expected id %s, got %s", tt.id, id)
					}
					return tt.mockRes, tt.mockErr
				},
			}
			svc := wallet.NewService(repo)
			res, err := svc.GetByID(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && res != tt.mockRes {
				t.Errorf("GetByID() got = %v, want %v", res, tt.mockRes)
			}
		})
	}
}

func TestService_Create(t *testing.T) {
	tests := []struct {
		name     string
		ownerID  string
		currency string
		mockErr  error
		wantErr  bool
	}{
		{
			name:     "success",
			ownerID:  "user_1",
			currency: "USD",
			mockErr:  nil,
			wantErr:  false,
		},
		{
			name:     "repository error",
			ownerID:  "user_1",
			currency: "USD",
			mockErr:  errors.New("db error"),
			wantErr:  true,
		},
		{
			name:     "validation error (missing owner)",
			ownerID:  "",
			currency: "USD",
			mockErr:  nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepository{
				createFunc: func(ctx context.Context, w *wallet.Wallet) error {
					if w.OwnerID != tt.ownerID {
						t.Errorf("expected ownerID %s, got %s", tt.ownerID, w.OwnerID)
					}
					if w.Currency != tt.currency {
						t.Errorf("expected currency %s, got %s", tt.currency, w.Currency)
					}
					if w.ID == "" {
						t.Error("expected ID to be generated")
					}
					return tt.mockErr
				},
			}
			svc := wallet.NewService(repo)
			res, err := svc.Create(context.Background(), tt.ownerID, tt.currency)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && res == nil {
				t.Errorf("Create() returned nil wallet on success")
			}
		})
	}
}
