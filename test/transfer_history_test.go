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

func TestTransferHistory_GetTransfer_Success(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Create wallets with ₹100.00 (10,000 paise) and ₹50.00 (5,000 paise)
	w1 := fmt.Sprintf("hist_s_%d", time.Now().UnixNano())
	w2 := fmt.Sprintf("hist_r_%d", time.Now().UnixNano())
	createTestWallet(t, ts, w1, 10000)
	createTestWallet(t, ts, w2, 5000)

	idempKey := fmt.Sprintf("hist_k_%d", time.Now().UnixNano())
	status, created, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   w1,
		ToWalletID:     w2,
		Amount:         3500, // ₹35.00
	})
	require.Equal(t, http.StatusCreated, status)
	require.NotNil(t, created)

	// 2. Act: Lookup transfer via GET /transfers/{id}
	getStat, fetched, errBody := getTestTransfer(t, ts, created.ID)

	// 3. Assert: Found and matches created transfer
	require.Equal(t, http.StatusOK, getStat)
	require.Nil(t, errBody)
	require.NotNil(t, fetched)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, idempKey, fetched.IdempotencyKey)
	assert.Equal(t, w1, fetched.FromWalletID)
	assert.Equal(t, w2, fetched.ToWalletID)
	assert.Equal(t, int64(3500), fetched.Amount)
	assert.Equal(t, domain.StatusProcessed, fetched.Status)
	assert.Nil(t, fetched.ErrorReason)
}

func TestTransferHistory_GetTransfer_FailedTransfer(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Create sender with only ₹10.00 (1,000 paise)
	w1 := fmt.Sprintf("fail_s_%d", time.Now().UnixNano())
	w2 := fmt.Sprintf("fail_r_%d", time.Now().UnixNano())
	createTestWallet(t, ts, w1, 1000)
	createTestWallet(t, ts, w2, 5000)

	idempKey := fmt.Sprintf("fail_k_%d", time.Now().UnixNano())
	status, _, errBody := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: idempKey,
		FromWalletID:   w1,
		ToWalletID:     w2,
		Amount:         5000, // ₹50.00 (insufficient)
	})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, domain.ErrReasonInsufficientFunds, errBody["error"])

	// Find the persisted FAILED transfer record in DB
	saved := getTestTransferByIdempotencyKey(t, pool, idempKey)
	require.NotEmpty(t, saved.ID)
	assert.Equal(t, domain.StatusFailed, saved.Status)

	// 2. Act: Query the failed transfer via GET /transfers/{id}
	getStat, fetched, _ := getTestTransfer(t, ts, saved.ID)

	// 3. Assert: Retrieved correctly with FAILED status and error reason
	require.Equal(t, http.StatusOK, getStat)
	require.NotNil(t, fetched)
	assert.Equal(t, saved.ID, fetched.ID)
	assert.Equal(t, domain.StatusFailed, fetched.Status)
	require.NotNil(t, fetched.ErrorReason)
	assert.Equal(t, domain.ErrReasonInsufficientFunds, *fetched.ErrorReason)
}

func TestTransferHistory_GetTransfer_NotFound(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	status, fetched, errBody := getTestTransfer(t, ts, "non_existent_transfer_999")
	require.Equal(t, http.StatusNotFound, status)
	assert.Nil(t, fetched)
	require.NotNil(t, errBody)
	assert.Equal(t, "NOT_FOUND", errBody["error"])
	assert.Equal(t, "transfer not found", errBody["message"])
}

func TestTransferHistory_ListWalletTransfers_SuccessAndOrdering(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	// 1. Arrange: Create 3 wallets
	wA := fmt.Sprintf("wa_%d", time.Now().UnixNano())
	wB := fmt.Sprintf("wb_%d", time.Now().UnixNano())
	wC := fmt.Sprintf("wc_%d", time.Now().UnixNano())

	createTestWallet(t, ts, wA, 100000) // ₹1,000.00
	createTestWallet(t, ts, wB, 50000)  // ₹500.00
	createTestWallet(t, ts, wC, 50000)  // ₹500.00

	// Transfer 1: wA -> wB (debit for wA)
	_, tr1, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: fmt.Sprintf("k1_%d", time.Now().UnixNano()),
		FromWalletID:   wA,
		ToWalletID:     wB,
		Amount:         1000, // ₹10.00
	})
	require.NotNil(t, tr1)
	time.Sleep(15 * time.Millisecond)

	// Transfer 2: wC -> wA (credit for wA)
	_, tr2, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: fmt.Sprintf("k2_%d", time.Now().UnixNano()),
		FromWalletID:   wC,
		ToWalletID:     wA,
		Amount:         2500, // ₹25.00
	})
	require.NotNil(t, tr2)
	time.Sleep(15 * time.Millisecond)

	// Transfer 3: wA -> wC (debit for wA)
	_, tr3, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
		IdempotencyKey: fmt.Sprintf("k3_%d", time.Now().UnixNano()),
		FromWalletID:   wA,
		ToWalletID:     wC,
		Amount:         1500, // ₹15.00
	})
	require.NotNil(t, tr3)

	// 2. Act: Fetch history for wA
	status, history, errBody := listTestWalletTransfers(t, ts, wA, "")

	// 3. Assert: Exactly 3 transfers, reverse-chronological order (tr3, tr2, tr1)
	require.Equal(t, http.StatusOK, status)
	require.Nil(t, errBody)
	require.Len(t, history, 3)

	assert.Equal(t, tr3.ID, history[0].ID)
	assert.Equal(t, int64(1500), history[0].Amount)

	assert.Equal(t, tr2.ID, history[1].ID)
	assert.Equal(t, int64(2500), history[1].Amount)

	assert.Equal(t, tr1.ID, history[2].ID)
	assert.Equal(t, int64(1000), history[2].Amount)
}

func TestTransferHistory_ListWalletTransfers_Pagination(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	wSender := fmt.Sprintf("ps_%d", time.Now().UnixNano())
	wReceiver := fmt.Sprintf("pr_%d", time.Now().UnixNano())

	createTestWallet(t, ts, wSender, 100000)
	createTestWallet(t, ts, wReceiver, 10000)

	var transferIDs []string
	for i := 1; i <= 5; i++ {
		_, tr, _ := sendTransfer(t, ts, domain.CreateTransferRequest{
			IdempotencyKey: fmt.Sprintf("page_k_%d_%d", time.Now().UnixNano(), i),
			FromWalletID:   wSender,
			ToWalletID:     wReceiver,
			Amount:         int64(i * 100),
		})
		require.NotNil(t, tr)
		transferIDs = append(transferIDs, tr.ID)
		time.Sleep(10 * time.Millisecond)
	}
	// transferIDs[0] is oldest (i=1), transferIDs[4] is newest (i=5)

	// Page 1: limit 2, offset 0 -> newest: transferIDs[4], transferIDs[3]
	stat1, page1, _ := listTestWalletTransfers(t, ts, wSender, "limit=2&offset=0")
	require.Equal(t, http.StatusOK, stat1)
	require.Len(t, page1, 2)
	assert.Equal(t, transferIDs[4], page1[0].ID)
	assert.Equal(t, transferIDs[3], page1[1].ID)

	// Page 2: limit 2, offset 2 -> transferIDs[2], transferIDs[1]
	stat2, page2, _ := listTestWalletTransfers(t, ts, wSender, "limit=2&offset=2")
	require.Equal(t, http.StatusOK, stat2)
	require.Len(t, page2, 2)
	assert.Equal(t, transferIDs[2], page2[0].ID)
	assert.Equal(t, transferIDs[1], page2[1].ID)

	// Page 3: limit 2, offset 4 -> oldest: transferIDs[0]
	stat3, page3, _ := listTestWalletTransfers(t, ts, wSender, "limit=2&offset=4")
	require.Equal(t, http.StatusOK, stat3)
	require.Len(t, page3, 1)
	assert.Equal(t, transferIDs[0], page3[0].ID)

	// Page 4: limit 2, offset 6 -> empty
	stat4, page4, _ := listTestWalletTransfers(t, ts, wSender, "limit=2&offset=6")
	require.Equal(t, http.StatusOK, stat4)
	assert.Empty(t, page4)
}

func TestTransferHistory_ListWalletTransfers_WalletNotFound(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	status, history, errBody := listTestWalletTransfers(t, ts, "w_does_not_exist_404", "")
	require.Equal(t, http.StatusNotFound, status)
	assert.Nil(t, history)
	require.NotNil(t, errBody)
	assert.Equal(t, "NOT_FOUND", errBody["error"])
	assert.Equal(t, "wallet not found", errBody["message"])
}

func TestTransferHistory_ListWalletTransfers_EmptyHistory(t *testing.T) {
	ts, pool := setupTestServer(t)
	defer ts.Close()
	defer pool.Close()

	walletID := fmt.Sprintf("empty_hist_%d", time.Now().UnixNano())
	createTestWallet(t, ts, walletID, 5000)

	status, history, errBody := listTestWalletTransfers(t, ts, walletID, "")
	require.Equal(t, http.StatusOK, status)
	require.Nil(t, errBody)
	assert.Empty(t, history)
}
