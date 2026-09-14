package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/dbtest"
)

// PostgreSQL error classes these assertions expect.
const (
	sqlstateCheckViolation      = "23514"
	sqlstateUniqueViolation     = "23505"
	sqlstateForeignKeyViolation = "23503"
)

// TestSchemaRejectsInvalidWrites attempts, one at a time, every write the schema
// is supposed to make impossible.
//
// Most of these constraints are backstops: the domain rejects a negative amount
// or a self-transfer long before SQL sees it, so in normal operation they never
// fire. That is exactly why they need a test of their own — nothing else in the
// suite would notice if one were weakened or dropped in a later migration.
//
// Each case asserts the SQLSTATE *and* the constraint name. The names are not
// incidental: the handler reads transfers_from_wallet_fk and
// transfers_to_wallet_fk off a 23503 to decide which wallet to name in a 404, so
// renaming one would quietly turn that 404 into a 500.
func TestSchemaRejectsInvalidWrites(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	source := dbtest.Wallet(t, pool, 1000)
	destination := dbtest.Wallet(t, pool, 0)
	transferID := seedTransfer(t, pool, source, destination)

	tests := []struct {
		name       string
		statement  string
		args       []any
		sqlstate   string
		constraint string
	}{
		{
			name:       "a wallet cannot be created overdrawn",
			statement:  `INSERT INTO wallets (id, balance) VALUES ($1, -1)`,
			args:       []any{"w_" + uuid.NewString()},
			sqlstate:   sqlstateCheckViolation,
			constraint: "wallets_balance_non_negative",
		},
		{
			name:       "a wallet cannot be debited below zero",
			statement:  `UPDATE wallets SET balance = balance - 100000 WHERE id = $1`,
			args:       []any{source},
			sqlstate:   sqlstateCheckViolation,
			constraint: "wallets_balance_non_negative",
		},
		{
			name:       "a transfer cannot be for zero",
			statement:  transferInsert(0, "PENDING", nil),
			args:       transferArgs(source, destination),
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_amount_positive",
		},
		{
			name:       "a transfer cannot be for a negative amount",
			statement:  transferInsert(-1, "PENDING", nil),
			args:       transferArgs(source, destination),
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_amount_positive",
		},
		{
			name:       "a transfer cannot be in an unknown state",
			statement:  transferInsert(100, "SETTLED", nil),
			args:       transferArgs(source, destination),
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_state_valid",
		},
		{
			name:       "a transfer cannot move money to itself",
			statement:  transferInsert(100, "PENDING", nil),
			args:       []any{uuid.NewString(), "key_" + uuid.NewString(), "hash", source, source},
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_distinct_wallets",
		},
		{
			name:       "a failed transfer must carry a reason",
			statement:  transferInsert(100, "FAILED", nil),
			args:       transferArgs(source, destination),
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_failure_reason_matches_state",
		},
		{
			name:       "a failed transfer's reason cannot be blank",
			statement:  transferInsert(100, "FAILED", ptr("   ")),
			args:       transferArgs(source, destination),
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_failure_reason_matches_state",
		},
		{
			name:       "a processed transfer must not carry a reason",
			statement:  transferInsert(100, "PROCESSED", ptr("insufficient funds")),
			args:       transferArgs(source, destination),
			sqlstate:   sqlstateCheckViolation,
			constraint: "transfers_failure_reason_matches_state",
		},
		{
			name:       "a transfer cannot name an unknown source wallet",
			statement:  transferInsert(100, "PENDING", nil),
			args:       []any{uuid.NewString(), "key_" + uuid.NewString(), "hash", "w_absent", destination},
			sqlstate:   sqlstateForeignKeyViolation,
			constraint: "transfers_from_wallet_fk",
		},
		{
			name:       "a transfer cannot name an unknown destination wallet",
			statement:  transferInsert(100, "PENDING", nil),
			args:       []any{uuid.NewString(), "key_" + uuid.NewString(), "hash", source, "w_absent"},
			sqlstate:   sqlstateForeignKeyViolation,
			constraint: "transfers_to_wallet_fk",
		},
		{
			name:      "an idempotency key cannot be reused",
			statement: transferInsert(100, "PENDING", nil),
			// Same key as the seeded transfer.
			args:       []any{uuid.NewString(), seededKey(t, pool, transferID), "hash", source, destination},
			sqlstate:   sqlstateUniqueViolation,
			constraint: "transfers_idempotency_key_unique",
		},
		{
			name: "a transfer cannot carry two debits",
			statement: `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
			            VALUES ($1, $2, 'DEBIT', 100)`,
			// The seeded transfer already has a DEBIT, on the other wallet.
			args:       []any{transferID, destination},
			sqlstate:   sqlstateUniqueViolation,
			constraint: "ledger_entries_one_per_transfer_side",
		},
		{
			name: "a ledger entry cannot exist without its transfer",
			statement: `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
			            VALUES ($1, $2, 'CREDIT', 100)`,
			args:       []any{"transfer_absent", source},
			sqlstate:   sqlstateForeignKeyViolation,
			constraint: "ledger_entries_transfer_fk",
		},
		{
			name: "a ledger entry cannot name an unknown wallet",
			statement: `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
			            VALUES ($1, $2, 'CREDIT', 100)`,
			args:       []any{transferID, "w_absent"},
			sqlstate:   sqlstateForeignKeyViolation,
			constraint: "ledger_entries_wallet_fk",
		},
		{
			name: "a ledger entry cannot be of an unknown type",
			statement: `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
			            VALUES ($1, $2, 'REVERSAL', 100)`,
			args:       []any{transferID, source},
			sqlstate:   sqlstateCheckViolation,
			constraint: "ledger_entries_type_valid",
		},
		{
			name: "a ledger entry cannot be for zero",
			statement: `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
			            VALUES ($1, $2, 'CREDIT', 0)`,
			args:       []any{transferID, source},
			sqlstate:   sqlstateCheckViolation,
			constraint: "ledger_entries_amount_positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tc.statement, tc.args...)
			if err == nil {
				t.Fatal("the database accepted a write it should have rejected")
			}

			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("not a PostgreSQL error: %v", err)
			}
			if pgErr.Code != tc.sqlstate {
				t.Errorf("SQLSTATE is %s, want %s (%v)", pgErr.Code, tc.sqlstate, err)
			}
			if pgErr.ConstraintName != tc.constraint {
				t.Errorf("constraint is %q, want %q", pgErr.ConstraintName, tc.constraint)
			}
		})
	}
}

// seedTransfer inserts one processed transfer with its debit entry, so the
// uniqueness and foreign key cases have something real to collide with.
func seedTransfer(t *testing.T, pool *pgxpool.Pool, source, destination string) string {
	t.Helper()

	id := uuid.NewString()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, transferInsert(100, "PENDING", nil),
		id, "key_"+uuid.NewString(), "hash", source, destination); err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		 VALUES ($1, $2, 'DEBIT', 100)`, id, source); err != nil {
		t.Fatalf("seed ledger entry: %v", err)
	}

	return id
}

func seededKey(t *testing.T, pool *pgxpool.Pool, transferID string) string {
	t.Helper()

	var key string
	if err := pool.QueryRow(context.Background(),
		`SELECT idempotency_key FROM transfers WHERE id = $1`, transferID).Scan(&key); err != nil {
		t.Fatalf("read seeded key: %v", err)
	}

	return key
}

// transferInsert builds an INSERT whose amount, state and failure reason are
// fixed by the case, and whose ids come from args.
func transferInsert(amount int64, state string, reason *string) string {
	failureReason := "NULL"
	if reason != nil {
		// Single quotes: in SQL, double quotes name an identifier rather than a
		// string literal. Doubling any embedded quote keeps the literal valid.
		failureReason = "'" + strings.ReplaceAll(*reason, "'", "''") + "'"
	}

	return fmt.Sprintf(`
		INSERT INTO transfers (id, idempotency_key, request_hash,
		                       from_wallet_id, to_wallet_id, amount, state, failure_reason)
		VALUES ($1, $2, $3, $4, $5, %d, '%s', %s)`, amount, state, failureReason)
}

func transferArgs(source, destination string) []any {
	return []any{uuid.NewString(), "key_" + uuid.NewString(), "hash", source, destination}
}

func ptr(s string) *string { return &s }
