package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

// Postgres SQLSTATE codes this package cares about. Hardcoded rather than
// pulled from a pgerrcode dependency to keep the dependency surface small —
// see docs/design.md and prompt.md §2 for the "four direct dependencies"
// budget.
const (
	pgCodeUniqueViolation     = "23505"
	pgCodeForeignKeyViolation = "23503"
	pgCodeCheckViolation      = "23514"
	pgCodeSerializationFail   = "40001"
	pgCodeDeadlockDetected    = "40P01"
	pgCodeLockNotAvailable    = "55P03"
	pgCodeQueryCanceled       = "57014"
)

// translateError maps a raw pgx/Postgres error to a domain sentinel error
// where one applies. Callers above this package only ever see domain errors
// checkable with errors.Is/errors.As — never a *pgconn.PgError. This is what
// keeps Postgres-specific knowledge out of the service and HTTP layers.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case pgCodeUniqueViolation:
		if pgErr.ConstraintName == "wallets_pkey" {
			return domain.ErrWalletAlreadyExists
		}
		return fmt.Errorf("unexpected unique constraint violation (%s): %w", pgErr.ConstraintName, err)
	case pgCodeForeignKeyViolation:
		return domain.ErrWalletNotFound
	case pgCodeCheckViolation:
		return fmt.Errorf("invariant violated (%s): %w", pgErr.ConstraintName, err)
	case pgCodeLockNotAvailable, pgCodeQueryCanceled:
		return domain.ErrResourceBusy
	case pgCodeSerializationFail, pgCodeDeadlockDetected:
		// Handled by DB.Do's retry loop; if retries are exhausted this still
		// surfaces as a busy-resource condition rather than a raw pg error.
		return domain.ErrResourceBusy
	default:
		return err
	}
}

// isRetryableCode reports whether err is a transient Postgres error that
// DB.Do should retry the whole transaction for.
func isRetryableCode(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgCodeSerializationFail || pgErr.Code == pgCodeDeadlockDetected
}
