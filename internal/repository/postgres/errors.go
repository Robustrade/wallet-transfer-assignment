package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// postgresUniqueViolation is the SQLSTATE for a unique_violation error.
const postgresUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == postgresUniqueViolation
	}
	return false
}
