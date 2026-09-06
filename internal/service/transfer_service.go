package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/postgres"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CreateTransferInput struct {
	IdempotencyKey *string `json:"idempotencyKey,omitempty"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         string  `json:"amount"`
}

type TransferResponse struct {
	TransferID     string  `json:"transferId"`
	IdempotencyKey *string `json:"idempotencyKey,omitempty"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         string  `json:"amount"`
	Status         string  `json:"status"`
	ErrorCode      *string `json:"errorCode,omitempty"`
	ErrorMessage   *string `json:"errorMessage,omitempty"`
	CreatedAt      string  `json:"createdAt"`
	ProcessedAt    *string `json:"processedAt,omitempty"`
}

type CachedResponse struct {
	StatusCode int
	Body       []byte
}

type TransferService struct {
	transactor      *postgres.Transactor
	walletRepo      *postgres.WalletRepository
	transferRepo    *postgres.TransferRepository
	ledgerRepo      *postgres.LedgerRepository
	idempotencyRepo *postgres.IdempotencyRepository
}

func NewTransferService(
	transactor *postgres.Transactor,
	walletRepo *postgres.WalletRepository,
	transferRepo *postgres.TransferRepository,
	ledgerRepo *postgres.LedgerRepository,
	idempotencyRepo *postgres.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		transactor:      transactor,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

func (s *TransferService) CreateTransfer(ctx context.Context, input *CreateTransferInput) (*TransferResponse, *CachedResponse, error) {
	// 1. Input Syntax & Domain Validations
	if input.FromWalletID == "" {
		return nil, nil, domain.ErrInvalidInput("fromWalletId is required")
	}
	if input.ToWalletID == "" {
		return nil, nil, domain.ErrInvalidInput("toWalletId is required")
	}
	fromID, err := uuid.Parse(input.FromWalletID)
	if err != nil {
		return nil, nil, domain.ErrInvalidInput("fromWalletId must be a valid UUID")
	}
	toID, err := uuid.Parse(input.ToWalletID)
	if err != nil {
		return nil, nil, domain.ErrInvalidInput("toWalletId must be a valid UUID")
	}
	if fromID == toID {
		return nil, nil, domain.ErrSameWalletTransfer()
	}

	amount, err := domain.ParseMoney(input.Amount)
	if err != nil {
		return nil, nil, err
	}
	if !amount.IsPositive() {
		return nil, nil, domain.ErrInvalidAmount("amount must be strictly greater than zero")
	}

	var idempotencyKeyStr string
	var reqHash string
	if input.IdempotencyKey != nil && *input.IdempotencyKey != "" {
		idempotencyKeyStr = *input.IdempotencyKey
		reqHash = domain.ComputeRequestHash(input.FromWalletID, input.ToWalletID, amount.StringExact())
	}

	var responsePayload *TransferResponse
	var cachedResp *CachedResponse
	var postCommitErr error

	// Execute atomic transaction
	txErr := s.transactor.ExecTx(ctx, func(tx pgx.Tx) error {
		// 2. Check Idempotency Key if provided
		if idempotencyKeyStr != "" {
			existingRec, err := s.idempotencyRepo.GetIdempotencyRecord(ctx, tx, idempotencyKeyStr)
			if err != nil {
				return err
			}
			if existingRec != nil {
				// Verify if payload hash matches
				if existingRec.RequestHash != reqHash {
					return domain.ErrIdempotencyKeyReused()
				}
				// Replay cached response directly
				cachedResp = &CachedResponse{
					StatusCode: existingRec.ResponseCode,
					Body:       existingRec.ResponseBody,
				}
				return nil
			}
		}

		// 3. Lock wallets in deterministic order to prevent deadlocks
		fromWallet, toWallet, err := s.walletRepo.LockAndGetWallets(ctx, tx, fromID, toID)
		if err != nil {
			return err
		}

		transferID := uuid.New()
		transfer, err := domain.NewTransfer(transferID, input.IdempotencyKey, fromID, toID, amount)
		if err != nil {
			return err
		}

		// 4. Verify balance adequacy
		if err := fromWallet.Debit(amount); err != nil {
			insufficientErr := domain.ErrInsufficientFunds()
			// Mark transfer as failed
			_ = transfer.MarkFailed(insufficientErr.Code, insufficientErr.Message)
			if createErr := s.transferRepo.CreateTransfer(ctx, tx, transfer); createErr != nil {
				return createErr
			}

			// Store idempotency record for failed attempt if key present
			if idempotencyKeyStr != "" {
				errResp := domain.AppError{Code: insufficientErr.Code, Message: insufficientErr.Message}
				errBytes, _ := json.Marshal(map[string]interface{}{"error": errResp})
				idemRec := &domain.IdempotencyRecord{
					Key:          idempotencyKeyStr,
					RequestHash:  reqHash,
					TransferID:   &transferID,
					ResponseCode: insufficientErr.HTTPStatus,
					ResponseBody: errBytes,
				}
				if saveErr := s.idempotencyRepo.SaveIdempotencyRecord(ctx, tx, idemRec); saveErr != nil {
					return saveErr
				}
			}

			postCommitErr = insufficientErr
			return nil
		}

		// 5. Credit destination wallet
		if err := toWallet.Credit(amount); err != nil {
			return err
		}

		// 6. Update wallet balances in DB
		if err := s.walletRepo.UpdateWalletBalance(ctx, tx, fromWallet); err != nil {
			return err
		}
		if err := s.walletRepo.UpdateWalletBalance(ctx, tx, toWallet); err != nil {
			return err
		}

		// 7. Transition state to PROCESSED & persist transfer
		if err := transfer.MarkProcessed(); err != nil {
			return err
		}
		if err := s.transferRepo.CreateTransfer(ctx, tx, transfer); err != nil {
			return err
		}

		// 8. Create Double-Entry Ledger records
		debitEntry, err := domain.NewLedgerEntry(uuid.New(), transferID, fromID, domain.LedgerTypeDebit, amount)
		if err != nil {
			return err
		}
		creditEntry, err := domain.NewLedgerEntry(uuid.New(), transferID, toID, domain.LedgerTypeCredit, amount)
		if err != nil {
			return err
		}
		if err := s.ledgerRepo.CreateLedgerEntry(ctx, tx, debitEntry); err != nil {
			return err
		}
		if err := s.ledgerRepo.CreateLedgerEntry(ctx, tx, creditEntry); err != nil {
			return err
		}

		// Build response
		processedAtStr := transfer.ProcessedAt.Format(time.RFC3339Nano)
		responsePayload = &TransferResponse{
			TransferID:     transferID.String(),
			IdempotencyKey: input.IdempotencyKey,
			FromWalletID:   fromID.String(),
			ToWalletID:     toID.String(),
			Amount:         amount.StringExact(),
			Status:         string(transfer.Status),
			CreatedAt:      transfer.CreatedAt.Format(time.RFC3339Nano),
			ProcessedAt:    &processedAtStr,
		}

		// 9. Save Idempotency Record if key provided
		if idempotencyKeyStr != "" {
			respBytes, jsonErr := json.Marshal(responsePayload)
			if jsonErr != nil {
				return domain.ErrInternal(fmt.Errorf("failed marshaling response payload: %w", jsonErr))
			}
			idemRec := &domain.IdempotencyRecord{
				Key:          idempotencyKeyStr,
				RequestHash:  reqHash,
				TransferID:   &transferID,
				ResponseCode: http.StatusCreated,
				ResponseBody: respBytes,
			}
			if err := s.idempotencyRepo.SaveIdempotencyRecord(ctx, tx, idemRec); err != nil {
				return err
			}
		}

		return nil
	})

	if txErr != nil {
		return nil, nil, txErr
	}

	if postCommitErr != nil {
		return nil, nil, postCommitErr
	}

	return responsePayload, cachedResp, nil
}

func (s *TransferService) GetTransferByID(ctx context.Context, id uuid.UUID) (*TransferResponse, error) {
	tr, err := s.transferRepo.GetTransferByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var processedAtStr *string
	if tr.ProcessedAt != nil {
		str := tr.ProcessedAt.Format(time.RFC3339Nano)
		processedAtStr = &str
	}

	return &TransferResponse{
		TransferID:     tr.ID.String(),
		IdempotencyKey: tr.IdempotencyKey,
		FromWalletID:   tr.FromWalletID.String(),
		ToWalletID:     tr.ToWalletID.String(),
		Amount:         tr.Amount.StringExact(),
		Status:         string(tr.Status),
		ErrorCode:      tr.ErrorCode,
		ErrorMessage:   tr.ErrorMessage,
		CreatedAt:      tr.CreatedAt.Format(time.RFC3339Nano),
		ProcessedAt:    processedAtStr,
	}, nil
}
