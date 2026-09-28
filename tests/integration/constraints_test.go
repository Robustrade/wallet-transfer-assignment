package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository/sqlrepo"
)

// TestConstraints proves the database rejects invalid data on its own, with
// raw SQL that bypasses the service and repositories. These are the last
// line of defense if application code ever has a bug.
func TestConstraints(t *testing.T) {
	t.Parallel()

	now := sqlrepo.FormatTime(time.Now())
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }

	insertTransfer := func(e *testEnv, key, from, to string, amount int64, status string, reason any) error {
		return e.exec(
			`INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newID(), key, from, to, amount, status, reason, now, now)
	}

	tests := []struct {
		name string
		run  func(t *testing.T, e *testEnv)
	}{
		{"wallet balance cannot go negative", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			err := e.exec("UPDATE wallets SET balance = -1 WHERE id = ?", "a")
			require.Error(t, err)
			assert.False(t, e.dialect.IsUniqueViolation(err), "a CHECK failure is not a unique violation")
			assert.Equal(t, int64(100), e.balance("a"))
		}},
		{"wallet currency must be 3 characters", func(t *testing.T, e *testEnv) {
			err := e.exec("INSERT INTO wallets (id, balance, currency, created_at, updated_at) VALUES ('x', 0, 'SGDX', ?, ?)", now, now)
			require.Error(t, err)
			assert.Equal(t, int64(0), e.count("wallets"))
		}},
		{"a transfer cannot get a second DEBIT entry", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			id := e.insertProcessedTransfer("a", "b", 10)

			err := e.insertLedgerEntry(id, "a", "DEBIT", 10)
			require.Error(t, err)
			assert.True(t, e.dialect.IsUniqueViolation(err), "want unique violation, got %v", err)
			assert.Equal(t, int64(2), e.count("ledger_entries"))
		}},
		{"ledger entry type must be DEBIT or CREDIT", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			id := e.insertProcessedTransfer("a", "b", 10)
			require.Error(t, e.insertLedgerEntry(id, "a", "REFUND", 10))
		}},
		{"ledger amount must be positive", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			id := e.insertProcessedTransfer("a", "b", 10)
			// Delete the real CREDIT first, so only the amount CHECK can reject the insert.
			require.NoError(t, e.exec("DELETE FROM ledger_entries WHERE transfer_id = ? AND entry_type = 'CREDIT'", id))
			require.Error(t, e.insertLedgerEntry(id, "b", "CREDIT", 0))
			require.NoError(t, e.insertLedgerEntry(id, "b", "CREDIT", 10)) // restore for invariants
		}},
		{"transfer from a wallet to itself is rejected", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			require.Error(t, insertTransfer(e, "k1", "a", "a", 10, "FAILED", "INSUFFICIENT_FUNDS"))
			assert.Equal(t, int64(0), e.count("transfers"))
		}},
		{"transfer amount must be positive", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			require.Error(t, insertTransfer(e, "k1", "a", "b", 0, "FAILED", "INSUFFICIENT_FUNDS"))
			require.Error(t, insertTransfer(e, "k2", "a", "b", -5, "FAILED", "INSUFFICIENT_FUNDS"))
			assert.Equal(t, int64(0), e.count("transfers"))
		}},
		{"transfer status must be in the enum", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			require.Error(t, insertTransfer(e, "k1", "a", "b", 10, "DONE", nil))
			require.Error(t, insertTransfer(e, "k2", "a", "b", 10, "processed", nil))
			assert.Equal(t, int64(0), e.count("transfers"))
		}},
		{"failure reason must be a known value", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			require.Error(t, insertTransfer(e, "k1", "a", "b", 10, "FAILED", "BAD_LUCK"))
			assert.Equal(t, int64(0), e.count("transfers"))
		}},
		{"failure reason is present if and only if status is FAILED", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			require.Error(t, insertTransfer(e, "k1", "a", "b", 10, "FAILED", nil), "FAILED without reason")
			require.Error(t, insertTransfer(e, "k2", "a", "b", 10, "PROCESSED", "INSUFFICIENT_FUNDS"), "PROCESSED with reason")
			assert.Equal(t, int64(0), e.count("transfers"))
		}},
		{"two transfers cannot share an idempotency key", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			require.NoError(t, insertTransfer(e, "same", "a", "b", 10, "FAILED", "INSUFFICIENT_FUNDS"))
			err := insertTransfer(e, "same", "a", "b", 10, "FAILED", "INSUFFICIENT_FUNDS")
			require.Error(t, err)
			assert.True(t, e.dialect.IsUniqueViolation(err), "want unique violation, got %v", err)
			assert.Equal(t, int64(1), e.count("transfers"))
		}},
		{"duplicate idempotency key is rejected", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			t1 := e.insertProcessedTransfer("a", "b", 10)
			t2 := e.insertProcessedTransfer("a", "b", 20)
			hash := "0000000000000000000000000000000000000000000000000000000000000000"
			insertRecord := func(transferID string) error {
				return e.exec(`INSERT INTO idempotency_records ("key", request_hash, transfer_id, created_at) VALUES (?, ?, ?, ?)`,
					"dup", hash, transferID, now)
			}
			require.NoError(t, insertRecord(t1))
			err := insertRecord(t2)
			require.Error(t, err)
			assert.True(t, e.dialect.IsUniqueViolation(err), "want unique violation, got %v", err)
			assert.Equal(t, int64(1), e.count("idempotency_records"))
		}},
		{"foreign key: ledger entry for a non-existent wallet", func(t *testing.T, e *testEnv) {
			// Critical for SQLite, which ignores foreign keys unless
			// PRAGMA foreign_keys=ON is set on every connection.
			e.seedWallet("a", 100, "SGD")
			e.seedWallet("b", 0, "SGD")
			id := e.insertProcessedTransfer("a", "b", 10)
			require.NoError(t, e.exec("DELETE FROM ledger_entries WHERE transfer_id = ? AND entry_type = 'CREDIT'", id))
			require.Error(t, e.insertLedgerEntry(id, "ghost", "CREDIT", 10))
			require.NoError(t, e.insertLedgerEntry(id, "b", "CREDIT", 10)) // restore for invariants
		}},
		{"foreign key: ledger entry for a non-existent transfer", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			require.Error(t, e.insertLedgerEntry(newID(), "a", "DEBIT", 10))
			assert.Equal(t, int64(0), e.count("ledger_entries"))
		}},
		{"foreign key: transfer for a non-existent wallet", func(t *testing.T, e *testEnv) {
			e.seedWallet("a", 100, "SGD")
			require.Error(t, insertTransfer(e, "k1", "a", "ghost", 10, "FAILED", "INSUFFICIENT_FUNDS"))
			require.Error(t, insertTransfer(e, "k2", "ghost", "a", 10, "FAILED", "INSUFFICIENT_FUNDS"))
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forEachDriver(t, func(t *testing.T, driver string) {
				tc.run(t, newEnv(t, driver))
			})
		})
	}
}
