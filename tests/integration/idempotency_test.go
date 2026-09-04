package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdempotency_DuplicateRequestReturnsOriginalResponse(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	w1 := CreateTestWallet(t, ts, "500.00")
	w2 := CreateTestWallet(t, ts, "100.00")

	idemKey := "key_unique_12345"
	reqBody := service.CreateTransferInput{
		IdempotencyKey: &idemKey,
		FromWalletID:   w1.ID.String(),
		ToWalletID:     w2.ID.String(),
		Amount:         "100.00",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// First Request
	w1Recorder := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
	req1.Header.Set("Content-Type", "application/json")

	ts.Router.ServeHTTP(w1Recorder, req1)
	assert.Equal(t, http.StatusCreated, w1Recorder.Code)

	var resp1 service.TransferResponse
	err := json.Unmarshal(w1Recorder.Body.Bytes(), &resp1)
	require.NoError(t, err)

	// Second Identical Request with same idempotency key
	w2Recorder := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes))
	req2.Header.Set("Content-Type", "application/json")

	ts.Router.ServeHTTP(w2Recorder, req2)

	// Must return original response payload
	assert.Equal(t, http.StatusCreated, w2Recorder.Code)

	var resp2 service.TransferResponse
	err = json.Unmarshal(w2Recorder.Body.Bytes(), &resp2)
	require.NoError(t, err)

	assert.Equal(t, resp1.TransferID, resp2.TransferID)
	assert.Equal(t, resp1.Amount, resp2.Amount)

	// Verify balance was only deducted ONCE ($500 - $100 = $400)
	updatedW1, err := ts.WalletService.GetWalletBalance(nil, w1.ID)
	require.NoError(t, err)
	assert.Equal(t, "400.00", updatedW1.Balance.String())
}

func TestIdempotency_KeyReusedWithDifferentPayloadReturnsConflict(t *testing.T) {
	ts := SetupTestSuite(t)
	if ts == nil {
		return
	}
	defer ts.DB.Close()

	w1 := CreateTestWallet(t, ts, "500.00")
	w2 := CreateTestWallet(t, ts, "100.00")

	idemKey := "key_reused_conflict"

	// Request 1: $50
	reqBody1 := service.CreateTransferInput{
		IdempotencyKey: &idemKey,
		FromWalletID:   w1.ID.String(),
		ToWalletID:     w2.ID.String(),
		Amount:         "50.00",
	}
	bodyBytes1, _ := json.Marshal(reqBody1)

	w1Recorder := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	ts.Router.ServeHTTP(w1Recorder, req1)
	assert.Equal(t, http.StatusCreated, w1Recorder.Code)

	// Request 2: Reusing SAME idempotency key but with DIFFERENT amount ($200.00)
	reqBody2 := service.CreateTransferInput{
		IdempotencyKey: &idemKey,
		FromWalletID:   w1.ID.String(),
		ToWalletID:     w2.ID.String(),
		Amount:         "200.00",
	}
	bodyBytes2, _ := json.Marshal(reqBody2)

	w2Recorder := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/transfers", bytes.NewBuffer(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	ts.Router.ServeHTTP(w2Recorder, req2)

	// Must return 409 Conflict with IDEMPOTENCY_KEY_REUSED error code
	assert.Equal(t, http.StatusConflict, w2Recorder.Code)

	var errResp map[string]map[string]string
	err := json.Unmarshal(w2Recorder.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, string(domain.ErrCodeIdempotencyKeyReused), errResp["error"]["code"])
}
