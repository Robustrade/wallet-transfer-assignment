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

func TestTransfer_HappyPath(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Create sender with ₹100.00 (10,000 paise) and receiver with ₹20.00 (2,000 paise)
	fromID := fmt.Sprintf("sender_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 10000)
	createTestWallet(t, ts, toID, 2000)

	// 2. Act: Transfer ₹30.00 (3,000 paise) from sender to receiver
	status, transfer, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: fmt.Sprintf("idemp_%d", time.Now().UnixNano()),
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         3000,
	})

	// 3. Assert: Transfer created successfully with status PROCESSED
	require.Equal(t, http.StatusCreated, status)
	require.NotNil(t, transfer)
	assert.Equal(t, domain.StatusProcessed, transfer.Status)
	assert.Equal(t, int64(3000), transfer.Amount)

	// 4. Assert: Wallet balances updated correctly
	sender := getTestWallet(t, ts, fromID)
	assert.Equal(t, int64(7000), sender.Balance)

	receiver := getTestWallet(t, ts, toID)
	assert.Equal(t, int64(5000), receiver.Balance)

	// 5. Assert: Immutable double-entry ledger records created in database
	ledgerEntries := getTestLedgerEntries(t, pool, transfer.ID)
	require.Len(t, ledgerEntries, 2)

	creditLeg := ledgerEntries[0]
	assert.Equal(t, domain.LedgerEntryCredit, creditLeg.Type)
	assert.Equal(t, toID, creditLeg.WalletID)
	assert.Equal(t, int64(3000), creditLeg.Amount)

	debitLeg := ledgerEntries[1]
	assert.Equal(t, domain.LedgerEntryDebit, debitLeg.Type)
	assert.Equal(t, fromID, debitLeg.WalletID)
	assert.Equal(t, int64(3000), debitLeg.Amount)
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Sender has only ₹5.00 (500 paise)
	fromID := fmt.Sprintf("sender_%d", time.Now().UnixNano())
	toID := fmt.Sprintf("receiver_%d", time.Now().UnixNano())
	createTestWallet(t, ts, fromID, 500)
	createTestWallet(t, ts, toID, 1000)

	idempKey := fmt.Sprintf("idemp_%d", time.Now().UnixNano())

	// 2. Act: Attempt to transfer ₹10.00 (1,000 paise)
	status, _, errBody := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         1000,
	})

	// 3. Assert: 422 Unprocessable Entity with INSUFFICIENT_FUNDS error code
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, domain.ErrReasonInsufficientFunds, errBody["error"])

	// 4. Assert: Balances remain strictly untouched
	assert.Equal(t, int64(500), getTestWallet(t, ts, fromID).Balance)
	assert.Equal(t, int64(1000), getTestWallet(t, ts, toID).Balance)

	// 5. Assert: Transfer record is persisted in DB with FAILED status and error reason
	failedTransfer := getTestTransferByIdempotencyKey(t, pool, idempKey)
	assert.Equal(t, domain.StatusFailed, failedTransfer.Status)
	require.NotNil(t, failedTransfer.ErrorReason)
	assert.Equal(t, domain.ErrReasonInsufficientFunds, *failedTransfer.ErrorReason)
	assert.Equal(t, int64(1000), failedTransfer.Amount)
	assert.Equal(t, fromID, failedTransfer.FromWalletID)
	assert.Equal(t, toID, failedTransfer.ToWalletID)

	// 6. Assert: Zero ledger entries were created for the failed transfer
	ledgerEntries := getTestLedgerEntries(t, pool, failedTransfer.ID)
	assert.Empty(t, ledgerEntries)
}

func TestTransfer_WalletNotFound(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	existingID := fmt.Sprintf("existing_%d", time.Now().UnixNano())
	createTestWallet(t, ts, existingID, 1000)

	status, _, errBody := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: fmt.Sprintf("idemp_%d", time.Now().UnixNano()),
		FromWalletID:   existingID,
		ToWalletID:     "non_existent_wallet",
		Amount:         100,
	})

	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, "WALLET_NOT_FOUND", errBody["error"])
}

func TestTransfer_Validation(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	tests := []struct {
		name string
		req  domain.CreateTransferRequest
	}{
		{
			name: "self transfer",
			req: domain.CreateTransferRequest{
				IdempotencyKey: "k1",
				FromWalletID:   "w1",
				ToWalletID:     "w1",
				Amount:         100,
			},
		},
		{
			name: "zero amount",
			req: domain.CreateTransferRequest{
				IdempotencyKey: "k2",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         0,
			},
		},
		{
			name: "negative amount",
			req: domain.CreateTransferRequest{
				IdempotencyKey: "k3",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         -50,
			},
		},
		{
			name: "empty idempotency key",
			req: domain.CreateTransferRequest{
				IdempotencyKey: "",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _, errBody := sendTransfer(t, ts, tt.req)
			require.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, "VALIDATION_ERROR", errBody["error"])
		})
	}
}
