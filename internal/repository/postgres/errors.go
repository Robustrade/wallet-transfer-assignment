package postgres

import (
	"errors"

	"github.com/jackc/pgconn"
)

func IsDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	return false
}
