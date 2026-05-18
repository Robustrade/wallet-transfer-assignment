package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/candidate/wallet-transfer/internal/domain"
)

type OutboxRepository struct{}

func NewOutboxRepository() *OutboxRepository {
	return &OutboxRepository{}
}

func (r *OutboxRepository) Create(ctx context.Context, tx *sql.Tx, e *domain.OutboxEvent) error {
	const q = `
		INSERT INTO outbox_events (event_type, payload, status)
		VALUES (?, ?, ?)`

	res, err := tx.ExecContext(ctx, q, e.EventType, e.Payload, e.Status)
	if err != nil {
		return fmt.Errorf("outbox event create: %w", err)
	}

	id, _ := res.LastInsertId()
	e.ID = uint64(id)
	return nil
}
