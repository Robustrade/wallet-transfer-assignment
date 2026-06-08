package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TransferService struct {
	db *repository.DB
}

func NewTransferService(db *repository.DB) *TransferService {
	return &TransferService{db: db}
}

type CreateTransferResult struct {
	Transfer *domain.Transfer
	Replayed bool
}

func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*CreateTransferResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	fromWalletID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid fromWalletId", domain.ErrInvalidWalletID)
	}
	toWalletID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid toWalletId", domain.ErrInvalidWalletID)
	}

	var result *CreateTransferResult
	err = s.db.WithTx(ctx, func(tx pgx.Tx) error {
		var txErr error
		result, txErr = s.createTransferTx(ctx, tx, req, fromWalletID, toWalletID)
		return txErr
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

func (s *TransferService) createTransferTx(
	ctx context.Context,
	tx pgx.Tx,
	req domain.CreateTransferRequest,
	fromWalletID uuid.UUID,
	toWalletID uuid.UUID,
) (*CreateTransferResult, error) {
	existing, err := repository.GetTransferByIdempotencyKey(ctx, tx, req.IdempotencyKey)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		if !transferMatchesRequest(existing, fromWalletID, toWalletID, req.Amount) {
			return nil, ErrIdempotencyConflict
		}
		return &CreateTransferResult{Transfer: existing, Replayed: true}, nil
	}

	wallets, err := lockWalletsInOrder(ctx, tx, fromWalletID, toWalletID)
	if err != nil {
		return nil, err
	}

	source := wallets[fromWalletID]
	if source == nil || wallets[toWalletID] == nil {
		return nil, ErrWalletNotFound
	}

	transfer, err := repository.InsertTransfer(ctx, tx, repository.InsertTransferParams{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferStatusPending,
	})
	if err != nil {
		if repository.IsUniqueViolation(err) {
			existing, lookupErr := repository.GetTransferByIdempotencyKey(ctx, tx, req.IdempotencyKey)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if !transferMatchesRequest(existing, fromWalletID, toWalletID, req.Amount) {
				return nil, ErrIdempotencyConflict
			}
			return &CreateTransferResult{Transfer: existing, Replayed: true}, nil
		}
		return nil, err
	}

	if source.Balance < req.Amount {
		return s.failTransfer(ctx, tx, transfer.ID, ErrInsufficientFunds)
	}

	if err := repository.UpdateWalletBalance(ctx, tx, fromWalletID, -req.Amount); err != nil {
		return nil, err
	}
	if err := repository.UpdateWalletBalance(ctx, tx, toWalletID, req.Amount); err != nil {
		return nil, err
	}

	if _, err := repository.InsertLedgerEntry(ctx, tx, repository.InsertLedgerEntryParams{
		TransferID: transfer.ID,
		WalletID:   fromWalletID,
		Type:       domain.LedgerEntryTypeDebit,
		Amount:     req.Amount,
	}); err != nil {
		return nil, err
	}
	if _, err := repository.InsertLedgerEntry(ctx, tx, repository.InsertLedgerEntryParams{
		TransferID: transfer.ID,
		WalletID:   toWalletID,
		Type:       domain.LedgerEntryTypeCredit,
		Amount:     req.Amount,
	}); err != nil {
		return nil, err
	}

	processed, err := repository.UpdateTransferStatus(ctx, tx, transfer.ID, domain.TransferStatusProcessed, nil)
	if err != nil {
		return nil, err
	}

	return &CreateTransferResult{Transfer: processed, Replayed: false}, nil
}

func (s *TransferService) failTransfer(
	ctx context.Context,
	tx pgx.Tx,
	transferID uuid.UUID,
	cause error,
) (*CreateTransferResult, error) {
	reason := cause.Error()
	failed, err := repository.UpdateTransferStatus(ctx, tx, transferID, domain.TransferStatusFailed, &reason)
	if err != nil {
		return nil, err
	}
	return &CreateTransferResult{Transfer: failed, Replayed: false}, cause
}

func lockWalletsInOrder(ctx context.Context, tx pgx.Tx, walletIDs ...uuid.UUID) (map[uuid.UUID]*domain.Wallet, error) {
	ids := append([]uuid.UUID(nil), walletIDs...)
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})

	unique := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	wallets := make(map[uuid.UUID]*domain.Wallet, len(unique))
	for _, id := range unique {
		wallet, err := repository.GetWalletForUpdate(ctx, tx, id)
		if errors.Is(err, repository.ErrNotFound) {
			wallets[id] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		wallets[id] = wallet
	}
	return wallets, nil
}

func transferMatchesRequest(transfer *domain.Transfer, fromWalletID, toWalletID uuid.UUID, amount int64) bool {
	return transfer.FromWalletID == fromWalletID &&
		transfer.ToWalletID == toWalletID &&
		transfer.Amount == amount
}
