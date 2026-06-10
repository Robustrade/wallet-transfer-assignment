package application

import (
	"context"
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
)

type mockWalletRepository struct {
	getByIDFn func(ctx context.Context, id string) (*domain.Wallet, error)
}

func (m *mockWalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

func (m *mockWalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	return m.getByIDFn(ctx, id)
}

func (m *mockWalletRepository) GetByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	panic("not implemented")
}

func (m *mockWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

type mockLedgerRepository struct {
	getByWalletIDFn func(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

func (m *mockLedgerRepository) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	panic("not implemented")
}

func (m *mockLedgerRepository) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	panic("not implemented")
}

func (m *mockLedgerRepository) GetByWalletID(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	return m.getByWalletIDFn(ctx, walletID)
}

func TestReconcile_MatchReturnsTrueWhenLedgerMatchesBalance(t *testing.T) {
	walletRepo := &mockWalletRepository{
		getByIDFn: func(ctx context.Context, id string) (*domain.Wallet, error) {
			return &domain.Wallet{ID: id, Balance: 10000}, nil
		},
	}
	ledgerRepo := &mockLedgerRepository{
		getByWalletIDFn: func(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
			return []domain.LedgerEntry{
				{WalletID: walletID, EntryType: domain.LedgerEntryTypeCredit, Amount: 10000},
			}, nil
		},
	}

	service := NewWalletService(walletRepo, ledgerRepo)
	result, err := service.Reconcile(context.Background(), "wallet-123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Match {
		t.Fatalf("expected match=true, got false")
	}
	if result.LedgerBalance != 10000 || result.StoredBalance != 10000 {
		t.Fatalf("expected balances 10000, got stored=%d ledger=%d", result.StoredBalance, result.LedgerBalance)
	}
}

func TestReconcile_MatchReturnsFalseWhenLedgerDiffersFromStoredBalance(t *testing.T) {
	walletRepo := &mockWalletRepository{
		getByIDFn: func(ctx context.Context, id string) (*domain.Wallet, error) {
			return &domain.Wallet{ID: id, Balance: 10000}, nil
		},
	}
	ledgerRepo := &mockLedgerRepository{
		getByWalletIDFn: func(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
			return []domain.LedgerEntry{
				{WalletID: walletID, EntryType: domain.LedgerEntryTypeCredit, Amount: 9000},
			}, nil
		},
	}

	service := NewWalletService(walletRepo, ledgerRepo)
	result, err := service.Reconcile(context.Background(), "wallet-123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Match {
		t.Fatalf("expected match=false, got true")
	}
	if result.LedgerBalance != 9000 || result.StoredBalance != 10000 {
		t.Fatalf("unexpected balances stored=%d ledger=%d", result.StoredBalance, result.LedgerBalance)
	}
}

func TestReconcile_PropagatesWalletNotFound(t *testing.T) {
	walletRepo := &mockWalletRepository{
		getByIDFn: func(ctx context.Context, id string) (*domain.Wallet, error) {
			return nil, repository.ErrNotFound
		},
	}
	ledgerRepo := &mockLedgerRepository{}

	service := NewWalletService(walletRepo, ledgerRepo)
	_, err := service.Reconcile(context.Background(), "missing-wallet")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
