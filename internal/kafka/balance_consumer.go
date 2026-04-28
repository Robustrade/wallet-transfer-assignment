package kafka

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/robustrade/wallet-transfer-assignment/internal/balance"
	"github.com/robustrade/wallet-transfer-assignment/internal/model"
	"github.com/segmentio/kafka-go"
)

func NewBalanceConsumer(db *sql.DB, repo balance.BalanceRepository) *Consumer {
	brokers := brokerListFromEnv()
	topic := "LEDGER_UPDATED"
	groupID := "balance-consumer"

	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			GroupID:  groupID,
			Topic:    topic,
			MinBytes: 10e3, // 10KB
			MaxBytes: 10e6, // 10MB
		}),
		db:          db,
		balanceRepo: repo,
		topic:       topic,
		groupID:     groupID,
	}
}

func (c *Consumer) processBalanceMessage(ctx context.Context) {
	msg, err := c.reader.ReadMessage(ctx)
	if err != nil {
		log.Printf("Error reading message: %v", err)
		return
	}

	log.Printf("Received message: key=%s, offset=%d", string(msg.Key), msg.Offset)

	var payload []model.LedgerEntry
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		log.Printf("Error unmarshaling payload: %v", err)
		return
	}

	if err := c.updateBalances(ctx, payload); err != nil {
		log.Printf("Error updating balance: %v", err)
		// Don't commit if processing failed - will retry
		return
	}

	// Commit the message after successful processing
	c.commitMessage(ctx, msg)
}

func (c *Consumer) updateBalances(ctx context.Context, payload []model.LedgerEntry) error {

	// Update wallet balances in a transaction
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if err := c.balanceRepo.UpdateBalances(ctx, tx, payload); err != nil {
		return fmt.Errorf("failed to update balances: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Successfully updated balances for entries: %v", payload)

	return nil
}
