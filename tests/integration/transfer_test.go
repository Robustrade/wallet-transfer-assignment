//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// I1 — Happy path over HTTP.
func TestI1_HappyPath(t *testing.T) {
	env := setupTestEnv(t)
	env.seedWallet("wallet_a", "INR", 10_000)
	env.seedWallet("wallet_b", "INR", 0)

	status, body := env.post(t, transferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	})
	require.Equal(t, http.StatusCreated, status, "body=%s", body)

	var resp transferResp
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, "PROCESSED", resp.Status)
	assert.False(t, resp.Replayed)
	assert.Equal(t, int64(100), resp.Amount)
	assert.Equal(t, "INR", resp.Currency)

	// Timestamps must come from the DB, not the zero time.
	assert.False(t, resp.CreatedAt.IsZero(), "createdAt must be populated")
	assert.False(t, resp.UpdatedAt.IsZero(), "updatedAt must be populated")

	// DB state.
	assert.Equal(t, int64(9_900), env.walletBalance("wallet_a"))
	assert.Equal(t, int64(100), env.walletBalance("wallet_b"))
	assert.Equal(t, 1, env.transferCount())
	assert.Equal(t, 2, env.ledgerCount())
}

// I2 — Idempotent replay over HTTP.
func TestI2_IdempotentReplay(t *testing.T) {
	env := setupTestEnv(t)
	env.seedWallet("wallet_a", "INR", 10_000)
	env.seedWallet("wallet_b", "INR", 0)

	body := transferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}

	// First call → 201 Created, replayed=false.
	status1, raw1 := env.post(t, body)
	require.Equal(t, http.StatusCreated, status1)
	var first transferResp
	require.NoError(t, json.Unmarshal(raw1, &first))
	assert.False(t, first.Replayed)

	// Second call → 200 OK, replayed=true, identical id and balances unchanged.
	status2, raw2 := env.post(t, body)
	require.Equal(t, http.StatusOK, status2)
	var second transferResp
	require.NoError(t, json.Unmarshal(raw2, &second))
	assert.True(t, second.Replayed)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, first.Status, second.Status)
	// The replay must echo the canonical currency that was committed on the
	// original request.
	assert.Equal(t, first.Currency, second.Currency)
	assert.Equal(t, "INR", second.Currency)

	// Balances and ledger mutated only once.
	assert.Equal(t, int64(9_900), env.walletBalance("wallet_a"))
	assert.Equal(t, int64(100), env.walletBalance("wallet_b"))
	assert.Equal(t, 1, env.transferCount())
	assert.Equal(t, 2, env.ledgerCount())
}

// I3 — Ledger invariant + reproducibility.
//
// Run N transfers between M wallets, then assert the ledger zero-sum and the
// stored-balance == initial + ledger-net invariants. Replaying the ledger from
// scratch reconstructs the materialized balance.
func TestI3_LedgerInvariantAndReproducibility(t *testing.T) {
	env := setupTestEnv(t)

	const (
		M           = 5
		initialEach = int64(10_000)
		N           = 30
	)

	for i := 0; i < M; i++ {
		env.seedWallet(walletName(i), "INR", initialEach)
	}

	for i := 0; i < N; i++ {
		from := i % M
		to := (i + 1) % M
		amount := int64(10 + (i % 7))

		status, body := env.post(t, transferReq{
			IdempotencyKey: fmt.Sprintf("k-%d", i),
			FromWalletID:   walletName(from),
			ToWalletID:     walletName(to),
			Amount:         amount,
		})
		require.Equalf(t, http.StatusCreated, status, "i=%d body=%s", i, body)
	}

	// Zero-sum: sum of debits == sum of credits.
	assert.Equal(t,
		env.ledgerSumByType("DEBIT"),
		env.ledgerSumByType("CREDIT"),
		"sum(debits) must equal sum(credits)",
	)

	// Per-wallet: stored balance == initial + ledger net.
	for i := 0; i < M; i++ {
		w := walletName(i)
		expected := initialEach + env.ledgerNetForWallet(w)
		assert.Equal(t, expected, env.walletBalance(w),
			"wallet %s: stored balance must equal initial + ledger net", w,
		)
	}

	// Ledger has exactly 2*N rows.
	assert.Equal(t, 2*N, env.ledgerCount())

	// Persisted-currency invariant: every transfer row must carry the canonical
	// INR currency, since the initial INSERT uses an empty placeholder and the
	// canonical currency is written only at outcome time.
	currencies := env.transferCurrencies()
	assert.Equal(t, N, len(currencies), "expected one currency per transfer row")
	for _, c := range currencies {
		assert.Equal(t, "INR", c, "every transfer must persist the canonical currency")
	}
}

// I4 — Insufficient funds returns 422 FAILED with no ledger entries.
func TestI4_InsufficientFunds(t *testing.T) {
	env := setupTestEnv(t)
	env.seedWallet("wallet_a", "INR", 100)
	env.seedWallet("wallet_b", "INR", 0)

	status, body := env.post(t, transferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         9999,
	})
	require.Equal(t, http.StatusUnprocessableEntity, status, "body=%s", body)

	var resp transferResp
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, "FAILED", resp.Status)
	require.NotNil(t, resp.FailureReason)
	assert.Equal(t, "INSUFFICIENT_FUNDS", *resp.FailureReason)

	// The transfer row exists with status FAILED so a retry replays the same outcome.
	assert.Equal(t, "FAILED", env.transferStatusByKey("k1"))
	// No ledger entries created.
	assert.Equal(t, 0, env.ledgerCount())
	// Balances unchanged.
	assert.Equal(t, int64(100), env.walletBalance("wallet_a"))
	assert.Equal(t, int64(0), env.walletBalance("wallet_b"))

	// Replay returns 200 OK with the same FAILED status.
	status2, body2 := env.post(t, transferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         9999,
	})
	require.Equal(t, http.StatusOK, status2, "body=%s", body2)
	var replay transferResp
	require.NoError(t, json.Unmarshal(body2, &replay))
	assert.True(t, replay.Replayed)
	assert.Equal(t, "FAILED", replay.Status)
}

// I5 — Same idempotency key, different body returns 409 IDEMPOTENCY_CONFLICT.
func TestI5_IdempotencyConflict(t *testing.T) {
	env := setupTestEnv(t)
	env.seedWallet("wallet_a", "INR", 10_000)
	env.seedWallet("wallet_b", "INR", 0)

	first := transferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	status1, _ := env.post(t, first)
	require.Equal(t, http.StatusCreated, status1)

	conflict := first
	conflict.Amount = 999

	status2, body2 := env.post(t, conflict)
	require.Equal(t, http.StatusConflict, status2, "body=%s", body2)

	var er errorResp
	require.NoError(t, json.Unmarshal(body2, &er))
	assert.Equal(t, "IDEMPOTENCY_CONFLICT", er.Code)

	// DB state: only the original transfer's effect.
	assert.Equal(t, int64(9_900), env.walletBalance("wallet_a"))
	assert.Equal(t, int64(100), env.walletBalance("wallet_b"))
	assert.Equal(t, 1, env.transferCount())
	assert.Equal(t, 2, env.ledgerCount())
}

func walletName(i int) string { return fmt.Sprintf("wallet_%d", i) }
