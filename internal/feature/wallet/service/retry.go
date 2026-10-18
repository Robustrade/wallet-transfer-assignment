package service

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func isRetryableDBError(err error) bool {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "40P01" || pgErr.Code == "40001"
}
