package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"wallet-transfer-assignment/internal/repository"
)

func TestErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		code            string
		conflict, retry bool
	}{
		{"23505", true, false}, {"40P01", false, true}, {"40001", false, true}, {"23503", false, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			original := &pgconn.PgError{Code: tc.code, Message: "secret SQL details"}
			err := wrap("create entry", fmt.Errorf("driver: %w", original))
			if errors.Is(err, repository.ErrConflict) != tc.conflict {
				t.Fatal("wrong conflict classification")
			}
			if errors.Is(err, repository.ErrRetryableTransaction) != tc.retry {
				t.Fatal("wrong retry classification")
			}
			if !errors.Is(err, repository.ErrTransactionFailed) {
				t.Fatal("SQL failure must require rollback")
			}
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg != original {
				t.Fatal("lost driver cause")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("driver details exposed")
			}
		})
	}
	err := wrap("get wallet", sql.ErrNoRows)
	if !errors.Is(err, repository.ErrNotFound) || !errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrTransactionFailed) {
		t.Fatal("wrong missing-row classification")
	}
	if !errors.Is(wrap("query", context.Canceled), context.Canceled) {
		t.Fatal("lost cancellation")
	}
	if wrap("query", nil) != nil {
		t.Fatal("nil error changed")
	}
}
