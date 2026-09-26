package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"wallet/internal/domain"
)

// staysServerSide asserts the error is left alone: no retry-safe sentinel
// matches, so errorResponse turns it into a 500.
func staysServerSide(t *testing.T, got error) {
	t.Helper()
	for _, sentinel := range []error{
		domain.ErrLockTimeout, domain.ErrStatementTimeout, domain.ErrDatabaseUnavailable,
	} {
		if errors.Is(got, sentinel) {
			t.Errorf("error = %v must not match %v (must stay a server-side error)", got, sentinel)
		}
	}
}

// staysCancelled asserts a cancelled client context is preserved rather than
// being reported as a database problem.
func staysCancelled(t *testing.T, got error) {
	t.Helper()
	if !errors.Is(got, context.Canceled) {
		t.Errorf("error = %v, want errors.Is context.Canceled", got)
	}
	if errors.Is(got, domain.ErrDatabaseUnavailable) {
		t.Errorf("error = %v must not be reclassified as database unavailable", got)
	}
}

// TestMapErrorClassification pins the classification table that decides
// retry-safe 503s versus server-side 500s: the stop-and-retry PostgreSQL
// codes, transport failures (dial errors, pool-acquire timeouts), and the
// errors that must NOT be reclassified.
func TestMapErrorClassification(t *testing.T) {
	pgErr := func(code string) *pgconn.PgError {
		return &pgconn.PgError{Code: code, Message: "server says " + code}
	}
	is := func(target error) func(*testing.T, error) {
		return func(t *testing.T, got error) {
			t.Helper()
			if !errors.Is(got, target) {
				t.Fatalf("mapError = %v, want errors.Is %v", got, target)
			}
		}
	}

	cases := []struct {
		name  string
		err   error
		check func(*testing.T, error) // nil: the result must be nil
	}{
		{"nil", nil, nil},
		{"lock timeout (55P03)", pgErr("55P03"), is(domain.ErrLockTimeout)},
		{"statement timeout (57014)", pgErr("57014"), is(domain.ErrStatementTimeout)},
		{"dial failure", errors.New("dial tcp 127.0.0.1:5433: connect: connection refused"), is(domain.ErrDatabaseUnavailable)},
		{"pool acquire timeout", fmt.Errorf("begin transaction: %w", context.DeadlineExceeded), is(domain.ErrDatabaseUnavailable)},
		{"sql error stays unchanged", pgErr("23505"), staysServerSide},
		{"cancelled context stays cancelled", fmt.Errorf("begin transaction: %w", context.Canceled), staysCancelled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapError(tc.err)
			if tc.check == nil {
				if got != nil {
					t.Fatalf("mapError(nil) = %v, want nil", got)
				}
				return
			}
			tc.check(t, got)
		})
	}
}
