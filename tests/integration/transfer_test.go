package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferExecution_Success(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	w1 := CreateTestWallet(t, ts, "200.00")
	w2 := CreateTestWallet(t, ts, "50.00")

	reqBody := service.CreateTransferInput{
		FromWalletID: w1.ID.String(),
		ToWalletID:   w2.ID.String(),
		Amount:       "75.50",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	ts.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp service.TransferResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "PROCESSED", resp.Status)
	assert.Equal(t, "75.50", resp.Amount)

	// Verify updated wallet balances in DB
	ctx := context.Background()
	updatedW1, err := ts.WalletService.GetWalletBalance(ctx, w1.ID)
	require.NoError(t, err)
	assert.Equal(t, "124.50", updatedW1.Balance.String())

	updatedW2, err := ts.WalletService.GetWalletBalance(ctx, w2.ID)
	require.NoError(t, err)
	assert.Equal(t, "125.50", updatedW2.Balance.String())

	// Verify Double-Entry Ledger entries
	w1Entries, err := ts.WalletService.GetWalletTransactions(ctx, w1.ID)
	require.NoError(t, err)
	require.Len(t, w1Entries, 1)
	assert.Equal(t, domain.LedgerTypeDebit, w1Entries[0].Type)
	assert.Equal(t, "75.50", w1Entries[0].Amount.String())

	w2Entries, err := ts.WalletService.GetWalletTransactions(ctx, w2.ID)
	require.NoError(t, err)
	require.Len(t, w2Entries, 1)
	assert.Equal(t, domain.LedgerTypeCredit, w2Entries[0].Type)
	assert.Equal(t, "75.50", w2Entries[0].Amount.String())
}

func TestTransferExecution_InsufficientFunds(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	w1 := CreateTestWallet(t, ts, "30.00")
	w2 := CreateTestWallet(t, ts, "100.00")

	reqBody := service.CreateTransferInput{
		FromWalletID: w1.ID.String(),
		ToWalletID:   w2.ID.String(),
		Amount:       "50.00",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	ts.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var errResp map[string]map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, string(domain.ErrCodeInsufficientFunds), errResp["error"]["code"])

	// Verify wallet balances remained untouched
	ctx := context.Background()
	updatedW1, err := ts.WalletService.GetWalletBalance(ctx, w1.ID)
	require.NoError(t, err)
	assert.Equal(t, "30.00", updatedW1.Balance.String())

	updatedW2, err := ts.WalletService.GetWalletBalance(ctx, w2.ID)
	require.NoError(t, err)
	assert.Equal(t, "100.00", updatedW2.Balance.String())
}

func TestTransferExecution_NonExistentWallet(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	w1 := CreateTestWallet(t, ts, "100.00")
	nonExistentID := uuid.New().String()

	reqBody := service.CreateTransferInput{
		FromWalletID: w1.ID.String(),
		ToWalletID:   nonExistentID,
		Amount:       "20.00",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	ts.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
