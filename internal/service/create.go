package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"

	"wallet-transfer-assignment/internal/appErrors"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/postgres"
)

func (s *TransferService) CreateTransfer(ctx context.Context, request domain.TransferRequest) (TransferResult, error) {
	if err := request.Validate(s.maxAmount); err != nil {
		return TransferResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.operationTimeout)
	defer cancel()
	record, err := s.findOrReserve(ctx, request)
	if err != nil {
		return TransferResult{}, processingError(err)
	}
	if record.RequestHash != request.Hash() {
		return TransferResult{}, appErrors.ErrIdempotencyConflict
	}
	result, err := s.ProcessTransfer(ctx, record.TransferID)
	if errors.Is(err, appErrors.ErrTransferTemporarilyUnavailable) && ctx.Err() == nil {
		return s.pendingOrReplay(ctx, record.TransferID, err)
	}
	return result, err
}

func (s *TransferService) findOrReserve(ctx context.Context, request domain.TransferRequest) (domain.IdempotencyRecord, error) {
	db := s.db.WithContext(ctx)
	record, found, err := postgres.FindIdempotencyRecord(db, request.IdempotencyKey)
	if err != nil || found {
		return record, err
	}
	record, created, err := s.reserveTransfer(ctx, request)
	if err != nil || created {
		return record, err
	}
	// reserveTransfer's deferred rollback has removed the losing tentative row.
	record, found, err = postgres.FindIdempotencyRecord(db, request.IdempotencyKey)
	if err != nil {
		return record, err
	}
	if !found {
		return record, appErrors.ErrServiceUnavailable
	}
	return record, nil
}

func (s *TransferService) reserveTransfer(ctx context.Context, request domain.TransferRequest) (domain.IdempotencyRecord, bool, error) {
	var record domain.IdempotencyRecord
	tx := s.db.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		return record, false, tx.Error
	}
	defer func() { _ = tx.Rollback().Error }()
	if err := postgres.SetTransactionTimeouts(tx, min(s.operationTimeout, 2*time.Second), min(s.operationTimeout, 4*time.Second)); err != nil {
		return record, false, err
	}
	if err := postgres.CheckWallets(tx, request.FromWalletID, request.ToWalletID); err != nil {
		return record, false, err
	}
	id, err := postgres.CreatePendingTransfer(tx, request)
	if err != nil {
		return record, false, err
	}
	record = domain.IdempotencyRecord{IdempotencyKey: request.IdempotencyKey, RequestHash: request.Hash(), TransferID: id}
	created, err := postgres.InsertIdempotencyRecord(tx, record)
	if err != nil || !created {
		return record, false, err
	}
	if err := tx.Commit().Error; err != nil {
		return record, false, fmt.Errorf("%w: commit reservation: %w", appErrors.ErrServiceUnavailable, err)
	}
	return record, true, nil
}

func (s *TransferService) pendingOrReplay(ctx context.Context, id string, cause error) (TransferResult, error) {
	db := s.db.WithContext(ctx)
	transfer, err := postgres.FindTransfer(db, id)
	if err != nil {
		return TransferResult{}, cause
	}
	if transfer.Status != domain.Pending {
		return replayResult(db, id)
	}
	body, err := json.Marshal(TransferResponse{TransferID: id, Status: domain.Pending})
	if err != nil {
		return TransferResult{}, err
	}
	return TransferResult{StatusCode: http.StatusAccepted, Body: body}, nil
}

func replayResult(db *gorm.DB, id string) (TransferResult, error) {
	status, body, err := postgres.LoadTransferResponse(db, id)
	if err != nil {
		return TransferResult{}, processingError(err)
	}
	return TransferResult{StatusCode: status, Body: body, Replayed: true}, nil
}
