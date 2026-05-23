package wallet

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"shindechandrakant/internal/api/dtos"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Service interface {
	Transfer(ctx context.Context, req dtos.TransferRequest) (*Transfer, error)
}

type walletService struct {
	repo Repository
}

func NewWalletService(repo Repository) Service {
	return &walletService{repo: repo}
}

func (s *walletService) Transfer(ctx context.Context, req dtos.TransferRequest) (*Transfer, error) {
	if req.FromWalletId == req.ToWalletId {
		return nil, ErrSameWallet
	}

	// math.Round mitigates float64 representational imprecision (e.g. 0.1 + 0.2 != 0.3).
	// All monetary values are stored as integer minor units (e.g. 10.34 → 1034).
	amount := int64(math.Round(req.Amount * 100))

	// Idempotency check — return original result for duplicate requests
	existing, err := s.repo.FindTransferByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Always lock wallets in a consistent alphabetical order to prevent deadlocks
	// when two concurrent transfers involve the same pair of wallets in opposite directions.
	firstID, secondID := req.FromWalletId, req.ToWalletId
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	w1, err := s.repo.GetWalletByIDForUpdate(ctx, tx, firstID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	w2, err := s.repo.GetWalletByIDForUpdate(ctx, tx, secondID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}

	fromWallet, toWallet := w1, w2
	if w1.WalletId == req.ToWalletId {
		fromWallet, toWallet = w2, w1
	}

	if fromWallet.Status != WalletStatusActive || toWallet.Status != WalletStatusActive {
		return nil, ErrWalletInactive
	}

	if fromWallet.Balance < amount {
		return nil, ErrInsufficientFunds
	}

	transfer := &Transfer{
		TransactionId:  uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletId:   req.FromWalletId,
		ToWalletId:     req.ToWalletId,
		Amount:         amount,
		State:          StatePending,
	}

	if err := s.repo.CreateTransfer(ctx, tx, transfer); err != nil {
		// Another concurrent request with the same idempotency key beat us to it.
		// errors.As unwraps correctly even if the error is wrapped by the driver.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return s.repo.FindTransferByIdempotencyKey(ctx, req.IdempotencyKey)
		}
		return nil, err
	}

	newFromBalance := fromWallet.Balance - amount
	newToBalance := toWallet.Balance + amount

	if err := s.repo.UpdateWalletBalance(ctx, tx, fromWallet.WalletId, newFromBalance); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateWalletBalance(ctx, tx, toWallet.WalletId, newToBalance); err != nil {
		return nil, err
	}

	entries := []LedgerEntry{
		{
			EntryId:       uuid.New().String(),
			TransactionId: transfer.TransactionId,
			WalletId:      fromWallet.WalletId,
			Amount:        amount,
			Entry:         LedgerEntryDebit,
			BalanceBefore: fromWallet.Balance,
			BalanceAfter:  newFromBalance,
		},
		{
			EntryId:       uuid.New().String(),
			TransactionId: transfer.TransactionId,
			WalletId:      toWallet.WalletId,
			Amount:        amount,
			Entry:         LedgerEntryCredit,
			BalanceBefore: toWallet.Balance,
			BalanceAfter:  newToBalance,
		},
	}

	if err := s.repo.CreateLedgerEntries(ctx, tx, entries); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.TransactionId, StateProcessed); err != nil {
		return nil, err
	}
	transfer.State = StateProcessed

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return transfer, nil
}
