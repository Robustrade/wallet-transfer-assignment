package postgres

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/amantiwari/wallet-transfer/internal/domain"
)

func parseUUID(value, entity string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, &domain.NotFoundError{Entity: entity, ID: value}
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func uuidString(value pgtype.UUID) (string, error) {
	if !value.Valid {
		return "", nil
	}
	id, err := uuid.FromBytes(value.Bytes[:])
	if err != nil {
		return "", fmt.Errorf("decode uuid: %w", err)
	}
	return id.String(), nil
}
