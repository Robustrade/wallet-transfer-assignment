//go:build integration

package service

import (
	"testing"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/idgen"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/testutil"
)

var testTransferIDGen = mustTestIDGenerator()

func mustTestIDGenerator() *idgen.Generator {
	gen, err := idgen.NewGenerator(1)
	if err != nil {
		panic(err)
	}
	return gen
}

func nextTestTransferID(t *testing.T) int64 {
	t.Helper()
	id, err := testTransferIDGen.NextID()
	if err != nil {
		t.Fatalf("mint test transfer id: %v", err)
	}
	return id
}

// newTestWallet inserts a fresh wallet with a unique ID and the given
// balance directly via SQL. Every integration test creates its own
// wallets — never reuses another test's — so concurrent test runs can
// never corrupt each other's expected balances.
func newTestWallet(t *testing.T, balance string) string {
	t.Helper()
	return testutil.NewTestWallet(t, testPool, balance).ID
}
