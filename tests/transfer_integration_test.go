package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/manish8561/wallet-transfer-assignment/internal/handlers"
	"github.com/manish8561/wallet-transfer-assignment/internal/repositories"
	"github.com/manish8561/wallet-transfer-assignment/internal/services"

	"gorm.io/gorm"
)

func TestHappyPath(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create wallets
	w1, err := helper.CreateWallet(200)
	if err != nil {
		t.Fatalf("Failed to create wallet 1: %v", err)
	}
	w2, err := helper.CreateWallet(0)
	if err != nil {
		t.Fatalf("Failed to create wallet 2: %v", err)
	}

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Execute transfer
	response, err := service.ExecuteTransfer(ctx, "test-key-1", w1, w2, 100)
	if err != nil {
		t.Fatalf("Transfer failed: %v", err)
	}

	// Verify response
	if response.Status != "PROCESSED" {
		t.Errorf("Expected status PROCESSED, got %s", response.Status)
	}
	if response.Amount != 100 {
		t.Errorf("Expected amount 100, got %d", response.Amount)
	}

	// Verify wallet balances
	b1, _ := helper.GetWalletBalance(w1)
	b2, _ := helper.GetWalletBalance(w2)
	if b1 != 100 {
		t.Errorf("Expected w1 balance 100, got %d", b1)
	}
	if b2 != 100 {
		t.Errorf("Expected w2 balance 100, got %d", b2)
	}

	// Verify ledger entries
	entries, _ := helper.GetLedgerEntries(response.TransferID)
	if len(entries) != 2 {
		t.Errorf("Expected 2 ledger entries, got %d", len(entries))
	}

	// Verify ledger balance
	balanced, _ := helper.VerifyLedgerBalance()
	if !balanced {
		t.Error("Ledger is not balanced")
	}

	t.Log("✓ Happy path test passed")
}

func TestIdempotency(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create wallets
	w1, _ := helper.CreateWallet(200)
	w2, _ := helper.CreateWallet(0)

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Execute transfer twice with same idempotency key
	resp1, err := service.ExecuteTransfer(ctx, "idempotent-key", w1, w2, 50)
	if err != nil {
		t.Fatalf("First transfer failed: %v", err)
	}

	resp2, err := service.ExecuteTransfer(ctx, "idempotent-key", w1, w2, 50)
	if err != nil {
		t.Fatalf("Second transfer failed: %v", err)
	}

	// Verify same transfer ID returned
	if resp1.TransferID != resp2.TransferID {
		t.Errorf("Expected same transfer ID, got %s and %s", resp1.TransferID, resp2.TransferID)
	}

	// Verify only 2 ledger entries (not 4)
	entries, _ := helper.GetLedgerEntries(resp1.TransferID)
	if len(entries) != 2 {
		t.Errorf("Expected 2 ledger entries for idempotent request, got %d", len(entries))
	}

	// Verify balance decreased only once
	b1, _ := helper.GetWalletBalance(w1)
	if b1 != 150 {
		t.Errorf("Expected w1 balance 150 (transferred 50 once), got %d", b1)
	}

	t.Log("✓ Idempotency test passed")
}

func TestInsufficientBalance(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create wallets
	w1, _ := helper.CreateWallet(100)
	w2, _ := helper.CreateWallet(0)

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Try to transfer more than available
	_, err = service.ExecuteTransfer(ctx, "fail-key", w1, w2, 150)
	if err == nil {
		t.Fatal("Expected transfer to fail due to insufficient balance")
	}

	// Verify no ledger entries created
	count, _ := helper.CountLedgerEntries()
	if count != 0 {
		t.Errorf("Expected 0 ledger entries after failed transfer, got %d", count)
	}

	// Verify balance unchanged
	b1, _ := helper.GetWalletBalance(w1)
	if b1 != 100 {
		t.Errorf("Expected w1 balance unchanged at 100, got %d", b1)
	}

	t.Log("✓ Insufficient balance test passed")
}

func TestConcurrentTransfers(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create wallets: source with 200, two targets with 0
	source, _ := helper.CreateWallet(200)
	target1, _ := helper.CreateWallet(0)
	target2, _ := helper.CreateWallet(0)

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Execute two concurrent transfers from same source wallet
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := service.ExecuteTransfer(ctx, "concurrent-1", source, target1, 100)
		mu.Lock()
		if err != nil {
			errs = append(errs, err)
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		_, err := service.ExecuteTransfer(ctx, "concurrent-2", source, target2, 100)
		mu.Lock()
		if err != nil {
			errs = append(errs, err)
		}
		mu.Unlock()
	}()

	wg.Wait()

	// At least one transfer should succeed (both can succeed if no race condition)
	if len(errs) > 1 {
		t.Errorf("Too many transfer failures: %v", errs)
	}

	// Verify final balance is correct
	sourceBal, _ := helper.GetWalletBalance(source)
	t1Bal, _ := helper.GetWalletBalance(target1)
	t2Bal, _ := helper.GetWalletBalance(target2)

	total := sourceBal + t1Bal + t2Bal
	if total != 200 {
		t.Errorf("Total balance incorrect: expected 200, got %d (source=%d, t1=%d, t2=%d)",
			total, sourceBal, t1Bal, t2Bal)
	}

	// Verify no negative balances
	if sourceBal < 0 || t1Bal < 0 || t2Bal < 0 {
		t.Errorf("Negative balance detected: source=%d, t1=%d, t2=%d", sourceBal, t1Bal, t2Bal)
	}

	t.Log("✓ Concurrent transfers test passed")
}

func TestLedgerConsistency(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create wallets
	w1, _ := helper.CreateWallet(1000)
	w2, _ := helper.CreateWallet(500)
	w3, _ := helper.CreateWallet(300)

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Execute multiple transfers
	transfers := [][4]interface{}{
		{"key1", w1, w2, int64(100)},
		{"key2", w2, w3, int64(50)},
		{"key3", w1, w3, int64(75)},
		{"key4", w3, w1, int64(25)},
	}

	for _, transfer := range transfers {
		key := transfer[0].(string)
		from := transfer[1].(string)
		to := transfer[2].(string)
		amt := transfer[3].(int64)
		_, err := service.ExecuteTransfer(ctx, key, from, to, amt)
		if err != nil {
			t.Fatalf("Transfer failed: %v", err)
		}
	}

	// Verify ledger consistency
	balanced, _ := helper.VerifyLedgerBalance()
	if !balanced {
		t.Error("Ledger is not balanced after multiple transfers")
	}

	t.Log("✓ Ledger consistency test passed")
}

func TestInvalidWallet(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create only one wallet
	w1, _ := helper.CreateWallet(200)

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Try to transfer to non-existent wallet
	transferID := "invalid-key"
	_, err = service.ExecuteTransfer(ctx, transferID, w1, "non-existent", 50)
	if err == nil {
		t.Fatal("Expected transfer to fail due to non-existent wallet")
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !bytes.Contains([]byte(err.Error()), []byte("wallet not found")) {
		t.Fatalf("Expected 'wallet not found' error, got: %v", err)
	}
	// Verify that the transfer record itself is not committed (due to transaction rollback)
	_, err = helper.GetTransfer(transferID)
	if err == nil {
		t.Fatalf("Expected transfer record %s not to exist after rollback, but it does", transferID)
	}

	// Verify no ledger entries created
	count, _ := helper.CountLedgerEntries()
	if count != 0 {
		t.Errorf("Expected 0 ledger entries, got %d", count)
	}

	t.Log("✓ Invalid wallet test passed")
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	w1, _ := helper.CreateWallet(200)

	// Initialize service
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	testCases := []struct {
		name      string
		from      string
		to        string
		amount    int64
		shouldErr bool
	}{
		{"Same wallet", w1, w1, 100, true},
		{"Zero amount", w1, "other", 0, true},
		{"Negative amount", w1, "other", -50, true},
		{"Valid transfer to non-existent wallet", w1, "valid-other", 50, true}, // Should fail on wallet lookup
	}

	for _, tc := range testCases {
		_, err := service.ExecuteTransfer(ctx, "validation-"+tc.name, tc.from, tc.to, tc.amount)
		hasErr := err != nil
		if hasErr != tc.shouldErr {
			t.Errorf("Test %s: expected error=%v, got error=%v (%v)", tc.name, tc.shouldErr, hasErr, err)
		}
	}

	t.Log("✓ Validation tests passed")
}

func TestHTTPHandler(t *testing.T) {
	ctx := context.Background()
	helper, err := SetupTestDB(ctx)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	helper.ClearAllTables() // Ensure a clean state for each test
	defer helper.Cleanup()

	// Create wallets
	w1, _ := helper.CreateWallet(200)
	w2, _ := helper.CreateWallet(0)

	// Initialize service and handler
	walletRepo := repositories.NewWalletRepository(helper.DB)
	transferRepo := repositories.NewTransferRepository(helper.DB)
	ledgerRepo := repositories.NewLedgerRepository(helper.DB)
	idempotencyRepo := repositories.NewIdempotencyRepository(helper.DB)
	service := services.NewTransferService(helper.DB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	handler := handlers.NewTransferHandler(service)

	// Test request
	req := map[string]interface{}{
		"idempotencyKey": "http-test-1",
		"fromWalletId":   w1,
		"toWalletId":     w2,
		"amount":         100,
	}

	reqBody, _ := json.Marshal(req)
	httpReq := &http.Request{
		Body: io.NopCloser(bytes.NewReader(reqBody)),
	}

	// Mock response writer
	var respBuf bytes.Buffer
	mockRW := &mockResponseWriter{
		Header_: make(http.Header),
		buf:     &respBuf,
	}

	// Call handler
	handler.CreateTransfer(mockRW, httpReq.WithContext(ctx))

	// Verify response
	if mockRW.statusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", mockRW.statusCode)
	}

	var respData map[string]interface{}
	json.Unmarshal(respBuf.Bytes(), &respData)
	if respData["status"] != "PROCESSED" {
		t.Errorf("Expected status PROCESSED in response, got %v", respData["status"])
	}

	t.Log("✓ HTTP handler test passed")
}

// mockResponseWriter implements http.ResponseWriter for testing
type mockResponseWriter struct {
	Header_    http.Header
	statusCode int
	buf        *bytes.Buffer
}

func (m *mockResponseWriter) Header() http.Header {
	return m.Header_
}

func (m *mockResponseWriter) Write(data []byte) (int, error) {
	return m.buf.Write(data)
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}
