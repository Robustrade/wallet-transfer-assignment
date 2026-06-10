package service_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/repository"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/service"
)

// ── Mock Repositories ─────────────────────────────────────────────────────────

type mockWalletRepo struct {
	wallets map[string]*domain.Wallet
}

func newMockWalletRepo() *mockWalletRepo {
	return &mockWalletRepo{wallets: make(map[string]*domain.Wallet)}
}

func (m *mockWalletRepo) Create(_ context.Context, _ repository.DBTX, wallet *domain.Wallet) (*domain.Wallet, error) {
	if _, exists := m.wallets[wallet.ID]; exists {
		return nil, domain.ErrDuplicateWallet
	}
	m.wallets[wallet.ID] = wallet
	return wallet, nil
}

func (m *mockWalletRepo) GetByID(_ context.Context, _ repository.DBTX, id string) (*domain.Wallet, error) {
	w, ok := m.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	return w, nil
}

func (m *mockWalletRepo) GetByIDForUpdate(_ context.Context, _ repository.DBTX, id string) (*domain.Wallet, error) {
	w, ok := m.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	copy := *w
	return &copy, nil
}

func (m *mockWalletRepo) UpdateBalance(_ context.Context, _ repository.DBTX, id string, delta int64) error {
	w, ok := m.wallets[id]
	if !ok {
		return domain.ErrWalletNotFound
	}
	newBalance := w.Balance + delta
	if newBalance < 0 {
		return fmt.Errorf("CHECK constraint violated: balance would be %d", newBalance)
	}
	w.Balance = newBalance
	return nil
}

type mockTransferRepo struct {
	transfers map[string]*domain.Transfer
	nextID    int
}

func newMockTransferRepo() *mockTransferRepo {
	return &mockTransferRepo{transfers: make(map[string]*domain.Transfer)}
}

func (m *mockTransferRepo) Create(_ context.Context, _ repository.DBTX, transfer *domain.Transfer) (*domain.Transfer, error) {
	m.nextID++
	transfer.ID = fmt.Sprintf("00000000-0000-0000-0000-%012d", m.nextID)
	m.transfers[transfer.ID] = transfer
	return transfer, nil
}

func (m *mockTransferRepo) GetByID(_ context.Context, _ repository.DBTX, id string) (*domain.Transfer, error) {
	t, ok := m.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	return t, nil
}

func (m *mockTransferRepo) UpdateStatus(_ context.Context, _ repository.DBTX, id string, status domain.TransferStatus, errorReason *string) error {
	t, ok := m.transfers[id]
	if !ok {
		return domain.ErrTransferNotFound
	}
	t.Status = status
	t.ErrorReason = errorReason
	return nil
}

// ── Test Helpers ──────────────────────────────────────────────────────────────

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// newTestDB creates a minimal repository.DB with a nil pool, suitable for tests
// that only exercise code paths that don't call Pool.Begin/Pool.BeginTx.
// Methods like CreateWallet and GetWallet use db.Pool as a DBTX, but our mocks
// ignore the DBTX argument, so the nil pool is safe here.
func newTestDB() *repository.DB {
	return &repository.DB{}
}

func assertError(t *testing.T, err error, expectedCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", expectedCode)
	}
	domainErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if domainErr.Code != expectedCode {
		t.Errorf("expected error code %s, got %s", expectedCode, domainErr.Code)
	}
}

// ── Validation Tests (service delegates to domain) ────────────────────────────

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	logger := newTestLogger()

	// Note: These tests validate the service's first step (request validation)
	// without needing a database at all — the service rejects invalid requests
	// before opening a transaction.
	svc := service.NewTransferService(nil, nil, nil, nil, nil, logger)

	tests := []struct {
		name string
		req  domain.CreateTransferRequest
		code string
	}{
		{
			name: "missing idempotency key",
			req:  domain.CreateTransferRequest{FromWalletID: "a", ToWalletID: "b", Amount: 100},
			code: "INVALID_INPUT",
		},
		{
			name: "missing from wallet",
			req:  domain.CreateTransferRequest{IdempotencyKey: "k1", ToWalletID: "b", Amount: 100},
			code: "INVALID_INPUT",
		},
		{
			name: "missing to wallet",
			req:  domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "a", Amount: 100},
			code: "INVALID_INPUT",
		},
		{
			name: "zero amount",
			req:  domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "b", Amount: 0},
			code: "INVALID_INPUT",
		},
		{
			name: "negative amount",
			req:  domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "b", Amount: -50},
			code: "INVALID_INPUT",
		},
		{
			name: "self transfer",
			req:  domain.CreateTransferRequest{IdempotencyKey: "k1", FromWalletID: "a", ToWalletID: "a", Amount: 100},
			code: "INVALID_INPUT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.CreateTransfer(context.Background(), tt.req)
			if result != nil {
				t.Error("expected nil result for validation error")
			}
			assertError(t, err, tt.code)
		})
	}
}

// ── Wallet Service Tests ─────────────────────────────────────────────────────

func TestCreateWallet_Success(t *testing.T) {
	walletRepo := newMockWalletRepo()
	logger := newTestLogger()
	svc := service.NewTransferService(newTestDB(), walletRepo, nil, nil, nil, logger)

	wallet, err := svc.CreateWallet(context.Background(), domain.CreateWalletRequest{
		ID: "w1", Balance: 500, Currency: "USD",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wallet.ID != "w1" {
		t.Errorf("expected wallet ID w1, got %s", wallet.ID)
	}
	if wallet.Balance != 500 {
		t.Errorf("expected balance 500, got %d", wallet.Balance)
	}
}

func TestCreateWallet_Duplicate(t *testing.T) {
	walletRepo := newMockWalletRepo()
	logger := newTestLogger()
	svc := service.NewTransferService(newTestDB(), walletRepo, nil, nil, nil, logger)

	_, _ = svc.CreateWallet(context.Background(), domain.CreateWalletRequest{
		ID: "w1", Balance: 500, Currency: "USD",
	})
	_, err := svc.CreateWallet(context.Background(), domain.CreateWalletRequest{
		ID: "w1", Balance: 1000, Currency: "USD",
	})
	assertError(t, err, "DUPLICATE_WALLET")
}

func TestCreateWallet_ValidationError(t *testing.T) {
	logger := newTestLogger()
	svc := service.NewTransferService(nil, nil, nil, nil, nil, logger)

	_, err := svc.CreateWallet(context.Background(), domain.CreateWalletRequest{
		Balance: 500, Currency: "USD",
	})
	assertError(t, err, "INVALID_INPUT")
}

func TestGetWallet_NotFound(t *testing.T) {
	walletRepo := newMockWalletRepo()
	logger := newTestLogger()
	svc := service.NewTransferService(newTestDB(), walletRepo, nil, nil, nil, logger)

	_, err := svc.GetWallet(context.Background(), "nonexistent")
	assertError(t, err, "WALLET_NOT_FOUND")
}

func TestGetTransfer_NotFound(t *testing.T) {
	transferRepo := newMockTransferRepo()
	logger := newTestLogger()
	svc := service.NewTransferService(newTestDB(), nil, transferRepo, nil, nil, logger)

	_, err := svc.GetTransfer(context.Background(), "nonexistent")
	assertError(t, err, "TRANSFER_NOT_FOUND")
}
