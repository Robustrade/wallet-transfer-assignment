//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/idgen"
)

var testTransferIDGen = mustTestIDGenerator()

func mustTestIDGenerator() *idgen.Generator {
	gen, err := idgen.NewGenerator(1)
	if err != nil {
		panic(err)
	}
	return gen
}

// newTestWallet inserts a fresh wallet with a unique ID and the given
// balance directly via SQL. Every integration test creates its own
// wallets — never reuses another test's — so concurrent test runs can
// never corrupt each other's expected balances.
func newTestWallet(t *testing.T, balance string) string {
	t.Helper()
	id := uuid.NewString()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := testPool.Exec(ctx, `
		INSERT INTO wallets (id, balance, created_at, updated_at)
		VALUES ($1, $2, now(), now())
	`, id, decimal.RequireFromString(balance))
	if err != nil {
		t.Fatalf("insert test wallet: %v", err)
	}
	return id
}
