package repository

import (
	"errors"
	"testing"

	"github.com/lib/pq"
	"wallettransfer/internal/domain"
)

// Only missing wallets in the transfer claim are caller errors. Unexpected
// foreign-key failures from other tables must not be masked as HTTP 404.
func TestPostgresForeignKeyErrorClassification(t *testing.T) {
	cases := []struct {
		name           string
		table          string
		constraint     string
		walletNotFound bool
	}{
		{"missing source wallet", "transfers", "transfers_from_wallet_id_fkey", true},
		{"missing destination wallet", "transfers", "transfers_to_wallet_id_fkey", true},
		{"missing ledger transfer", "ledger_entries", "ledger_entries_transfer_id_fkey", false},
		{"missing ledger wallet", "ledger_entries", "ledger_entries_wallet_id_fkey", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pgErr := &pq.Error{Code: "23503", Table: tc.table, Constraint: tc.constraint}
			got := translateError(pgErr)
			if errors.Is(got, domain.ErrWalletNotFound) != tc.walletNotFound {
				t.Fatalf("translateError(%s.%s) = %v; want wallet-not-found=%v", tc.table, tc.constraint, got, tc.walletNotFound)
			}
			if !tc.walletNotFound && !errors.Is(got, pgErr) {
				t.Fatalf("unexpected FK error should retain original cause: %v", got)
			}
		})
	}
}
