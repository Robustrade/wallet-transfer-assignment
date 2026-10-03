package apitest

import (
	"context"
	"maps"
	"net/http"
	"testing"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/repository"
)

// TestStartupSchemaAndSeed applies the schema and seed the way cmd/server
// does on every start.
func TestStartupSchemaAndSeed(t *testing.T) {
	beginTest(t)

	applyStartup(t)
	applyStartup(t) // a second start must succeed too

	wantOpening := map[string]int64{"wallet_1": 1000, "wallet_2": 1000, "wallet_3": 0}
	for id, want := range wantOpening {
		var opening int64
		err := pool.QueryRow(context.Background(),
			`SELECT opening_balance FROM wallets WHERE id = $1`, id).Scan(&opening)
		if err != nil {
			t.Fatalf("read seed wallet %s: %v", id, err)
		}
		if opening != want {
			t.Errorf("opening balance of %s = %d, want %d", id, opening, want)
		}
	}

	// A restart must not reset balances that transfers have changed.
	postTransfer(t, transferBody(newKey(), "wallet_1", "wallet_2", 1)).transfer(t, http.StatusCreated)
	before := seedBalances(t)
	applyStartup(t)
	if after := seedBalances(t); !maps.Equal(after, before) {
		t.Errorf("balances after restart = %v, want unchanged %v", after, before)
	}
}

// applyStartup applies the schema and seed, as cmd/server does on start.
func applyStartup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := repository.ApplySchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplySeed(ctx, pool); err != nil {
		t.Fatal(err)
	}
}

func seedBalances(t *testing.T) map[string]int64 {
	t.Helper()
	return map[string]int64{
		"wallet_1": balanceOf(t, "wallet_1"),
		"wallet_2": balanceOf(t, "wallet_2"),
		"wallet_3": balanceOf(t, "wallet_3"),
	}
}
