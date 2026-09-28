package sqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// Querier is the subset of *sql.DB and *sql.Tx the repositories need. The
// same repository code therefore works inside a transaction (TxManager) or
// directly on the pool (read APIs).
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NewRepos returns all repositories bound to q.
func NewRepos(q Querier, d Dialect) service.Repos {
	return service.Repos{
		Wallets:     &walletRepo{q: q, d: d},
		Transfers:   &transferRepo{q: q, d: d},
		Ledger:      &ledgerRepo{q: q, d: d},
		Idempotency: &idempotencyRepo{q: q, d: d},
	}
}

// TimeLayout is how timestamps are written to the database: UTC with exactly
// six fractional digits. Every value has the same width, so on SQLite (where
// timestamps are TEXT) string order equals time order, and ORDER BY created_at
// works. RFC3339Nano would trim trailing zeros and break that. Postgres parses
// the same string into TIMESTAMPTZ.
const TimeLayout = "2006-01-02T15:04:05.000000Z"

// FormatTime converts t to the database representation.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

// timeScanner reads a timestamp column from either driver: Postgres returns
// time.Time, and SQLite returns the TEXT written by FormatTime.
type timeScanner struct{ dst *time.Time }

func (s timeScanner) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		*s.dst = v.UTC()
		return nil
	case string:
		return s.parse(v)
	case []byte:
		return s.parse(string(v))
	default:
		return fmt.Errorf("cannot scan %T into a timestamp", src)
	}
}

func (s timeScanner) parse(v string) error {
	t, err := time.Parse(TimeLayout, v)
	if err != nil {
		return fmt.Errorf("parse timestamp %q: %w", v, err)
	}
	*s.dst = t
	return nil
}

// scanTime lets callers write rows.Scan(scanTime(&t)).
func scanTime(dst *time.Time) timeScanner { return timeScanner{dst: dst} }

// expectOneRow turns "rows affected != 1" into notFound, for UPDATEs whose
// WHERE clause identifies exactly one row.
func expectOneRow(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return notFound
	}
	return nil
}
