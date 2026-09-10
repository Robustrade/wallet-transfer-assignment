package test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"wallet-transfer-assignment/internal/domain"
)

func TestTransfer_IdempotentReplay(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Create sender with ₹100.00 and receiver with ₹10.00
	fromID := fmt.Sprintf("sender_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 10000)
	createTestWallet(t, ts, toID, 1000)

	idempKey := fmt.Sprintf("key_%d", time.Now().UnixNano())
	req := domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         2500,
	}

	// 2. Act: First submission -> should execute and create transfer (201 Created)
	status1, headers1, transfer1, errBody1 := sendTransferFull(t, ts, req)
	require.Equal(t, http.StatusCreated, status1)
	require.Nil(t, errBody1)
	require.NotNil(t, transfer1)
	assert.Equal(t, domain.StatusProcessed, transfer1.Status)
	assert.Equal(t, int64(2500), transfer1.Amount)
	assert.Empty(t, headers1.Get("X-Idempotent-Replay"))

	// 3. Act: Resend identical request with the exact same idempotencyKey
	status2, headers2, transfer2, errBody2 := sendTransferFull(t, ts, req)

	// 4. Assert: Replayed request returns 200 OK with X-Idempotent-Replay header
	require.Equal(t, http.StatusOK, status2)
	require.Nil(t, errBody2)
	require.NotNil(t, transfer2)
	assert.Equal(t, "true", headers2.Get("X-Idempotent-Replay"))

	// Assert identical response payload
	assert.Equal(t, transfer1.ID, transfer2.ID)
	assert.Equal(t, transfer1.IdempotencyKey, transfer2.IdempotencyKey)
	assert.Equal(t, transfer1.Amount, transfer2.Amount)
	assert.Equal(t, transfer1.Status, transfer2.Status)

	// 5. Assert: Balances were debited only ONCE (not twice)
	sender := getTestWallet(t, ts, fromID)
	assert.Equal(t, int64(7500), sender.Balance)

	receiver := getTestWallet(t, ts, toID)
	assert.Equal(t, int64(3500), receiver.Balance)

	// 6. Assert: Ledger has exactly 2 entries (one debit, one credit - no duplicate rows)
	ledgerEntries := getTestLedgerEntries(t, pool, transfer1.ID)
	require.Len(t, ledgerEntries, 2)

	// 7. Assert: Idempotency record stored durably in the database
	idempRec := getTestIdempotencyRecord(t, pool, idempKey)
	assert.Equal(t, idempKey, idempRec.Key)
	assert.Equal(t, req.CanonicalHash(), idempRec.RequestHash)
	assert.Equal(t, http.StatusOK, idempRec.ResponseCode)
	assert.NotEmpty(t, idempRec.ResponseBody)
}

func TestTransfer_IdempotencyConflict(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Create sender with ₹100.00 and receiver with ₹10.00
	fromID := fmt.Sprintf("sender_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 10000)
	createTestWallet(t, ts, toID, 1000)

	idempKey := fmt.Sprintf("key_conflict_%d", time.Now().UnixNano())
	originalReq := domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         2000,
	}

	// 2. Act: First submission -> 201 Created
	status1, _, transfer1, _ := sendTransferFull(t, ts, originalReq)
	require.Equal(t, http.StatusCreated, status1)
	require.NotNil(t, transfer1)

	// 3. Act: Resend same idempotencyKey but with different amount (₹50.00 instead of ₹20.00)
	tamperedReq := domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         5000, // modified amount
	}
	status2, _, _, errBody2 := sendTransferFull(t, ts, tamperedReq)

	// 4. Assert: 409 Conflict with IDEMPOTENCY_CONFLICT error code
	require.Equal(t, http.StatusConflict, status2)
	assert.Equal(t, domain.ErrReasonIdempotencyConflict, errBody2["error"])

	// 5. Assert: Sender balance still reflects only the first transfer
	sender := getTestWallet(t, ts, fromID)
	assert.Equal(t, int64(8000), sender.Balance)
}
