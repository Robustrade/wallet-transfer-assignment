package outbox

import (
	"context"
)

type Event struct {
	ID         string
	TransferID string
	EventType  string
	Payload    []byte
}

type OutboxRepository interface {
	FetchPending(ctx context.Context, batchSize int) ([]Event, error)
	MarkSent(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string) error
}
