package domain_test

import (
	"testing"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────────────
// CreateTransferRequest validation tests
// ──────────────────────────────────────────────────────────────────────────────

func TestCreateTransferRequest_Validate_Valid(t *testing.T) {
	req := domain.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	if err := req.Validate(); err != nil {
		t.Errorf("expected no error for valid request, got: %v", err)
	}
}

func TestCreateTransferRequest_Validate_MissingIdempotencyKey(t *testing.T) {
	req := domain.CreateTransferRequest{
		FromWalletID: "wallet_a",
		ToWalletID:   "wallet_b",
		Amount:       100,
	}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "idempotencyKey is required")
}

func TestCreateTransferRequest_Validate_MissingFromWallet(t *testing.T) {
	req := domain.CreateTransferRequest{
		IdempotencyKey: "key-1",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "fromWalletId is required")
}

func TestCreateTransferRequest_Validate_MissingToWallet(t *testing.T) {
	req := domain.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_a",
		Amount:         100,
	}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "toWalletId is required")
}

func TestCreateTransferRequest_Validate_ZeroAmount(t *testing.T) {
	req := domain.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         0,
	}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "amount must be a positive integer")
}

func TestCreateTransferRequest_Validate_NegativeAmount(t *testing.T) {
	req := domain.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         -50,
	}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "amount must be a positive integer")
}

func TestCreateTransferRequest_Validate_SelfTransfer(t *testing.T) {
	req := domain.CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_a",
		Amount:         100,
	}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "fromWalletId and toWalletId must be different")
}

// ──────────────────────────────────────────────────────────────────────────────
// Transfer state machine tests
// ──────────────────────────────────────────────────────────────────────────────

func TestTransfer_CanTransitionTo_FromPending(t *testing.T) {
	transfer := &domain.Transfer{Status: domain.TransferStatusPending}

	if !transfer.CanTransitionTo(domain.TransferStatusProcessed) {
		t.Error("PENDING should be able to transition to PROCESSED")
	}
	if !transfer.CanTransitionTo(domain.TransferStatusFailed) {
		t.Error("PENDING should be able to transition to FAILED")
	}
}

func TestTransfer_CanTransitionTo_ProcessedIsTerminal(t *testing.T) {
	transfer := &domain.Transfer{Status: domain.TransferStatusProcessed}

	if transfer.CanTransitionTo(domain.TransferStatusPending) {
		t.Error("PROCESSED should not transition to PENDING")
	}
	if transfer.CanTransitionTo(domain.TransferStatusFailed) {
		t.Error("PROCESSED should not transition to FAILED")
	}
	if transfer.CanTransitionTo(domain.TransferStatusProcessed) {
		t.Error("PROCESSED should not transition to PROCESSED")
	}
}

func TestTransfer_CanTransitionTo_FailedIsTerminal(t *testing.T) {
	transfer := &domain.Transfer{Status: domain.TransferStatusFailed}

	if transfer.CanTransitionTo(domain.TransferStatusPending) {
		t.Error("FAILED should not transition to PENDING")
	}
	if transfer.CanTransitionTo(domain.TransferStatusProcessed) {
		t.Error("FAILED should not transition to PROCESSED")
	}
	if transfer.CanTransitionTo(domain.TransferStatusFailed) {
		t.Error("FAILED should not transition to FAILED")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Request hash tests
// ──────────────────────────────────────────────────────────────────────────────

func TestComputeRequestHash_Deterministic(t *testing.T) {
	hash1 := domain.ComputeRequestHash("wallet_a", "wallet_b", 100)
	hash2 := domain.ComputeRequestHash("wallet_a", "wallet_b", 100)
	if hash1 != hash2 {
		t.Errorf("same inputs should produce same hash, got %s and %s", hash1, hash2)
	}
}

func TestComputeRequestHash_DifferentAmounts(t *testing.T) {
	hash1 := domain.ComputeRequestHash("wallet_a", "wallet_b", 100)
	hash2 := domain.ComputeRequestHash("wallet_a", "wallet_b", 200)
	if hash1 == hash2 {
		t.Error("different amounts should produce different hashes")
	}
}

func TestComputeRequestHash_DifferentWallets(t *testing.T) {
	hash1 := domain.ComputeRequestHash("wallet_a", "wallet_b", 100)
	hash2 := domain.ComputeRequestHash("wallet_b", "wallet_a", 100)
	if hash1 == hash2 {
		t.Error("swapped wallets should produce different hashes")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CreateWalletRequest validation tests
// ──────────────────────────────────────────────────────────────────────────────

func TestCreateWalletRequest_Validate_Valid(t *testing.T) {
	req := domain.CreateWalletRequest{ID: "w1", Balance: 100, Currency: "USD"}
	if err := req.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestCreateWalletRequest_Validate_DefaultCurrency(t *testing.T) {
	req := domain.CreateWalletRequest{ID: "w1", Balance: 100}
	if err := req.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if req.Currency != "USD" {
		t.Errorf("expected default currency USD, got %s", req.Currency)
	}
}

func TestCreateWalletRequest_Validate_MissingID(t *testing.T) {
	req := domain.CreateWalletRequest{Balance: 100, Currency: "USD"}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "wallet id is required")
}

func TestCreateWalletRequest_Validate_NegativeBalance(t *testing.T) {
	req := domain.CreateWalletRequest{ID: "w1", Balance: -100, Currency: "USD"}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "initial balance cannot be negative")
}

func TestCreateWalletRequest_Validate_InvalidCurrency(t *testing.T) {
	req := domain.CreateWalletRequest{ID: "w1", Balance: 100, Currency: "US"}
	err := req.Validate()
	assertDomainError(t, err, "INVALID_INPUT", "currency must be a 3-letter ISO code")
}

// ──────────────────────────────────────────────────────────────────────────────
// DomainError tests
// ──────────────────────────────────────────────────────────────────────────────

func TestDomainError_ErrorMethod(t *testing.T) {
	err := domain.ErrWalletNotFound
	if err.Error() != "wallet not found" {
		t.Errorf("expected 'wallet not found', got '%s'", err.Error())
	}
}

func TestNewInvalidInputError(t *testing.T) {
	err := domain.NewInvalidInputError("test message")
	if err.Code != "INVALID_INPUT" {
		t.Errorf("expected code INVALID_INPUT, got %s", err.Code)
	}
	if err.Message != "test message" {
		t.Errorf("expected message 'test message', got '%s'", err.Message)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func assertDomainError(t *testing.T, err error, expectedCode, expectedMsg string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	domainErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if domainErr.Code != expectedCode {
		t.Errorf("expected error code %s, got %s", expectedCode, domainErr.Code)
	}
	if domainErr.Message != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, domainErr.Message)
	}
}
