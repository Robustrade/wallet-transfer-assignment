package kafka

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/robustrade/wallet-transfer-assignment/internal/ledger"
	"github.com/robustrade/wallet-transfer-assignment/internal/model"
	"github.com/segmentio/kafka-go"
)

func NewLedgerConsumer(db *sql.DB, repo ledger.LedgerRepository, pub Publisher) *Consumer {
	brokers := brokerListFromEnv()
	topic := "TRANSFER_CREATED"
	groupID := "ledger-consumer"

	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			GroupID:  groupID,
			Topic:    topic,
			MinBytes: 10e3, // 10KB
			MaxBytes: 10e6, // 10MB
		}),
		db:         db,
		ledgerRepo: repo,
		publisher:  pub,
		topic:      topic,
		groupID:    groupID,
	}
}

func (c *Consumer) processLedgerMessage(ctx context.Context) {
	msg, err := c.reader.ReadMessage(ctx)
	if err != nil {
		log.Printf("Error reading message: %v", err)
		return
	}

	log.Printf("Received message: key=%s, offset=%d", string(msg.Key), msg.Offset)

	var payload model.TransferPayload
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		log.Printf("Error unmarshaling payload: %v", err)
		return
	}

	if err := c.processLedgerAndUpdateTransfers(ctx, payload); err != nil {
		log.Printf("Error processing ledger and update: %v", err)
		// Don't commit if processing failed - will retry
		return
	}

	// Commit the message after successful processing
	c.commitMessage(ctx, msg)
}

func (c *Consumer) processLedgerAndUpdateTransfers(ctx context.Context, payload model.TransferPayload) error {
	// Create debit and credit ledger entries
	entries := []model.LedgerEntry{
		{
			Id:         uuid.New().String(),
			TransferId: payload.TransferID,
			WalletId:   payload.FromWalletID,
			Amount:     -payload.Amount, // Debit: negative amount
		},
		{
			Id:         uuid.New().String(),
			TransferId: payload.TransferID,
			WalletId:   payload.ToWalletID,
			Amount:     payload.Amount, // Credit: positive amount
		},
	}

	// Insert ledger entries, update transfer status in a transaction
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if err := c.ledgerRepo.InsertLedgerEntries(ctx, tx, entries); err != nil {
		return fmt.Errorf("failed to insert ledger entries: %w", err)
	}

	if err := c.ledgerRepo.UpdateTransferStatus(ctx, tx, payload.TransferID, "PROCESSED"); err != nil {
		return fmt.Errorf("failed to update transfer status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Publish LEDGER_UPDATED event after successful commit
	updatedPayload := []model.LedgerEntry{
		{
			Id:         entries[0].Id,
			TransferId: payload.TransferID,
			WalletId:   payload.FromWalletID,
			Amount:     -payload.Amount, // Debit: negative amount
		},
		{
			Id:         entries[1].Id,
			TransferId: payload.TransferID,
			WalletId:   payload.ToWalletID,
			Amount:     payload.Amount, // Credit: positive amount
		},
	}

	updatedPayloadBytes, err := json.Marshal(updatedPayload)
	if err != nil {
		log.Printf("Error marshaling LEDGER_UPDATED payload: %v", err)
		return err
	}

	if err := c.publisher.Publish(ctx, payload.TransferID, "LEDGER_UPDATED", updatedPayloadBytes); err != nil {
		log.Printf("Error publishing LEDGER_UPDATED event: %v", err)
		return err
	}

	log.Printf("Successfully processed transfer %s: debited %s, credited %s", payload.TransferID, payload.FromWalletID, payload.ToWalletID)

	return nil
}
