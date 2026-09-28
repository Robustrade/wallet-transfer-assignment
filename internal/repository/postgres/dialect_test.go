package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestDialect_Rebind(t *testing.T) {
	t.Parallel()
	d := NewDialect(time.Second)

	tests := []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"SELECT * FROM t WHERE id = ?", "SELECT * FROM t WHERE id = $1"},
		{"INSERT INTO t (a, b, c) VALUES (?, ?, ?)", "INSERT INTO t (a, b, c) VALUES ($1, $2, $3)"},
		{"UPDATE t SET a=? WHERE b=? AND c=?", "UPDATE t SET a=$1 WHERE b=$2 AND c=$3"},
		{"?,?,?,?,?,?,?,?,?,?,?", "$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, d.Rebind(tc.in), tc.in)
	}
}

func TestDialect_Settings(t *testing.T) {
	t.Parallel()
	d := NewDialect(time.Second)

	assert.Equal(t, "postgres", d.Name())
	assert.Equal(t, " FOR UPDATE", d.LockClause())
	assert.Equal(t, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, d.TxOptions())
}

func TestDialect_ErrorClassification(t *testing.T) {
	t.Parallel()
	d := NewDialect(time.Second)
	pg := func(code string) error { return &pgconn.PgError{Code: code} }

	tests := []struct {
		name                         string
		err                          error
		retryable, lockTimeout, uniq bool
	}{
		{"deadlock_detected", pg("40P01"), true, false, false},
		{"serialization_failure", pg("40001"), true, false, false},
		{"lock_not_available (lock_timeout)", pg("55P03"), false, true, false},
		{"unique_violation", pg("23505"), false, false, true},
		{"check_violation", pg("23514"), false, false, false},
		{"foreign_key_violation", pg("23503"), false, false, false},
		{"wrapped deadlock", fmt.Errorf("update wallet: %w", pg("40P01")), true, false, false},
		{"multi-wrapped lock timeout", fmt.Errorf("%w: %w", errors.New("unavailable"), pg("55P03")), false, true, false},
		{"not a postgres error", errors.New("boom"), false, false, false},
		{"nil", nil, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.retryable, d.IsRetryable(tc.err), "IsRetryable")
			assert.Equal(t, tc.lockTimeout, d.IsLockTimeout(tc.err), "IsLockTimeout")
			assert.Equal(t, tc.uniq, d.IsUniqueViolation(tc.err), "IsUniqueViolation")
		})
	}
}
