package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/robustrade/wallet-transfer-assignment/internal/db"
	"github.com/robustrade/wallet-transfer-assignment/internal/model"
	repository "github.com/robustrade/wallet-transfer-assignment/internal/transfer"
)

type TransferService struct {
	db   *sql.DB
	repo repository.TransferRepository
}

func NewTransferService(db *sql.DB, repo repository.TransferRepository) *TransferService {

	return &TransferService{
		db:   db,
		repo: repo,
	}

}

func (s *TransferService) Transfer(ctx context.Context, transfer *model.Transfer) error {
	if err := ValidateTransferRequest(transfer); err != nil {
		return err
	}

	// Check wallet existence
	count, err := s.repo.WalletsExist(ctx, []string{transfer.FromWalletId, transfer.ToWalletId})
	if err != nil {
		return err
	}
	if count != 2 {
		return model.ErrWalletNotFound
	}

	// Generate unique ID for transfer
	transfer.Id = GenerateUUID()

	return db.WithTx(ctx, s.db, func(tx *sql.Tx) error {

		// Insert record in transfer table with PENDING status
		var transferExists bool
		err, transferExists = s.repo.InsertTransfer(ctx, tx, transfer)

		if transferExists {
			err = s.handleExistingTransfer(transfer)
		}
		if err != nil {
			return err
		}

		// Insert outbox entry for async processing
		payload := model.TransferPayload{
			TransferID:   transfer.Id,
			FromWalletID: transfer.FromWalletId,
			ToWalletID:   transfer.ToWalletId,
			Amount:       transfer.Amount,
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		outbox := model.OutboxEntry{
			Id:             uuid.New().String(),
			TransferId:     transfer.Id,
			EventType:      "TRANSFER_CREATED",
			EventStatus:    "PROCESSING",
			TransferStatus: model.StatusPending,
			Payload:        string(payloadBytes),
			CreatedAt:      time.Now(),
		}
		err = s.repo.InsertOutboxEntry(ctx, tx, &outbox)
		if err != nil {
			return err
		}

		return nil

	})

}

func ValidateTransferRequest(transfer *model.Transfer) error {
	if transfer.Amount <= 0 {
		return model.ErrInvalidAmount
	}
	if transfer.FromWalletId == transfer.ToWalletId {
		return model.ErrSameWallet
	}
	if transfer.IdempotencyKey == "" {
		return model.ErrMissingIdempotencyKey
	}
	if transfer.FromWalletId == "" || transfer.ToWalletId == "" {
		return model.ErrMissingWalletID
	}
	return nil
}

func GenerateUUID() string {
	return uuid.New().String()
}

func (s *TransferService) handleExistingTransfer(t *model.Transfer) error {

	switch t.Status {
	case model.StatusProcessed:
		return model.ErrTransferProcessed
	case model.StatusFailed:
		return model.ErrTransferFailedError
	case model.StatusPending:
		return model.ErrTransferPending
	}
	return nil

}
