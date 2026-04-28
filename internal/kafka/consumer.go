package kafka

import (
	"context"
	"database/sql"
	"log"

	"github.com/robustrade/wallet-transfer-assignment/internal/balance"
	"github.com/robustrade/wallet-transfer-assignment/internal/ledger"
	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	reader      *kafka.Reader
	db          *sql.DB
	ledgerRepo  ledger.LedgerRepository
	balanceRepo balance.BalanceRepository
	publisher   Publisher
	topic       string
	groupID     string
}

func (c *Consumer) StartLedgerConsumer(ctx context.Context) {
	log.Printf("Starting Kafka consumer for topic: %s, group: %s", c.topic, c.groupID)

	for {
		select {
		case <-ctx.Done():
			log.Println("Kafka consumer stopped")
			return
		default:
			c.processLedgerMessage(ctx)
		}
	}
}

func (c *Consumer) StartBalanceConsumer(ctx context.Context) {
	log.Printf("Starting Kafka consumer for topic: %s, group: %s", c.topic, c.groupID)

	for {
		select {
		case <-ctx.Done():
			log.Println("Kafka consumer stopped")
			return
		default:
			c.processBalanceMessage(ctx)
		}
	}
}

func (c *Consumer) commitMessage(ctx context.Context, msg kafka.Message) {
	if err := c.reader.CommitMessages(ctx, msg); err != nil {
		log.Printf("Error committing message: %v", err)
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
