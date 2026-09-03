package service_test

import (
	"context"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
)

func countRows(t *testing.T, db *database.DB, table string) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func assertLedgerConsistent(t *testing.T, db *database.DB, openingBalances map[string]int64) {
	t.Helper()
	ctx := context.Background()

	var debits, credits int64
	err := db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN entry_type = 'DEBIT'  THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE 0 END), 0)
		FROM ledger_entries`).Scan(&debits, &credits)
	if err != nil {
		t.Fatalf("sum ledger entries: %v", err)
	}
	if debits != credits {
		t.Errorf("ledger does not balance: debits = %d, credits = %d", debits, credits)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT t.status, COUNT(e.id)
		FROM transfers t LEFT JOIN ledger_entries e ON e.transfer_id = t.id
		GROUP BY t.id, t.status`)
	if err != nil {
		t.Fatalf("count entries per transfer: %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			status string
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan entries per transfer: %v", err)
		}
		want := 0
		if status == "PROCESSED" {
			want = 2
		}
		if count != want {
			t.Errorf("%s transfer has %d ledger entries, want %d", status, count, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate entries per transfer: %v", err)
	}

	for walletID, opening := range openingBalances {
		var stored, posted int64
		if err := db.QueryRowContext(ctx, db.Rebind("SELECT balance FROM wallets WHERE id = ?"), walletID).Scan(&stored); err != nil {
			t.Fatalf("select balance of %s: %v", walletID, err)
		}
		err := db.QueryRowContext(ctx, db.Rebind(`
			SELECT COALESCE(SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END), 0)
			FROM ledger_entries WHERE wallet_id = ?`), walletID).Scan(&posted)
		if err != nil {
			t.Fatalf("sum ledger entries of %s: %v", walletID, err)
		}
		if stored != opening+posted {
			t.Errorf("wallet %s balance = %d, but opening %d plus ledger %d = %d",
				walletID, stored, opening, posted, opening+posted)
		}
		if stored < 0 {
			t.Errorf("wallet %s balance = %d, must never be negative", walletID, stored)
		}
	}
}
