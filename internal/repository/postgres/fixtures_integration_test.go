//go:build integration

package postgres

import (
	"testing"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/testutil"
)

// newTestWallet inserts a fresh wallet with a unique ID and the given
// balance directly via SQL. Every integration test creates its own
// wallets — never reuses another test's — so concurrent test runs can
// never corrupt each other's expected balances.
func newTestWallet(t *testing.T, balance string) domain.Wallet {
	t.Helper()
	return testutil.NewTestWallet(t, testPool, balance)
}
