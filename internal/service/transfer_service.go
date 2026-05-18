package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

const (
	StatusOK                  = 200
	StatusCreated             = 201
	StatusUnprocessableEntity = 422
)

type TransferService struct {
	store repository.Store
}

func NewTransferService(store repository.Store) *TransferService {
	return &TransferService{
		store: store,
	}
}

type CreateTransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type CreateTransferResponse struct {
	TransferID   string `json:"transferId,omitempty"`
	Status       string `json:"status"`
	FromWalletID string `json:"fromWalletId,omitempty"`
	ToWalletID   string `json:"toWalletId,omitempty"`
	Amount       int64  `json:"amount,omitempty"`
	Error        string `json:"error,omitempty"`
}

type CreateTransferResult struct {
	StatusCode int
	Response   CreateTransferResponse
	Replayed   bool
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	cmd CreateTransferCommand,
) (*CreateTransferResult, error) {
	transfer, err := domain.NewTransfer(
		cmd.IdempotencyKey,
		cmd.FromWalletID,
		cmd.ToWalletID,
		cmd.Amount,
	)
	if err != nil {
		return nil, err
	}

	requestHash, err := hashTransferRequest(cmd)
	if err != nil {
		return nil, err
	}

	var result *CreateTransferResult

	err = s.store.WithTx(ctx, func(ctx context.Context, repo repository.Repository) error {
		idempotencyCreated, err := repo.TryCreateIdempotencyRecord(
			ctx,
			cmd.IdempotencyKey,
			requestHash,
		)
		if err != nil {
			return err
		}

		if !idempotencyCreated {
			existingResult, err := s.handleExistingIdempotencyRecord(
				ctx,
				repo,
				cmd.IdempotencyKey,
				requestHash,
			)
			if err != nil {
				return err
			}

			result = existingResult
			return nil
		}

		if err := repo.CreateTransfer(ctx, transfer); err != nil {
			return err
		}

		wallets, err := repo.LockWalletsForUpdate(
			ctx,
			[]string{cmd.FromWalletID, cmd.ToWalletID},
		)
		if err != nil {
			return err
		}

		fromWallet := wallets[cmd.FromWalletID]
		toWallet := wallets[cmd.ToWalletID]

		if err := fromWallet.Debit(cmd.Amount); err != nil {
			failedResult, failErr := s.failTransfer(
				ctx,
				repo,
				transfer,
				err.Error(),
			)
			if failErr != nil {
				return failErr
			}

			result = failedResult
			return nil
		}

		if err := toWallet.Credit(cmd.Amount); err != nil {
			return err
		}

		debitEntry, err := domain.NewDebitEntry(
			cmd.FromWalletID,
			transfer.ID,
			cmd.Amount,
		)
		if err != nil {
			return err
		}

		creditEntry, err := domain.NewCreditEntry(
			cmd.ToWalletID,
			transfer.ID,
			cmd.Amount,
		)
		if err != nil {
			return err
		}

		if err := repo.UpdateWalletBalance(ctx, fromWallet.ID, fromWallet.Balance); err != nil {
			return err
		}

		if err := repo.UpdateWalletBalance(ctx, toWallet.ID, toWallet.Balance); err != nil {
			return err
		}

		if err := repo.InsertLedgerEntries(ctx, debitEntry, creditEntry); err != nil {
			return err
		}

		if err := transfer.MarkProcessed(); err != nil {
			return err
		}

		if err := repo.UpdateTransferStatus(
			ctx,
			transfer.ID,
			domain.TransferStatusProcessed,
			nil,
		); err != nil {
			return err
		}

		response := CreateTransferResponse{
			TransferID:   transfer.ID,
			Status:       string(domain.TransferStatusProcessed),
			FromWalletID: cmd.FromWalletID,
			ToWalletID:   cmd.ToWalletID,
			Amount:       cmd.Amount,
		}

		responseBody, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("marshal transfer response: %w", err)
		}

		if err := repo.CompleteIdempotencyRecord(
			ctx,
			cmd.IdempotencyKey,
			transfer.ID,
			StatusCreated,
			responseBody,
		); err != nil {
			return err
		}

		result = &CreateTransferResult{
			StatusCode: StatusCreated,
			Response:   response,
			Replayed:   false,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *TransferService) handleExistingIdempotencyRecord(
	ctx context.Context,
	repo repository.Repository,
	idempotencyKey string,
	requestHash string,
) (*CreateTransferResult, error) {
	record, err := repo.GetIdempotencyRecordForUpdate(ctx, idempotencyKey)
	if err != nil {
		return nil, err
	}

	if record.RequestHash != requestHash {
		return nil, domain.ErrIdempotencyConflict
	}

	if record.ResponseCode == nil || len(record.ResponseBody) == 0 {
		return nil, domain.ErrIdempotencyInProgress
	}

	var response CreateTransferResponse
	if err := json.Unmarshal(record.ResponseBody, &response); err != nil {
		return nil, fmt.Errorf("unmarshal stored idempotency response: %w", err)
	}

	return &CreateTransferResult{
		StatusCode: *record.ResponseCode,
		Response:   response,
		Replayed:   true,
	}, nil
}

func (s *TransferService) failTransfer(
	ctx context.Context,
	repo repository.Repository,
	transfer *domain.Transfer,
	reason string,
) (*CreateTransferResult, error) {
	if err := transfer.MarkFailed(reason); err != nil {
		return nil, err
	}

	if err := repo.UpdateTransferStatus(
		ctx,
		transfer.ID,
		domain.TransferStatusFailed,
		transfer.FailureReason,
	); err != nil {
		return nil, err
	}

	response := CreateTransferResponse{
		TransferID:   transfer.ID,
		Status:       string(domain.TransferStatusFailed),
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		Error:        reason,
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("marshal failed transfer response: %w", err)
	}

	if err := repo.FailIdempotencyRecord(
		ctx,
		transfer.IdempotencyKey,
		transfer.ID,
		StatusUnprocessableEntity,
		responseBody,
	); err != nil {
		return nil, err
	}

	return &CreateTransferResult{
		StatusCode: StatusUnprocessableEntity,
		Response:   response,
		Replayed:   false,
	}, nil
}

func hashTransferRequest(cmd CreateTransferCommand) (string, error) {
	payload := struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{
		FromWalletID: cmd.FromWalletID,
		ToWalletID:   cmd.ToWalletID,
		Amount:       cmd.Amount,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request hash payload: %w", err)
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:]), nil
}
