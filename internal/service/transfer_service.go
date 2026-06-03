package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ProcessTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type ProcessTransferResponse struct {
	TransferID   string                `json:"transferId,omitempty"`
	FromWalletID string                `json:"fromWalletId"`
	ToWalletID   string                `json:"toWalletId"`
	Amount       int64                 `json:"amount"`
	Status       domain.TransferStatus `json:"status"`
	Error        string                `json:"error,omitempty"`
}

type TransferService interface {
	ProcessTransfer(ctx context.Context, req *ProcessTransferRequest) (*ProcessTransferResponse, int, error)
}

type transferService struct {
	db              *gorm.DB
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
}

func NewTransferService(
	db *gorm.DB,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempotencyRepo repository.IdempotencyRepository,
) *transferService {
	return &transferService{
		db:              db,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

func (s *transferService) ProcessTransfer(ctx context.Context, req *ProcessTransferRequest) (*ProcessTransferResponse, int, error) {
	var finalResponse *ProcessTransferResponse
	var responseCode int

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Check idempotency key
		rec, err := s.idempotencyRepo.Get(ctx, tx, req.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("idempotency check failed: %w", err)
		}

		if rec != nil {
			// Key exists
			if rec.Status == domain.IdempotencyStatusPending {
				return domain.ErrConcurrentRequest
			}

			// Request completed already, decode and return original response
			var cachedResp ProcessTransferResponse
			if err := json.Unmarshal([]byte(rec.ResponseBody), &cachedResp); err != nil {
				return fmt.Errorf("failed to unmarshal cached response: %w", err)
			}
			finalResponse = &cachedResp
			responseCode = rec.ResponseCode
			return nil
		}

		// Create PENDING record
		if err := s.idempotencyRepo.CreatePending(ctx, tx, req.IdempotencyKey); err != nil {
			if errors.Is(err, domain.ErrDuplicateRequest) {
				// Rare race condition, refetch
				rec2, err := s.idempotencyRepo.Get(ctx, tx, req.IdempotencyKey)
				if err != nil {
					return err
				}
				if rec2 != nil {
					if rec2.Status == domain.IdempotencyStatusPending {
						return domain.ErrConcurrentRequest
					}
					var cachedResp ProcessTransferResponse
					if err := json.Unmarshal([]byte(rec2.ResponseBody), &cachedResp); err != nil {
						return err
					}
					finalResponse = &cachedResp
					responseCode = rec2.ResponseCode
					return nil
				}
			}
			return err
		}

		// 2. Validate inputs
		if req.FromWalletID == req.ToWalletID {
			_ = s.saveFailure(ctx, tx, req.IdempotencyKey, req, domain.ErrSameWallet, 422)
			return domain.ErrSameWallet
		}
		if req.Amount <= 0 {
			_ = s.saveFailure(ctx, tx, req.IdempotencyKey, req, domain.ErrInvalidAmount, 400)
			return domain.ErrInvalidAmount
		}

		// 3. Lock wallets in alphabetical order to prevent deadlock
		firstID, secondID := req.FromWalletID, req.ToWalletID
		if firstID > secondID {
			firstID, secondID = secondID, firstID
		}

		w1, err := s.walletRepo.GetWithLock(ctx, tx, firstID)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				_ = s.saveFailure(ctx, tx, req.IdempotencyKey, req, fmt.Errorf("wallet %s not found: %w", firstID, domain.ErrWalletNotFound), 422)
				return domain.ErrWalletNotFound
			}
			return fmt.Errorf("failed to lock wallet %s: %w", firstID, err)
		}

		w2, err := s.walletRepo.GetWithLock(ctx, tx, secondID)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				_ = s.saveFailure(ctx, tx, req.IdempotencyKey, req, fmt.Errorf("wallet %s not found: %w", secondID, domain.ErrWalletNotFound), 422)
				return domain.ErrWalletNotFound
			}
			return fmt.Errorf("failed to lock wallet %s: %w", secondID, err)
		}

		// Assign source and destination wallets
		var sourceWallet, destWallet *domain.Wallet
		if w1.ID == req.FromWalletID {
			sourceWallet = w1
			destWallet = w2
		} else {
			sourceWallet = w2
			destWallet = w1
		}

		// 4. Check balance
		if sourceWallet.Balance < req.Amount {
			// This is a business logic failure (insufficient funds)
			transferID := uuid.New().String()
			transfer := &domain.Transfer{
				ID:           transferID,
				FromWalletID: req.FromWalletID,
				ToWalletID:   req.ToWalletID,
				Amount:       req.Amount,
				Status:       domain.TransferStatusFailed,
			}
			if err := s.transferRepo.Create(ctx, tx, transfer); err != nil {
				return fmt.Errorf("failed to create failed transfer record: %w", err)
			}

			resp := &ProcessTransferResponse{
				TransferID:   transferID,
				FromWalletID: req.FromWalletID,
				ToWalletID:   req.ToWalletID,
				Amount:       req.Amount,
				Status:       domain.TransferStatusFailed,
				Error:        domain.ErrInsufficientFunds.Error(),
			}
			respBytes, _ := json.Marshal(resp)

			idemRec := &domain.IdempotencyRecord{
				IdempotencyKey: req.IdempotencyKey,
				Status:         domain.IdempotencyStatusFailed,
				ResponseCode:   422,
				ResponseBody:   string(respBytes),
			}
			if err := s.idempotencyRepo.Update(ctx, tx, idemRec); err != nil {
				return fmt.Errorf("failed to update idempotency status to FAILED: %w", err)
			}

			finalResponse = resp
			responseCode = 422
			return nil
		}

		// 5. Deduct from source and credit destination
		if err := s.walletRepo.UpdateBalance(ctx, tx, sourceWallet.ID, -req.Amount); err != nil {
			return fmt.Errorf("failed to debit source wallet: %w", err)
		}
		if err := s.walletRepo.UpdateBalance(ctx, tx, destWallet.ID, req.Amount); err != nil {
			return fmt.Errorf("failed to credit destination wallet: %w", err)
		}

		// 6. Create transfer record
		transferID := uuid.New().String()
		transfer := &domain.Transfer{
			ID:           transferID,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       domain.TransferStatusProcessed,
		}
		if err := s.transferRepo.Create(ctx, tx, transfer); err != nil {
			return fmt.Errorf("failed to create transfer record: %w", err)
		}

		// 7. Create ledger entries
		debitEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			WalletID:   req.FromWalletID,
			TransferID: transferID,
			Type:       domain.EntryTypeDebit,
			Amount:     req.Amount,
		}
		if err := s.ledgerRepo.Create(ctx, tx, debitEntry); err != nil {
			return fmt.Errorf("failed to create debit ledger entry: %w", err)
		}

		creditEntry := &domain.LedgerEntry{
			ID:         uuid.New().String(),
			WalletID:   req.ToWalletID,
			TransferID: transferID,
			Type:       domain.EntryTypeCredit,
			Amount:     req.Amount,
		}
		if err := s.ledgerRepo.Create(ctx, tx, creditEntry); err != nil {
			return fmt.Errorf("failed to create credit ledger entry: %w", err)
		}

		// 8. Update idempotency record to SUCCESS
		resp := &ProcessTransferResponse{
			TransferID:   transferID,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       domain.TransferStatusProcessed,
		}
		respBytes, _ := json.Marshal(resp)

		idemRec := &domain.IdempotencyRecord{
			IdempotencyKey: req.IdempotencyKey,
			Status:         domain.IdempotencyStatusSuccess,
			ResponseCode:   201,
			ResponseBody:   string(respBytes),
		}
		if err := s.idempotencyRepo.Update(ctx, tx, idemRec); err != nil {
			return fmt.Errorf("failed to update idempotency status to SUCCESS: %w", err)
		}

		finalResponse = resp
		responseCode = 201
		return nil
	})

	if err != nil {
		// If transaction returned an error that is already saved in idempotency table,
		// retrieve the error response from DB (outside transaction) so we can return it.
		if errors.Is(err, domain.ErrSameWallet) || errors.Is(err, domain.ErrInvalidAmount) || errors.Is(err, domain.ErrWalletNotFound) {
			rec, getErr := s.idempotencyRepo.Get(ctx, s.db, req.IdempotencyKey)
			if getErr == nil && rec != nil && rec.Status == domain.IdempotencyStatusFailed {
				var cachedResp ProcessTransferResponse
				if json.Unmarshal([]byte(rec.ResponseBody), &cachedResp) == nil {
					return &cachedResp, rec.ResponseCode, nil
				}
			}
		}
		return nil, 0, err
	}

	return finalResponse, responseCode, nil
}

func (s *transferService) saveFailure(
	ctx context.Context,
	tx *gorm.DB,
	idemKey string,
	req *ProcessTransferRequest,
	errReason error,
	httpStatus int,
) error {
	resp := &ProcessTransferResponse{
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       domain.TransferStatusFailed,
		Error:        errReason.Error(),
	}
	respBytes, _ := json.Marshal(resp)

	idemRec := &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		Status:         domain.IdempotencyStatusFailed,
		ResponseCode:   httpStatus,
		ResponseBody:   string(respBytes),
	}

	return s.idempotencyRepo.Update(ctx, tx, idemRec)
}
