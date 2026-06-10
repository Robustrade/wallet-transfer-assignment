package application

import (
	"context"
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type mockWalletRepositoryForCreate struct {
	createdWallet *domain.Wallet
	createErr     error
}

func (m *mockWalletRepositoryForCreate) Create(ctx context.Context, wallet *domain.Wallet) error {
	m.createdWallet = wallet
	return m.createErr
}

func (m *mockWalletRepositoryForCreate) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	panic("not implemented")
}

func (m *mockWalletRepositoryForCreate) GetByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	panic("not implemented")
}

func (m *mockWalletRepositoryForCreate) Update(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

func TestWalletService_CreateGeneratesUUIDWhenIDEmpty(t *testing.T) {
	walletRepo := &mockWalletRepositoryForCreate{}
	service := NewWalletService(walletRepo, nil)

	wallet, err := service.Create(context.Background(), "", 1000)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if wallet == nil || wallet.ID == "" {
		t.Fatal("expected generated wallet ID")
	}
	if wallet.Balance != 1000 {
		t.Fatalf("expected balance 1000, got %d", wallet.Balance)
	}
	if walletRepo.createdWallet == nil {
		t.Fatal("expected repository Create to be called")
	}
}

func TestWalletService_CreateRejectsInvalidWalletID(t *testing.T) {
	walletRepo := &mockWalletRepositoryForCreate{}
	service := NewWalletService(walletRepo, nil)

	_, err := service.Create(context.Background(), "not-a-uuid", 1000)
	if !errors.Is(err, domain.ErrInvalidWalletIDFormat) {
		t.Fatalf("expected ErrInvalidWalletIDFormat, got %v", err)
	}
	if walletRepo.createdWallet != nil {
		t.Fatal("expected repository Create not to be called")
	}
}

type mockWalletRepositoryForGet struct {
	id string
}

func (m *mockWalletRepositoryForGet) Create(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

func (m *mockWalletRepositoryForGet) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	m.id = id
	return &domain.Wallet{ID: id, Balance: 500}, nil
}

func (m *mockWalletRepositoryForGet) GetByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	panic("not implemented")
}

func (m *mockWalletRepositoryForGet) Update(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

func TestWalletService_GetRejectsInvalidWalletID(t *testing.T) {
	walletRepo := &mockWalletRepositoryForGet{}
	service := NewWalletService(walletRepo, nil)

	_, err := service.Get(context.Background(), "not-a-uuid")
	if !errors.Is(err, domain.ErrInvalidWalletIDFormat) {
		t.Fatalf("expected ErrInvalidWalletIDFormat, got %v", err)
	}
	if walletRepo.id != "" {
		t.Fatal("expected repository GetByID not to be called")
	}
}
