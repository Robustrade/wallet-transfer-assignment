package testsupport

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reset clears transfers/ledger/idempotency and set seed wallet
// to starting balances. call it at the start of every test.
func Reset(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`truncate transfers, ledger_entries, idempotency_records restart identity cascade`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`update wallets set balance = case id
		   when 'wallet_1' then 100000
		   when 'wallet_2' then 50000
		   when 'wallet_3' then 0 end`); err != nil {
		t.Fatalf("reseed wallets: %v", err)
	}
}
