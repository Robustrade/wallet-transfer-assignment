package transporthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/application"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
)

type mockWalletRepo struct {
	getByIDFn func(ctx context.Context, id string) (*domain.Wallet, error)
}

func (m *mockWalletRepo) Create(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

func (m *mockWalletRepo) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	return m.getByIDFn(ctx, id)
}

func (m *mockWalletRepo) GetByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	panic("not implemented")
}

func (m *mockWalletRepo) Update(ctx context.Context, wallet *domain.Wallet) error {
	panic("not implemented")
}

type mockLedgerRepo struct {
	getByWalletIDFn func(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

func (m *mockLedgerRepo) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	panic("not implemented")
}

func (m *mockLedgerRepo) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	panic("not implemented")
}

func (m *mockLedgerRepo) GetByWalletID(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	return m.getByWalletIDFn(ctx, walletID)
}

func newServerWithMockWalletService() *Server {
	walletService := application.NewWalletService(&mockWalletRepo{
		getByIDFn: func(ctx context.Context, id string) (*domain.Wallet, error) {
			return &domain.Wallet{ID: id, Balance: 10000}, nil
		},
	}, &mockLedgerRepo{
		getByWalletIDFn: func(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
			return []domain.LedgerEntry{
				{WalletID: walletID, EntryType: domain.LedgerEntryTypeCredit, Amount: 10000},
			}, nil
		},
	})
	return NewServer(walletService, nil, nil)
}

func TestHandleReconcileWallet_ReturnsReconciliationResult(t *testing.T) {
	server := newServerWithMockWalletService()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/wallets/wallet-123/reconcile", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}

	var response walletReconciliationResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.WalletID != "wallet-123" {
		t.Fatalf("expected walletId wallet-123, got %q", response.WalletID)
	}
	if !response.Match {
		t.Fatal("expected match=true, got false")
	}
	if response.StoredBalance != 10000 || response.LedgerBalance != 10000 {
		t.Fatalf("unexpected balances stored=%d ledger=%d", response.StoredBalance, response.LedgerBalance)
	}
}

func TestHandleReconcileWallet_ReturnsNotFoundWhenWalletMissing(t *testing.T) {
	walletService := application.NewWalletService(&mockWalletRepo{
		getByIDFn: func(ctx context.Context, id string) (*domain.Wallet, error) {
			return nil, repository.ErrNotFound
		},
	}, &mockLedgerRepo{})
	server := NewServer(walletService, nil, nil)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/wallets/missing-wallet/reconcile", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", res.Code)
	}
}
