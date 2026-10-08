package postgres

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"wallet-transfer-assignment/internal/repository"
)

// dbError hides driver text while preserving inspection through errors.Is/As.
// HTTP handlers must still map repository errors to an explicit public contract.
type dbError struct {
	operation string
	cause     error
}

func (e *dbError) Error() string { return e.operation + ": repository operation failed" }
func (e *dbError) Unwrap() error { return e.cause }
func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	cause := err
	if errors.Is(err, sql.ErrNoRows) {
		cause = errors.Join(repository.ErrNotFound, err)
	} else {
		cause = errors.Join(repository.ErrTransactionFailed, err)
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			switch pg.Code {
			case "23505":
				cause = errors.Join(repository.ErrConflict, cause)
			case "40P01", "40001":
				cause = errors.Join(repository.ErrRetryableTransaction, cause)
			}
		}
	}
	return &dbError{operation: operation, cause: cause}
}
