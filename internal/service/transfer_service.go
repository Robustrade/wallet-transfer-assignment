package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository"
)

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResult struct {
	TransferID     string               `json:"transferId"`
	IdempotencyKey string               `json:"idempotencyKey"`
	FromWalletID   string               `json:"fromWalletId"`
	ToWalletID     string               `json:"toWalletId"`
	Amount         int64                `json:"amount"`
	State          domain.TransferState `json:"state"`
	FailureReason  string               `json:"failureReason,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
}

type TransferService struct {
	store repository.Store
}

func NewTransferService(store repository.Store) *TransferService {
	return &TransferService{store: store}
}

// CreateTransfer executes the full create-transfer workflow described
func (s *TransferService) CreateTransfer(ctx context.Context, req CreateTransferRequest) (result *TransferResult, replayed bool, err error) {
	if err := validate(req); err != nil {
		return nil, false, err
	}
	reqHash := hashRequest(req)

	// Fast path: a prior call already finished (success or business
	// failure) - replay it without opening a transaction.
	if existing, err := s.store.Idempotency().Get(ctx, req.IdempotencyKey); err != nil {
		return nil, false, err
	} else if existing != nil {
		res, replay, err := resolveExisting(existing, reqHash)
		if res != nil || err != nil {
			return res, replay, err
		}
	}

	var raced bool
	err = s.store.ExecTx(ctx, func(tx repository.Store) error {
		claimed, existing, err := tx.Idempotency().Claim(ctx, req.IdempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if !claimed {
			res, _, err := resolveExisting(existing, reqHash)
			if err != nil {
				return err
			}
			if res == nil {
				return domain.ErrIdempotencyKeyInUse
			}
			result, raced = res, true
			return nil
		}

		res, err := s.executeTransfer(ctx, tx, req)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, raced, nil
}

// executeTransfer runs once per claimed idempotency key: it locks both
// wallets in a fixed order, decides PROCESSED vs FAILED, and records the
// outcome (ledger entries + balances on success; just the transfer
// record on failure) plus the idempotency completion, all inside the
// caller's transaction.
func (s *TransferService) executeTransfer(ctx context.Context, tx repository.Store, req CreateTransferRequest) (*TransferResult, error) {
	from, to, err := lockWalletPair(ctx, tx.Wallets(), req.FromWalletID, req.ToWalletID)
	if err != nil {
		return nil, err
	}

	transferID := "txn_" + uuid.NewString()
	transfer, err := domain.NewTransfer(transferID, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		return nil, err
	}
	if err := tx.Transfers().Create(ctx, transfer); err != nil {
		return nil, err
	}

	if !from.CanDebit(req.Amount) {
		if err := transfer.MarkFailed(domain.ErrInsufficientFunds.Error()); err != nil {
			return nil, err
		}
	} else {
		if err := tx.Wallets().UpdateBalance(ctx, from.ID, from.Balance-req.Amount); err != nil {
			return nil, err
		}
		if err := tx.Wallets().UpdateBalance(ctx, to.ID, to.Balance+req.Amount); err != nil {
			return nil, err
		}
		debit, credit := domain.DoubleEntry(transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
		if err := tx.Ledger().InsertPair(ctx, debit, credit); err != nil {
			return nil, err
		}
		if err := transfer.MarkProcessed(); err != nil {
			return nil, err
		}
	}

	if err := tx.Transfers().UpdateState(ctx, transfer); err != nil {
		return nil, err
	}

	result := toResult(transfer)
	body, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal transfer result: %w", err)
	}
	if err := tx.Idempotency().Complete(ctx, req.IdempotencyKey, transfer.ID, 201, body); err != nil {
		return nil, err
	}
	return result, nil
}

// lockWalletPair takes row locks on both wallets in a deterministic
// order (lowest ID first) regardless of transfer direction, so two
// concurrent transfers that share a wallet - in either direction - never
// deadlock against each other waiting on the opposite lock order.
func lockWalletPair(ctx context.Context, wallets repository.WalletRepository, fromID, toID string) (from, to *domain.Wallet, err error) {
	firstID, secondID := fromID, toID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	w1, err := wallets.GetForUpdate(ctx, firstID)
	if err != nil {
		return nil, nil, err
	}
	w2, err := wallets.GetForUpdate(ctx, secondID)
	if err != nil {
		return nil, nil, err
	}
	if w1.ID == fromID {
		return w1, w2, nil
	}
	return w2, w1, nil
}

// resolveExisting interprets a previously (or concurrently) claimed
// idempotency record. It returns (nil, false, nil) when the record
// exists but is still IN_PROGRESS elsewhere and there is nothing to
// replay yet - the caller decides how to report that.
func resolveExisting(existing *domain.IdempotencyRecord, reqHash string) (*TransferResult, bool, error) {
	if existing == nil {
		return nil, false, nil
	}
	if existing.RequestHash != reqHash {
		return nil, false, domain.ErrIdempotencyKeyConflict
	}
	if existing.Status != domain.IdempotencyCompleted {
		return nil, false, nil
	}
	var res TransferResult
	if err := json.Unmarshal(existing.ResponseBody, &res); err != nil {
		return nil, false, fmt.Errorf("decode cached transfer result: %w", err)
	}
	return &res, true, nil
}

func toResult(t *domain.Transfer) *TransferResult {
	return &TransferResult{
		TransferID:     t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		State:          t.State,
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
	}
}

func hashRequest(req CreateTransferRequest) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d", req.FromWalletID, req.ToWalletID, req.Amount)
	return hex.EncodeToString(h.Sum(nil))
}

func validate(req CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return domain.ErrInvalidRequest
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return domain.ErrInvalidRequest
	}
	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWallet
	}
	if req.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	return nil
}
