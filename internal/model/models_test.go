package model

import "testing"

func TestTableNames(t *testing.T) {
	if (Transfer{}).TableName() != "transfers" {
		t.Fatalf("unexpected table name for Transfer")
	}
	if (Wallet{}).TableName() != "wallets" {
		t.Fatalf("unexpected table name for Wallet")
	}
	if (LedgerEntry{}).TableName() != "ledger_entries" {
		t.Fatalf("unexpected table name for LedgerEntry")
	}
	if (IdempotencyRecord{}).TableName() != "idempotency_records" {
		t.Fatalf("unexpected table name for IdempotencyRecord")
	}
}
