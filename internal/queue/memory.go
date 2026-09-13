package queue

import (
	"context"
	"log/slog"

	"github.com/ankit729/wallet-transfer/internal/domain"
)

type Memory struct {
	events chan domain.TransferEvent
}

func NewMemory(buffer int) *Memory {
	return &Memory{events: make(chan domain.TransferEvent, buffer)}
}

func (q *Memory) Publish(ctx context.Context, event domain.TransferEvent) error {
	select {
	case q.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *Memory) Start(ctx context.Context, handler func(context.Context, domain.TransferEvent) error) {
	go func() {
		for {
			select {
			case event := <-q.events:
				if err := handler(ctx, event); err != nil {
					slog.Error("transfer event processing failed",
						"eventType", event.Type, "transferID", event.TransferID, "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
