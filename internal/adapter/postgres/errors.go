package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/amantiwari/wallet-transfer/internal/domain"
)

func mapError(err error, entity, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.NotFoundError{Entity: entity, ID: id}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return &domain.ConflictError{Entity: entity, ID: id}
		case "23503":
			return &domain.NotFoundError{Entity: entity, ID: id}
		case "55P03", "57014":
			return fmt.Errorf("database busy: %w", domain.ErrBusy)
		case "22003":
			return fmt.Errorf("numeric overflow: %w", domain.ErrAmountOverflow)
		case "23514":
			return fmt.Errorf("database constraint: %s: %w", pgErr.Message, domain.ErrInvariant)
		}
	}
	return err
}
