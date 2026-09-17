package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shubh/wallet-service/internal/domain"
	"github.com/shubh/wallet-service/internal/service"
)

// Reusing MockTxManager and Mock Repositories from service layer tests
type MockTxManager struct{}

func (m *MockTxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type MockWalletRepository struct{}

func (m *MockWalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	return nil, nil
}
func (m *MockWalletRepository) LockWalletsForTransfer(ctx context.Context, w1, w2 string) (*domain.Wallet, *domain.Wallet, error) {
	return &domain.Wallet{ID: w1, Balance: 1000}, &domain.Wallet{ID: w2, Balance: 1000}, nil
}
func (m *MockWalletRepository) UpdateBalance(ctx context.Context, id string, bal int64) error {
	return nil
}

type MockTransferRepository struct{}

func (m *MockTransferRepository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	return nil, nil
}
func (m *MockTransferRepository) Create(ctx context.Context, t *domain.Transfer) error {
	t.ID = "transfer_123"
	return nil
}
func (m *MockTransferRepository) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	return nil, nil
}

type MockLedgerRepository struct{}

func (m *MockLedgerRepository) CreatePair(ctx context.Context, transferID string, w1, w2 string, amt int64) error {
	return nil
}
func (m *MockLedgerRepository) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return nil, nil
}

func setupMockTransferHandler() *TransferHandler {
	svc := service.NewTransferService(
		&MockTxManager{},
		&MockWalletRepository{},
		&MockTransferRepository{},
		&MockLedgerRepository{},
	)
	return NewTransferHandler(svc)
}

func TestCreateTransfer_WrongMethod(t *testing.T) {
	handler := setupMockTransferHandler()

	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	rr := httptest.NewRecorder()

	handler.CreateTransfer(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestCreateTransfer_WrongContentType(t *testing.T) {
	handler := setupMockTransferHandler()

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()

	handler.CreateTransfer(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415, got %d", rr.Code)
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	handler := setupMockTransferHandler()

	body := domain.TransferRequest{
		IdempotencyKey: "idem1",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         500,
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateTransfer(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateTransfer_PayloadTooLarge(t *testing.T) {
	handler := setupMockTransferHandler()

	// 2 MB payload (limit is 1MB)
	largeBody := strings.Repeat("a", 2*1024*1024)
	
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.CreateTransfer(rr, req)

	// MaxBytesReader returns EOF/error when limit exceeded, handler returns 400 Bad Request for decode fail
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for large payload, got %d", rr.Code)
	}
}
