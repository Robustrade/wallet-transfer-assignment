package app

import (
	"context"
	"database/sql"
	"sort"

	"github.com/google/uuid"
)

type TransferResult struct {
	Transfer *Transfer
	Existing bool
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateWallet(ctx context.Context, req *CreateWalletRequest) (*Wallet, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	id := req.ID
	if id == "" {
		id = uuid.NewString()
	}
	wallet := &Wallet{ID: id, Balance: req.Balance}
	err := s.repo.Transact(ctx, func(tx *sql.Tx) error {
		return s.repo.CreateWallet(ctx, tx, wallet)
	})
	if err != nil {
		return nil, err
	}
	return wallet, nil
}

func (s *Service) GetWallet(ctx context.Context, walletID string) (*Wallet, error) {
	return s.repo.GetWalletByID(ctx, walletID)
}

func (s *Service) CreateTransfer(ctx context.Context, req *CreateTransferRequest) (*TransferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, ErrInvalidRequest
	}

	var transferResult TransferResult
	var transactionalError error

	err := s.repo.Transact(ctx, func(tx *sql.Tx) error {
		existing, err := s.repo.GetTransferByIdempotencyKey(ctx, tx, req.IdempotencyKey)
		if err == nil {
			transferResult.Transfer = existing
			transferResult.Existing = true
			if existing.State == StateFailed {
				transactionalError = ErrInsufficientFunds
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}

		transfer := &Transfer{
			ID:             uuid.NewString(),
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
			State:          StatePending,
			IdempotencyKey: req.IdempotencyKey,
		}

		if err := s.repo.CreateTransfer(ctx, tx, transfer); err != nil {
			if IsUniqueConstraintError(err) {
				existing, err2 := s.repo.GetTransferByIdempotencyKey(ctx, tx, req.IdempotencyKey)
				if err2 != nil {
					return err2
				}
				transferResult.Transfer = existing
				transferResult.Existing = true
				if existing.State == StateFailed {
					transactionalError = ErrInsufficientFunds
				}
				return nil
			}
			return err
		}

		walletIDs := uniqueSortedIDs(req.FromWalletID, req.ToWalletID)
		wallets, err := s.repo.FindWallets(ctx, tx, walletIDs...)
		if err != nil {
			_ = s.repo.UpdateTransferState(ctx, tx, transfer.ID, StateFailed)
			transfer.State = StateFailed
			transferResult.Transfer = transfer
			transferResult.Existing = false
			return err
		}

		fromWallet, ok := wallets[req.FromWalletID]
		if !ok {
			_ = s.repo.UpdateTransferState(ctx, tx, transfer.ID, StateFailed)
			transfer.State = StateFailed
			transferResult.Transfer = transfer
			return ErrWalletNotFound
		}
		toWallet, ok := wallets[req.ToWalletID]
		if !ok {
			_ = s.repo.UpdateTransferState(ctx, tx, transfer.ID, StateFailed)
			transfer.State = StateFailed
			transferResult.Transfer = transfer
			return ErrWalletNotFound
		}

		if fromWallet.Balance < transfer.Amount {
			if err := s.repo.UpdateTransferState(ctx, tx, transfer.ID, StateFailed); err != nil {
				return err
			}
			transfer.State = StateFailed
			transferResult.Transfer = transfer
			transferResult.Existing = false
			transactionalError = ErrInsufficientFunds
			return nil
		}

		if err := s.repo.AdjustWalletBalance(ctx, tx, fromWallet.ID, -transfer.Amount); err != nil {
			return err
		}
		if err := s.repo.AdjustWalletBalance(ctx, tx, toWallet.ID, transfer.Amount); err != nil {
			return err
		}

		entries := []LedgerEntry{
			{WalletID: fromWallet.ID, TransferID: transfer.ID, Type: LedgerEntryDebit, Amount: transfer.Amount},
			{WalletID: toWallet.ID, TransferID: transfer.ID, Type: LedgerEntryCredit, Amount: transfer.Amount},
		}
		if err := s.repo.CreateLedgerEntries(ctx, tx, entries); err != nil {
			return err
		}

		if err := s.repo.UpdateTransferState(ctx, tx, transfer.ID, StateProcessed); err != nil {
			return err
		}
		transfer.State = StateProcessed
		transferResult.Transfer = transfer
		transferResult.Existing = false
		return nil
	})
	if err != nil {
		return nil, err
	}
	if transferResult.Transfer == nil {
		return nil, ErrInvalidRequest
	}
	if transferResult.Existing && transferResult.Transfer.State == StateFailed {
		transactionalError = ErrInsufficientFunds
	}
	return &transferResult, transactionalError
}

func uniqueSortedIDs(ids ...string) []string {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
