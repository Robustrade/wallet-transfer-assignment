package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/repository"
)

// TransferService orchestrates wallet transfer business operations.
type TransferService struct {
	txMgr           repository.TxManager
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
}

// NewTransferService constructs a TransferService instance.
func NewTransferService(
	txMgr repository.TxManager,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempotencyRepo repository.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		txMgr:           txMgr,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

// CreateTransferInput defines parameters required to create a wallet transfer.
type CreateTransferInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"` // Stored in minor units (e.g., cents)
}

// TransferResult wraps the resulting transfer entity along with replay indicator.
type TransferResult struct {
	Transfer   *domain.Transfer     `json:"transfer"`
	Ledger     []domain.LedgerEntry `json:"ledgerEntries,omitempty"`
	IsReplayed bool                 `json:"isReplayed"`
}

// CreateTransfer executes an atomic, concurrency-safe, idempotent wallet transfer.
func (s *TransferService) CreateTransfer(ctx context.Context, input CreateTransferInput) (*TransferResult, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, fmt.Errorf("idempotencyKey is required")
	}
	if strings.TrimSpace(input.FromWalletID) == "" || strings.TrimSpace(input.ToWalletID) == "" {
		return nil, domain.ErrInvalidWalletID
	}
	if input.FromWalletID == input.ToWalletID {
		return nil, domain.ErrSameWalletTransfer
	}
	if input.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}

	requestHash := domain.ComputeRequestHash(input.FromWalletID, input.ToWalletID, input.Amount)
	var result *TransferResult

	err := s.txMgr.ExecTx(ctx, func(tx *sql.Tx) error {
		// 1. Idempotency Check & Locking
		idemRecord, err := s.idempotencyRepo.GetForUpdate(ctx, tx, input.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("failed to check idempotency key: %w", err)
		}

		if idemRecord != nil {
			if idemRecord.RequestHash != requestHash {
				return domain.ErrIdempotencyKeyReusedMismatch
			}

			if idemRecord.Status == domain.IdempotencyStatusInProgress {
				return domain.ErrIdempotencyConflict
			}

			// Replay response for completed or failed requests
			if idemRecord.TransferID != "" {
				t, err := s.transferRepo.GetByID(ctx, tx, idemRecord.TransferID)
				if err != nil {
					return fmt.Errorf("failed to fetch replayed transfer: %w", err)
				}
				ledgerEntries, _ := s.ledgerRepo.GetByTransferID(ctx, tx, t.ID)
				result = &TransferResult{
					Transfer:   t,
					Ledger:     ledgerEntries,
					IsReplayed: true,
				}
				return nil
			}
		}

		// Create initial IN_PROGRESS idempotency record
		idemRecord = &domain.IdempotencyRecord{
			IdempotencyKey: input.IdempotencyKey,
			RequestHash:    requestHash,
			Status:         domain.IdempotencyStatusInProgress,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if err := s.idempotencyRepo.Create(ctx, tx, idemRecord); err != nil {
			return fmt.Errorf("failed to create idempotency record: %w", err)
		}

		// 2. Deterministic Row Locking (Order by wallet ID lexicographically via single FOR UPDATE query)
		fromWallet, toWallet, err := s.walletRepo.GetTwoWalletsForUpdate(ctx, tx, input.FromWalletID, input.ToWalletID)
		if err != nil {
			s.recordFailedIdempotency(ctx, tx, idemRecord, err.Error())
			return err
		}

		// 3. Balance Check
		rawUUID := strings.ReplaceAll(uuid.New().String(), "-", "")
		transferID := fmt.Sprintf("trn_%s", rawUUID)
		now := time.Now().UTC()

		if fromWallet.Balance < input.Amount {
			// Persist FAILED transfer status durably
			failedTransfer := &domain.Transfer{
				ID:             transferID,
				IdempotencyKey: input.IdempotencyKey,
				FromWalletID:   input.FromWalletID,
				ToWalletID:     input.ToWalletID,
				Amount:         input.Amount,
				Status:         domain.TransferStatusFailed,
				FailureReason:  domain.ErrInsufficientFunds.Error(),
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if err := s.transferRepo.Create(ctx, tx, failedTransfer); err != nil {
				return fmt.Errorf("failed to save failed transfer record: %w", err)
			}

			s.recordCompletedIdempotency(ctx, tx, idemRecord, transferID, 400, failedTransfer)
			result = &TransferResult{
				Transfer:   failedTransfer,
				IsReplayed: false,
			}
			return domain.ErrInsufficientFunds
		}

		// 4. Atomic Execution: Update Balances & Insert Transfer + Double-Entry Ledger
		newFromBalance := fromWallet.Balance - input.Amount
		newToBalance := toWallet.Balance + input.Amount

		if err := s.walletRepo.UpdateBalance(ctx, tx, fromWallet.ID, newFromBalance); err != nil {
			return fmt.Errorf("failed to update source wallet balance: %w", err)
		}
		if err := s.walletRepo.UpdateBalance(ctx, tx, toWallet.ID, newToBalance); err != nil {
			return fmt.Errorf("failed to update destination wallet balance: %w", err)
		}

		processedTransfer := &domain.Transfer{
			ID:             transferID,
			IdempotencyKey: input.IdempotencyKey,
			FromWalletID:   input.FromWalletID,
			ToWalletID:     input.ToWalletID,
			Amount:         input.Amount,
			Status:         domain.TransferStatusProcessed,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.transferRepo.Create(ctx, tx, processedTransfer); err != nil {
			return fmt.Errorf("failed to insert transfer record: %w", err)
		}

		debitID := fmt.Sprintf("led_%s", strings.ReplaceAll(uuid.New().String(), "-", ""))
		creditID := fmt.Sprintf("led_%s", strings.ReplaceAll(uuid.New().String(), "-", ""))

		ledgerEntries := []domain.LedgerEntry{
			{
				ID:         debitID,
				TransferID: transferID,
				WalletID:   input.FromWalletID,
				Type:       domain.LedgerEntryTypeDebit,
				Amount:     input.Amount,
				CreatedAt:  now,
			},
			{
				ID:         creditID,
				TransferID: transferID,
				WalletID:   input.ToWalletID,
				Type:       domain.LedgerEntryTypeCredit,
				Amount:     input.Amount,
				CreatedAt:  now,
			},
		}

		if ledgerEntries[0].WalletID > ledgerEntries[1].WalletID {
			ledgerEntries[0], ledgerEntries[1] = ledgerEntries[1], ledgerEntries[0]
		}

		if err := s.ledgerRepo.CreateEntries(ctx, tx, ledgerEntries); err != nil {
			return fmt.Errorf("failed to insert double-entry ledger records: %w", err)
		}

		// 5. Update Idempotency Record as COMPLETED
		s.recordCompletedIdempotency(ctx, tx, idemRecord, transferID, 200, processedTransfer)

		result = &TransferResult{
			Transfer:   processedTransfer,
			Ledger:     ledgerEntries,
			IsReplayed: false,
		}
		return nil
	})

	if err != nil && !errorsIsInsufficientFunds(err) {
		return nil, err
	}

	return result, err
}

func errorsIsInsufficientFunds(err error) bool {
	return err == domain.ErrInsufficientFunds || strings.Contains(err.Error(), domain.ErrInsufficientFunds.Error())
}

func (s *TransferService) recordCompletedIdempotency(ctx context.Context, tx *sql.Tx, rec *domain.IdempotencyRecord, transferID string, code int, respBody any) {
	rec.Status = domain.IdempotencyStatusCompleted
	rec.TransferID = transferID
	rec.ResponseCode = code
	if bodyBytes, err := json.Marshal(respBody); err == nil {
		rec.ResponseBody = string(bodyBytes)
	}
	_ = s.idempotencyRepo.Update(ctx, tx, rec)
}

func (s *TransferService) recordFailedIdempotency(ctx context.Context, tx *sql.Tx, rec *domain.IdempotencyRecord, reason string) {
	rec.Status = domain.IdempotencyStatusFailed
	rec.ResponseBody = reason
	_ = s.idempotencyRepo.Update(ctx, tx, rec)
}

// GetTransferByID retrieves transfer details and double-entry ledger entries.
func (s *TransferService) GetTransferByID(ctx context.Context, id string) (*domain.Transfer, []domain.LedgerEntry, error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil, fmt.Errorf("transfer ID cannot be empty")
	}

	transfer, err := s.transferRepo.GetByID(ctx, nil, id)
	if err != nil {
		return nil, nil, err
	}

	ledgerEntries, err := s.ledgerRepo.GetByTransferID(ctx, nil, id)
	if err != nil {
		return nil, nil, err
	}

	return transfer, ledgerEntries, nil
}
