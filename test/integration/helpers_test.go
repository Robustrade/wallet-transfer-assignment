package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/httpapi"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/postgres"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newTestServer builds the real stack — postgres repositories, the service
// layer, and the httpapi router — over db and serves it via a real HTTP
// listener, so these tests exercise the exact code path a production
// request takes.
func newTestServer(t *testing.T, db *postgres.DB) *httptest.Server {
	t.Helper()
	transferSvc := service.NewTransferService(db)
	walletSvc := service.NewWalletService(db)
	router := httpapi.NewRouter(transferSvc, walletSvc, db, testLogger())
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

// resetDB truncates every table this suite touches and re-seeds the
// treasury system wallet, giving each test a clean, known starting state.
func resetDB(t *testing.T, db *postgres.DB) {
	t.Helper()
	ctx := context.Background()
	err := db.Gorm().WithContext(ctx).Exec(`TRUNCATE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE`).Error
	require.NoError(t, err)
	err = db.Gorm().WithContext(ctx).Exec(`INSERT INTO wallets (id, kind, currency, balance_minor) VALUES ('treasury_inr', 'SYSTEM', 'INR', 0)`).Error
	require.NoError(t, err)
}

func postJSON(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

func createWallet(t *testing.T, srv *httptest.Server, id, currency string) {
	t.Helper()
	resp := postJSON(t, srv.URL+"/wallets", map[string]any{"id": id, "currency": currency})
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

// fundWallet transfers amount into walletID from the seeded treasury, using
// a fresh random idempotency key so repeated calls across a test don't
// collide.
func fundWallet(t *testing.T, srv *httptest.Server, walletID string, amount int64) {
	t.Helper()
	resp := postJSON(t, srv.URL+"/transfers", map[string]any{
		"idempotencyKey": "fund-" + walletID + "-" + uuid.NewString(),
		"fromWalletId":   "treasury_inr",
		"toWalletId":     walletID,
		"amount":         amount,
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func getWalletBalance(t *testing.T, db *postgres.DB, id string) int64 {
	t.Helper()
	var balance int64
	err := db.Gorm().WithContext(context.Background()).
		Raw(`SELECT balance_minor FROM wallets WHERE id = ?`, id).Scan(&balance).Error
	require.NoError(t, err)
	return balance
}

func countLedgerEntries(t *testing.T, db *postgres.DB, transferID string) (debits, credits int) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.Gorm().WithContext(ctx).
		Raw(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ? AND entry_type = 'DEBIT'`, transferID).Scan(&debits).Error)
	require.NoError(t, db.Gorm().WithContext(ctx).
		Raw(`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ? AND entry_type = 'CREDIT'`, transferID).Scan(&credits).Error)
	return debits, credits
}

type walletLedgerBalance struct {
	ID         string `gorm:"column:id"`
	Stored     int64  `gorm:"column:balance_minor"`
	FromLedger int64  `gorm:"column:ledger_balance"`
}

// assertInvariants checks the global properties described in
// docs/design.md §3.4 and §3.6, which must hold after every transfer
// attempt regardless of outcome. It is called at the end of every test in
// this package.
func assertInvariants(t *testing.T, db *postgres.DB) {
	t.Helper()
	ctx := context.Background()
	gdb := db.Gorm().WithContext(ctx)

	var walletSum int64
	require.NoError(t, gdb.Raw(`SELECT COALESCE(SUM(balance_minor), 0) FROM wallets`).Scan(&walletSum).Error)
	require.Zero(t, walletSum, "closed ledger: total balance across all wallets must sum to zero")

	var ledgerSum int64
	require.NoError(t, gdb.Raw(`
		SELECT COALESCE(SUM(CASE WHEN entry_type = 'CREDIT' THEN amount_minor ELSE -amount_minor END), 0)
		FROM ledger_entries`).Scan(&ledgerSum).Error)
	require.Zero(t, ledgerSum, "every debit must be matched by an equal credit")

	var balances []walletLedgerBalance
	require.NoError(t, gdb.Raw(`
		SELECT w.id, w.balance_minor,
		       COALESCE(SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_minor
		                          WHEN le.entry_type = 'DEBIT'  THEN -le.amount_minor
		                          ELSE 0 END), 0) AS ledger_balance
		FROM wallets w
		LEFT JOIN ledger_entries le ON le.wallet_id = w.id
		GROUP BY w.id, w.balance_minor`).Scan(&balances).Error)
	for _, b := range balances {
		require.Equal(t, b.Stored, b.FromLedger, "wallet %s: stored balance must equal its ledger sum", b.ID)
	}

	var badProcessed int
	require.NoError(t, gdb.Raw(`
		SELECT COUNT(*) FROM transfers t
		WHERE t.status = 'PROCESSED'
		  AND (
		        (SELECT COUNT(*) FROM ledger_entries le WHERE le.transfer_id = t.id AND le.entry_type = 'DEBIT')  != 1
		     OR (SELECT COUNT(*) FROM ledger_entries le WHERE le.transfer_id = t.id AND le.entry_type = 'CREDIT') != 1
		  )`).Scan(&badProcessed).Error)
	require.Zero(t, badProcessed, "every PROCESSED transfer must have exactly one DEBIT and one CREDIT")

	var failedWithLedger int
	require.NoError(t, gdb.Raw(`
		SELECT COUNT(*) FROM transfers t
		WHERE t.status = 'FAILED' AND EXISTS (SELECT 1 FROM ledger_entries le WHERE le.transfer_id = t.id)
	`).Scan(&failedWithLedger).Error)
	require.Zero(t, failedWithLedger, "a FAILED transfer must have zero ledger entries")

	var negativeUserWallets int
	require.NoError(t, gdb.Raw(`
		SELECT COUNT(*) FROM wallets WHERE kind = 'USER' AND balance_minor < 0
	`).Scan(&negativeUserWallets).Error)
	require.Zero(t, negativeUserWallets, "no USER wallet may go negative")
}
