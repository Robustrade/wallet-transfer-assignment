package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/postgres"
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (r *TransferRequest) Validate() error {
	t := model.Transfer{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         r.Amount,
	}
	return t.Validate()
}

type TransferResponse struct {
	TransferID     string               `json:"transferId"`
	IdempotencyKey string               `json:"idempotencyKey"`
	FromWalletID   string               `json:"fromWalletId"`
	ToWalletID     string               `json:"toWalletId"`
	Amount         int64                `json:"amount"`
	Status         model.TransferStatus `json:"status"`
	FailureReason  *string              `json:"failureReason,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
	LedgerEntries  []model.LedgerEntry  `json:"ledgerEntries,omitempty"`
}

type TransferService struct {
	repo repository.Repository
}

func NewTransferService(repo repository.Repository) *TransferService {
	return &TransferService{repo: repo}
}

// ExecuteTransfer processes a wallet-to-wallet transfer with atomic ACID transactions,
// exactly-once idempotency guarantees, row-level locking, and double-entry bookkeeping.
func (s *TransferService) ExecuteTransfer(ctx context.Context, req TransferRequest) (*TransferResponse, int, error) {
	// 1. Early Pre-Lock Validation
	if err := req.Validate(); err != nil {
		return nil, 400, err
	}

	payloadHash := model.ComputePayloadHash(req.FromWalletID, req.ToWalletID, req.Amount)

	// 2. Check Idempotency Record
	existingRecord, err := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, 500, fmt.Errorf("failed to check idempotency key: %w", err)
	}

	if existingRecord != nil {
		// Verify if the payload hash matches the original request
		if existingRecord.RequestHash != payloadHash {
			return nil, 409, model.ErrIdempotencyConflict
		}

		// Replay cached response with its original status code
		var cachedResp TransferResponse
		if jsonErr := json.Unmarshal([]byte(existingRecord.ResponseBody), &cachedResp); jsonErr == nil {
			return &cachedResp, existingRecord.ResponseStatus, nil
		}
		// If cached body was an error message
		return nil, existingRecord.ResponseStatus, errors.New(existingRecord.ResponseBody)
	}

	// 3. Execute Transaction
	var finalResponse TransferResponse
	var finalStatus int
	var executionErr error

	txErr := s.repo.ExecTx(ctx, func(tx *sql.Tx) error {
		// Acquire row locks in deterministic lexicographical order to prevent deadlocks
		wFrom, wTo, lockErr := s.repo.GetWalletsForUpdate(ctx, tx, req.FromWalletID, req.ToWalletID)
		if lockErr != nil {
			return lockErr
		}

		if wFrom.Currency != wTo.Currency {
			return model.ErrCurrencyMismatch
		}

		transferID := "tr_" + uuid.New().String()
		now := time.Now().UTC()

		// Case A: Insufficient Funds
		if wFrom.Balance < req.Amount {
			reason := "insufficient funds in source wallet"
			tFailed := model.Transfer{
				ID:             transferID,
				IdempotencyKey: req.IdempotencyKey,
				FromWalletID:   req.FromWalletID,
				ToWalletID:     req.ToWalletID,
				Amount:         req.Amount,
				Status:         model.TransferStatusFailed,
				FailureReason:  &reason,
				CreatedAt:      now,
				UpdatedAt:      now,
			}

			if err := s.repo.CreateTransfer(ctx, tx, &tFailed); err != nil {
				return err
			}

			// Generate zero ledger entries for failed transfers
			respBody := "insufficient funds in source wallet"
			idempRecord := model.IdempotencyRecord{
				Key:            req.IdempotencyKey,
				RequestHash:    payloadHash,
				Status:         model.IdempotencyStatusCompleted,
				ResponseStatus: 409,
				ResponseBody:   respBody,
				TransferID:     &transferID,
				CreatedAt:      now,
				UpdatedAt:      now,
			}

			if err := s.repo.SaveIdempotencyRecord(ctx, tx, &idempRecord); err != nil {
				return err
			}

			finalStatus = 409
			executionErr = model.ErrInsufficientFunds
			return nil // Commit failure record to DB so retries receive cached 409
		}

		// Case B: Check balance overflow on receiver before mutation
		if wTo.Balance > math.MaxInt64-req.Amount {
			executionErr = model.ErrBalanceOverflow
			return model.ErrBalanceOverflow
		}

		// Case C: Sufficient Funds - Process Transfer
		newFromBalance := wFrom.Balance - req.Amount
		newToBalance := wTo.Balance + req.Amount

		if err := s.repo.UpdateWalletBalance(ctx, tx, req.FromWalletID, newFromBalance); err != nil {
			return err
		}
		if err := s.repo.UpdateWalletBalance(ctx, tx, req.ToWalletID, newToBalance); err != nil {
			return err
		}

		tProcessed := model.Transfer{
			ID:             transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         model.TransferStatusProcessed,
			FailureReason:  nil,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := s.repo.CreateTransfer(ctx, tx, &tProcessed); err != nil {
			return err
		}

		// Double-Entry Bookkeeping: exactly 2 entries (1 DEBIT, 1 CREDIT)
		ledgerEntries := []model.LedgerEntry{
			{
				ID:         "le_" + uuid.New().String(),
				TransferID: transferID,
				WalletID:   req.FromWalletID,
				EntryType:  model.EntryTypeDebit,
				Amount:     req.Amount,
				CreatedAt:  now,
			},
			{
				ID:         "le_" + uuid.New().String(),
				TransferID: transferID,
				WalletID:   req.ToWalletID,
				EntryType:  model.EntryTypeCredit,
				Amount:     req.Amount,
				CreatedAt:  now,
			},
		}

		if err := s.repo.CreateLedgerEntries(ctx, tx, ledgerEntries); err != nil {
			return err
		}

		finalResponse = TransferResponse{
			TransferID:     transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Status:         model.TransferStatusProcessed,
			CreatedAt:      now,
			LedgerEntries:  ledgerEntries,
		}

		respBytes, _ := json.Marshal(finalResponse)
		idempRecord := model.IdempotencyRecord{
			Key:            req.IdempotencyKey,
			RequestHash:    payloadHash,
			Status:         model.IdempotencyStatusCompleted,
			ResponseStatus: 201,
			ResponseBody:   string(respBytes),
			TransferID:     &transferID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := s.repo.SaveIdempotencyRecord(ctx, tx, &idempRecord); err != nil {
			return err
		}

		finalStatus = 201
		return nil
	})

	if txErr != nil {
		// Handle concurrent race conditions where two identical keys hit at the exact same instant
		if postgres.IsUniqueViolation(txErr) || errors.Is(txErr, model.ErrIdempotencyConflict) {
			// Fetch the winning committed transaction's record
			winnerRecord, winErr := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
			if winErr == nil && winnerRecord != nil {
				if winnerRecord.RequestHash != payloadHash {
					return nil, 409, model.ErrIdempotencyConflict
				}
				var cachedResp TransferResponse
				if jsonErr := json.Unmarshal([]byte(winnerRecord.ResponseBody), &cachedResp); jsonErr == nil {
					return &cachedResp, winnerRecord.ResponseStatus, nil
				}
				return nil, winnerRecord.ResponseStatus, errors.New(winnerRecord.ResponseBody)
			}
		}

		if errors.Is(txErr, model.ErrWalletNotFound) {
			return nil, 404, txErr
		}
		if errors.Is(txErr, model.ErrSelfTransfer) {
			return nil, 400, txErr
		}
		if errors.Is(txErr, model.ErrCurrencyMismatch) {
			return nil, 400, txErr
		}
		return nil, 500, txErr
	}

	if executionErr != nil {
		return nil, finalStatus, executionErr
	}

	return &finalResponse, finalStatus, nil
}

func (s *TransferService) GetTransfer(ctx context.Context, id string) (*TransferResponse, error) {
	t, err := s.repo.GetTransfer(ctx, id)
	if err != nil {
		return nil, err
	}

	entries, err := s.repo.GetLedgerEntriesByTransferID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &TransferResponse{
		TransferID:     t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         t.Status,
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
		LedgerEntries:  entries,
	}, nil
}
