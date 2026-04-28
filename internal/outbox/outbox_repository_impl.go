package outbox

import (
	"context"
	"database/sql"
)

type outboxRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) OutboxRepository {
	return &outboxRepository{db: db}
}

func (r *outboxRepository) FetchPending(ctx context.Context, batchSize int) ([]Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT id, transfer_id, event_type, payload
		FROM outbox
		WHERE event_status = 'PROCESSING'
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`

	rows, err := tx.QueryContext(ctx, query, batchSize)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer rows.Close()

	var events []Event

	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TransferID, &e.EventType, &e.Payload); err != nil {
			tx.Rollback()
			return nil, err
		}
		events = append(events, e)
	}

	// mark claimed rows as PROCESSING inside the same transaction
	for _, e := range events {
		_, err := tx.ExecContext(ctx,
			`UPDATE outbox SET event_status = 'PROCESSING' WHERE id = $1`, e.ID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return events, nil
}

func (r *outboxRepository) MarkSent(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox
		SET event_status = 'SENT', processed_at = NOW()
		WHERE id = $1
	`, id)
	return err
}

func (r *outboxRepository) MarkFailed(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox
		SET event_status = 'FAILED'
		WHERE id = $1
	`, id)
	return err
}
