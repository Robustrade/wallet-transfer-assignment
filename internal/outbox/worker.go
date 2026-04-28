package outbox

import (
	"context"
	"log"
	"time"

	"github.com/robustrade/wallet-transfer-assignment/internal/kafka"
)

type Worker struct {
	repo      OutboxRepository
	publisher kafka.Publisher
}

func NewWorker(repo OutboxRepository, pub kafka.Publisher) *Worker {
	return &Worker{
		repo:      repo,
		publisher: pub,
	}
}

func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("outbox worker stopped")
			return

		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *Worker) process(ctx context.Context) {
	events, err := w.repo.FetchPending(ctx, 500)
	if err != nil {
		log.Println("fetch error:", err)
		return
	}

	for _, e := range events {
		err := w.publisher.Publish(ctx, e.TransferID, e.EventType, e.Payload)
		if err != nil {
			log.Println("publish failed:", err)
			_ = w.repo.MarkFailed(ctx, e.ID)
			continue
		}

		err = w.repo.MarkSent(ctx, e.ID)
		if err != nil {
			log.Println("mark sent failed:", err)
		}
	}
}
