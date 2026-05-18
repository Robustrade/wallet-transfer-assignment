package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/candidate/wallet-transfer/internal/db"
	"github.com/candidate/wallet-transfer/internal/domain"
	"github.com/candidate/wallet-transfer/internal/repository"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

type CreateWalletRequest struct {
	UserID   uint64 `json:"user_id"  binding:"required"`
	Currency string `json:"currency" binding:"required,len=3"`
}

type TransferRequest struct {
	IdempotencyKey string          `json:"idempotencyKey" binding:"required"`
	FromWalletID   uint64          `json:"fromWalletId"   binding:"required"`
	ToWalletID     uint64          `json:"toWalletId"     binding:"required"`
	Amount         decimal.Decimal `json:"amount"         binding:"required"`
	Currency       string          `json:"currency"       binding:"required,len=3"`
	Description    string          `json:"description"`
	Metadata       string          `json:"metadata"`
}

type TransferResponse struct {
	Transaction *domain.Transaction `json:"transaction"`
}

type TransferService struct {
	db      *db.DB
	redis   *redis.Client
	wallets *repository.WalletRepository
	txns    *repository.TransactionRepository
	ledger  *repository.LedgerRepository
	outbox  *repository.OutboxRepository
}

func NewTransferService(
	d *db.DB,
	r *redis.Client,
	w *repository.WalletRepository,
	t *repository.TransactionRepository,
	l *repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		db:      d,
		redis:   r,
		wallets: w,
		txns:    t,
		ledger:  l,
		outbox:  repository.NewOutboxRepository(),
	}
}

func (s *TransferService) CreateWallet(ctx context.Context, req *CreateWalletRequest) (*domain.Wallet, error) {
	return s.wallets.Create(ctx, req.UserID, req.Currency)
}

func (s *TransferService) GetWallet(ctx context.Context, id uint64) (*domain.Wallet, error) {
	return s.wallets.GetByID(ctx, s.db, id)
}

func (s *TransferService) Transfer(ctx context.Context, req *TransferRequest) (*TransferResponse, error) {
	if req.IdempotencyKey == "" {
		return nil, fmt.Errorf("Idempotency-Key header is required")
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWallet
	}
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, domain.ErrInvalidAmount
	}

	redisKey := fmt.Sprintf("idempotency:transfer:%s", req.IdempotencyKey)
	if s.redis != nil {
		cachedVal, err := s.redis.Get(ctx, redisKey).Result()
		if err == nil && cachedVal != "" {
			var cachedTxn domain.Transaction
			if err := json.Unmarshal([]byte(cachedVal), &cachedTxn); err == nil {
				if err := validateIdempotentRequest(req, &cachedTxn); err != nil {
					return nil, err
				}
				return &TransferResponse{Transaction: &cachedTxn}, nil
			}
		}
	}

	existing, err := s.txns.GetByIdempotencyKey(ctx, s.db, req.IdempotencyKey)
	if err != nil && !errors.Is(err, domain.ErrTransactionNotFound) {
		return nil, fmt.Errorf("idempotency pre-check: %w", err)
	}
	if existing != nil {
		if err := validateIdempotentRequest(req, existing); err != nil {
			return nil, err
		}
		if s.redis != nil {
			if data, err := json.Marshal(existing); err == nil {
				_ = s.redis.Set(ctx, redisKey, string(data), 24*time.Hour).Err()
			}
		}
		return &TransferResponse{Transaction: existing}, nil
	}

	var result *domain.Transaction

	err = s.db.WithTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		firstID, secondID := req.FromWalletID, req.ToWalletID
		if firstID > secondID {
			firstID, secondID = secondID, firstID
		}

		firstWallet, err := s.wallets.LockByID(ctx, tx, firstID)
		if err != nil {
			return fmt.Errorf("lock wallet %d: %w", firstID, err)
		}
		secondWallet, err := s.wallets.LockByID(ctx, tx, secondID)
		if err != nil {
			return fmt.Errorf("lock wallet %d: %w", secondID, err)
		}

		inner, err := s.txns.GetByIdempotencyKey(ctx, tx, req.IdempotencyKey)
		if err != nil && !errors.Is(err, domain.ErrTransactionNotFound) {
			return err
		}
		if inner != nil {
			if err := validateIdempotentRequest(req, inner); err != nil {
				return err
			}
			result = inner
			return nil
		}

		var fromWallet, toWallet *domain.Wallet
		if req.FromWalletID == firstID {
			fromWallet, toWallet = firstWallet, secondWallet
		} else {
			fromWallet, toWallet = secondWallet, firstWallet
		}

		if fromWallet.Currency != toWallet.Currency {
			return domain.ErrCurrencyMismatch
		}
		if fromWallet.Currency != req.Currency {
			return domain.ErrCurrencyMismatch
		}
		if fromWallet.Balance.LessThan(req.Amount) {
			return domain.ErrInsufficientFunds
		}

		hash := sha256.Sum256([]byte(req.IdempotencyKey))
		referenceID := fmt.Sprintf("TXN_%s_%s", time.Now().Format("20060102"), hex.EncodeToString(hash[:16]))

		txn, err := s.txns.Create(ctx, tx, &domain.Transaction{
			IdempotencyKey: req.IdempotencyKey,
			ReferenceID:    referenceID,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			Currency:       req.Currency,
			Status:         domain.StatusPending,
			Description:    req.Description,
			Metadata:       req.Metadata,
		})
		if err != nil {
			if errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
				winner, _ := s.txns.GetByIdempotencyKey(ctx, tx, req.IdempotencyKey)
				if winner != nil {
					if err := validateIdempotentRequest(req, winner); err != nil {
						return err
					}
				}
				result = winner
				return nil
			}
			return fmt.Errorf("create transaction: %w", err)
		}

		newFromBalance := fromWallet.Balance.Sub(req.Amount)
		newToBalance := toWallet.Balance.Add(req.Amount)

		if err := s.wallets.UpdateBalance(ctx, tx, req.FromWalletID, newFromBalance); err != nil {
			return fmt.Errorf("debit wallet: %w", err)
		}
		if err := s.wallets.UpdateBalance(ctx, tx, req.ToWalletID, newToBalance); err != nil {
			return fmt.Errorf("credit wallet: %w", err)
		}

		if err := s.ledger.Create(ctx, tx, &domain.LedgerEntry{
			TransactionID: txn.ID,
			WalletID:      req.FromWalletID,
			EntryType:     domain.EntryDebit,
			Amount:        req.Amount,
			BalanceBefore: fromWallet.Balance,
			BalanceAfter:  newFromBalance,
		}); err != nil {
			return fmt.Errorf("debit ledger entry: %w", err)
		}
		if err := s.ledger.Create(ctx, tx, &domain.LedgerEntry{
			TransactionID: txn.ID,
			WalletID:      req.ToWalletID,
			EntryType:     domain.EntryCredit,
			Amount:        req.Amount,
			BalanceBefore: toWallet.Balance,
			BalanceAfter:  newToBalance,
		}); err != nil {
			return fmt.Errorf("credit ledger entry: %w", err)
		}

		eventPayload := struct {
			TransactionID uint64 `json:"transaction_id"`
			ReferenceID   string `json:"reference_id"`
			FromWalletID  uint64 `json:"from_wallet_id"`
			ToWalletID    uint64 `json:"to_wallet_id"`
			Amount        string `json:"amount"`
			Currency      string `json:"currency"`
		}{
			TransactionID: txn.ID,
			ReferenceID:   txn.ReferenceID,
			FromWalletID:  txn.FromWalletID,
			ToWalletID:    txn.ToWalletID,
			Amount:        txn.Amount.String(),
			Currency:      txn.Currency,
		}
		eventPayloadBytes, err := json.Marshal(eventPayload)
		if err != nil {
			return fmt.Errorf("marshal outbox event payload: %w", err)
		}
		if err := s.outbox.Create(ctx, tx, &domain.OutboxEvent{
			EventType: "TransferCompleted",
			Payload:   string(eventPayloadBytes),
			Status:    "PENDING",
		}); err != nil {
			return fmt.Errorf("write outbox event: %w", err)
		}

		if err := s.txns.UpdateStatus(ctx, tx, txn.ID, domain.StatusCompleted); err != nil {
			return fmt.Errorf("update status: %w", err)
		}

		// Re-fetch the transaction to capture updated_at set by the database
		final, err := s.txns.GetByIdempotencyKey(ctx, tx, req.IdempotencyKey)
		if err != nil {
			// Fallback to in-memory object if re-fetch fails
			txn.Status = domain.StatusCompleted
			result = txn
			return nil
		}
		result = final
		return nil
	})

	if err != nil {
		return nil, err
	}

	if s.redis != nil && result != nil {
		if data, err := json.Marshal(result); err == nil {
			_ = s.redis.Set(ctx, redisKey, string(data), 24*time.Hour).Err()
		}
	}

	return &TransferResponse{Transaction: result}, nil
}

func (s *TransferService) GetTransaction(ctx context.Context, id uint64) (*domain.Transaction, error) {
	return s.txns.GetByID(ctx, s.db, id)
}

func (s *TransferService) GetLedger(ctx context.Context, walletID uint64, limit int, cursor uint64) ([]*domain.LedgerEntry, error) {
	return s.ledger.GetByWalletID(ctx, s.db, walletID, limit, cursor)
}

func validateIdempotentRequest(req *TransferRequest, txn *domain.Transaction) error {
	if txn.FromWalletID != req.FromWalletID ||
		txn.ToWalletID != req.ToWalletID ||
		!txn.Amount.Equal(req.Amount) ||
		txn.Currency != req.Currency {
		return domain.ErrIdempotencyConflict
	}
	return nil
}
