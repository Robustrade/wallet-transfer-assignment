package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"wallet-transfer-assignment/internal/domain"
)

// createTestWallet creates a wallet via POST /wallets and fails the test if not successful.
func createTestWallet(t *testing.T, ts *httptest.Server, id string, balance int64) domain.Wallet {
	t.Helper()

	body, err := json.Marshal(map[string]any{"id": id, "balance": balance})
	require.NoError(t, err)

	resp, err := ts.Client().Post(ts.URL+"/wallets", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var w domain.Wallet
	err = json.NewDecoder(resp.Body).Decode(&w)
	require.NoError(t, err)
	return w
}

// getTestWallet fetches a wallet via GET /wallets/{id} and returns it.
func getTestWallet(t *testing.T, ts *httptest.Server, id string) domain.Wallet {
	t.Helper()

	resp, err := ts.Client().Get(ts.URL + "/wallets/" + id)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var w domain.Wallet
	err = json.NewDecoder(resp.Body).Decode(&w)
	require.NoError(t, err)
	return w
}

// sendTransfer sends a POST /transfers request and returns the HTTP status and decoded transfer if 201.
func sendTransfer(t *testing.T, ts *httptest.Server, req domain.CreateTransferRequest) (int, *domain.Transfer, map[string]any) {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := ts.Client().Post(ts.URL+"/transfers", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var tr domain.Transfer
		err = json.NewDecoder(resp.Body).Decode(&tr)
		require.NoError(t, err)
		return resp.StatusCode, &tr, nil
	}

	var errBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	return resp.StatusCode, nil, errBody
}

// getTestLedgerEntries queries the database directly for ledger entries belonging to a transfer.
func getTestLedgerEntries(t *testing.T, pool *pgxpool.Pool, transferID string) []domain.LedgerEntry {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY type ASC
	`, transferID)
	require.NoError(t, err)
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var typ string
		err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &typ, &e.Amount, &e.CreatedAt)
		require.NoError(t, err)
		e.Type = domain.LedgerEntryType(typ)
		entries = append(entries, e)
	}
	return entries
}

// getTestTransferByIdempotencyKey queries the database directly for a transfer by its idempotency key.
func getTestTransferByIdempotencyKey(t *testing.T, pool *pgxpool.Pool, key string) domain.Transfer {
	t.Helper()

	var tr domain.Transfer
	var status string
	err := pool.QueryRow(context.Background(), `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, key).Scan(&tr.ID, &tr.IdempotencyKey, &tr.FromWalletID, &tr.ToWalletID, &tr.Amount, &status, &tr.ErrorReason, &tr.CreatedAt, &tr.UpdatedAt)
	require.NoError(t, err)
	tr.Status = domain.TransferStatus(status)
	return tr
}
