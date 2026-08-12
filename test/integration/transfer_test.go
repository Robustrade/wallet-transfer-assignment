package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTransfer_HappyPath(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 1000)

	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	body := decodeJSON(t, resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "PROCESSED", body["status"])
	require.NotNil(t, body["settledAt"])
	transferID, _ := body["id"].(string)
	require.NotEmpty(t, transferID)

	require.Equal(t, int64(900), getWalletBalance(t, db, "wallet_1"))
	require.Equal(t, int64(100), getWalletBalance(t, db, "wallet_2"))

	debits, credits := countLedgerEntries(t, db, transferID)
	require.Equal(t, 1, debits, "exactly one DEBIT entry")
	require.Equal(t, 1, credits, "exactly one CREDIT entry")

	var debitBalanceAfter, creditBalanceAfter int64
	ctx := context.Background()
	require.NoError(t, db.Pool().QueryRow(ctx,
		`SELECT balance_after FROM ledger_entries WHERE transfer_id = $1 AND entry_type = 'DEBIT'`, transferID).Scan(&debitBalanceAfter))
	require.NoError(t, db.Pool().QueryRow(ctx,
		`SELECT balance_after FROM ledger_entries WHERE transfer_id = $1 AND entry_type = 'CREDIT'`, transferID).Scan(&creditBalanceAfter))
	require.Equal(t, int64(900), debitBalanceAfter)
	require.Equal(t, int64(100), creditBalanceAfter)

	assertInvariants(t, db)
}

func TestTransfer_InsufficientFundsPersistsAsFailed(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 50)

	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	body := decodeJSON(t, resp)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	require.Equal(t, "FAILED", body["status"])
	require.Equal(t, "INSUFFICIENT_FUNDS", body["failureCode"])
	require.Nil(t, body["settledAt"])
	transferID, _ := body["id"].(string)

	require.Equal(t, int64(50), getWalletBalance(t, db, "wallet_1"), "balance must be untouched by a failed transfer")

	debits, credits := countLedgerEntries(t, db, transferID)
	require.Zero(t, debits)
	require.Zero(t, credits)

	var status, failureCode string
	require.NoError(t, db.Pool().QueryRow(context.Background(),
		`SELECT status, failure_code FROM transfers WHERE id = $1`, transferID).Scan(&status, &failureCode))
	require.Equal(t, "FAILED", status)
	require.Equal(t, "INSUFFICIENT_FUNDS", failureCode)

	// Replay must return the identical failed result, not retry the debit.
	replayResp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "abc123",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	require.Equal(t, http.StatusUnprocessableEntity, replayResp.StatusCode)
	require.Equal(t, "true", replayResp.Header.Get("Idempotent-Replay"))
	replayBody := decodeJSON(t, replayResp)
	require.Equal(t, body, replayBody, "a replay must return the byte-identical result")

	assertInvariants(t, db)
}

func TestTransfer_RollbackLeavesNothingBehind(t *testing.T) {
	db := requireDB(t)
	resetDB(t, db)
	srv := newTestServer(t, db)

	createWallet(t, srv, "wallet_1", "INR")
	createWallet(t, srv, "wallet_2", "INR")
	fundWallet(t, srv, "wallet_1", 1000)

	// Simulate a transaction that inserts a PENDING transfer under the same
	// key and then rolls back — modeling a crash or infrastructure error
	// mid-settlement (see docs/design.md §8: "nothing is left behind, not
	// even a PENDING row").
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.Pool().Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount_minor, currency, status)
		VALUES (gen_random_uuid(), 'rollback-key', 'deadbeef', 'wallet_1', 'wallet_2', 100, 'INR', 'PENDING')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	// The key must be completely free afterwards: a normal request succeeds.
	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "rollback-key",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	body := decodeJSON(t, resp)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "PROCESSED", body["status"])
	require.Empty(t, resp.Header.Get("Idempotent-Replay"), "this must be a fresh attempt, not a replay")

	assertInvariants(t, db)
}
