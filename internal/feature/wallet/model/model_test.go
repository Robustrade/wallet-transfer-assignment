package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/model"
)

// The GORM models must point at tables the migrations actually create.
func TestTableNamesMatchMigrations(t *testing.T) {
	files, err := filepath.Glob("../../../../migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations not found: %v", err)
	}
	var schema strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		schema.Write(b)
	}

	tables := map[string]string{
		"wallets":             model.Wallet{}.TableName(),
		"transfers":           model.Transfer{}.TableName(),
		"ledger_entries":      model.LedgerEntry{}.TableName(),
		"idempotency_records": model.IdempotencyRecord{}.TableName(),
	}
	for want, got := range tables {
		if got != want {
			t.Errorf("TableName() = %q, want %q", got, want)
		}
		if !strings.Contains(schema.String(), "CREATE TABLE "+want) {
			t.Errorf("no migration creates table %q", want)
		}
	}
}
