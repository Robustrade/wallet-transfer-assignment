package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type TransferService interface {
	CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.CreateTransferResponse, error)
}

type transferService struct {
	db              *sql.DB
	walletRepo      repository.WalletRepository
	transferRepo    repository.TransferRepository
	ledgerRepo      repository.LedgerRepository
	idempotencyRepo repository.IdempotencyRepository
}

func NewTransferService(
	db *sql.DB,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	idempotencyRepo repository.IdempotencyRepository,
) TransferService {
	return &transferService{
		db:              db,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
}

var ErrInsufficientFunds = errors.New("insufficient funds")
var ErrWalletNotFound = errors.New("wallet not found")

// CreateTransfer implements TransferService.
func (t *transferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.CreateTransferResponse, error) {
	err := validateCreateTransferRequest(req)
	if err != nil {
		return nil, err
	}

	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	existingTransferId, err := t.idempotencyRepo.GetByKey(ctx, tx, req.IdempotencyKey)
	if err == nil {
		transfer, err := t.transferRepo.GetById(ctx, tx, existingTransferId)
		if err != nil {
			return nil, err
		}

		if transfer.FromWalletId != req.FromWalletId || transfer.ToWalletId != req.ToWalletId || transfer.Amount != req.Amount {
			return nil, ErrInvalidRequest
		}

		if transfer.Status == domain.TransferFailed {
			return nil, ErrInsufficientFunds
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}

		return &domain.CreateTransferResponse{
			TransferId:   transfer.Id,
			Status:       string(transfer.Status),
			FromWalletId: transfer.FromWalletId,
			ToWalletId:   transfer.ToWalletId,
			Amount:       transfer.Amount,
		}, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := time.Now()
	transferId := uuid.New().String()

	err = t.transferRepo.Create(ctx, tx, &domain.Transfer{
		Id:             transferId,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletId:   req.FromWalletId,
		ToWalletId:     req.ToWalletId,
		Amount:         req.Amount,
		Status:         domain.TransferPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		if isWalletForeignKeyError(err) {
			return nil, ErrWalletNotFound
		}

		return nil, err
	}

	err = t.idempotencyRepo.Create(ctx, tx, uuid.New().String(), req.IdempotencyKey, transferId)
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyKeyExists) {
			err := tx.Rollback()
			if err != nil {
				return nil, err
			}

			return t.getExistingTransferResponse(ctx, req.IdempotencyKey)
		}

		return nil, err
	}

	fromWallet, toWallet, err := t.lockWallets(ctx, tx, req.FromWalletId, req.ToWalletId)
	if err != nil {
		return nil, err
	}

	if fromWallet.Balance < req.Amount {
		err = t.transferRepo.UpdateStatus(ctx, tx, transferId, domain.TransferFailed)
		if err != nil {
			return nil, err
		}

		err = tx.Commit()
		if err != nil {
			return nil, err
		}

		return nil, ErrInsufficientFunds
	}

	newFromBalance := fromWallet.Balance - req.Amount
	newToBalance := toWallet.Balance + req.Amount

	err = t.walletRepo.UpdateBalance(ctx, tx, fromWallet.Id, newFromBalance)
	if err != nil {
		return nil, err
	}

	err = t.walletRepo.UpdateBalance(ctx, tx, toWallet.Id, newToBalance)
	if err != nil {
		return nil, err
	}

	err = t.ledgerRepo.Create(ctx, tx, &domain.LedgerEntry{
		Id:         uuid.New().String(),
		TransferId: transferId,
		WalletId:   fromWallet.Id,
		EntryType:  domain.LedgerDebit,
		Amount:     req.Amount,
		CreatedAt:  now,
	})
	if err != nil {
		return nil, err
	}

	err = t.ledgerRepo.Create(ctx, tx, &domain.LedgerEntry{
		Id:         uuid.New().String(),
		TransferId: transferId,
		WalletId:   toWallet.Id,
		EntryType:  domain.LedgerCredit,
		Amount:     req.Amount,
		CreatedAt:  now,
	})
	if err != nil {
		return nil, err
	}

	err = t.transferRepo.UpdateStatus(ctx, tx, transferId, domain.TransferProcessed)
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return &domain.CreateTransferResponse{
		TransferId:   transferId,
		Status:       string(domain.TransferProcessed),
		FromWalletId: req.FromWalletId,
		ToWalletId:   req.ToWalletId,
		Amount:       req.Amount,
	}, nil
}

func (t *transferService) lockWallets(ctx context.Context, tx *sql.Tx, fromWalletId, toWalletId string) (*domain.Wallet, *domain.Wallet, error) {
	var fromWallet *domain.Wallet
	var toWallet *domain.Wallet

	if fromWalletId < toWalletId {
		wallet, err := t.walletRepo.GetWalletForUpdate(ctx, tx, fromWalletId)
		if err != nil {
			if errors.Is(err, repository.ErrWalletNotFound) {
				return nil, nil, ErrWalletNotFound
			}
			return nil, nil, err
		}
		fromWallet = wallet

		wallet, err = t.walletRepo.GetWalletForUpdate(ctx, tx, toWalletId)
		if err != nil {
			if errors.Is(err, repository.ErrWalletNotFound) {
				return nil, nil, ErrWalletNotFound
			}
			return nil, nil, err
		}
		toWallet = wallet
	} else {
		wallet, err := t.walletRepo.GetWalletForUpdate(ctx, tx, toWalletId)
		if err != nil {
			if errors.Is(err, repository.ErrWalletNotFound) {
				return nil, nil, ErrWalletNotFound
			}
			return nil, nil, err
		}
		toWallet = wallet

		wallet, err = t.walletRepo.GetWalletForUpdate(ctx, tx, fromWalletId)
		if err != nil {
			if errors.Is(err, repository.ErrWalletNotFound) {
				return nil, nil, ErrWalletNotFound
			}
			return nil, nil, err
		}
		fromWallet = wallet
	}

	return fromWallet, toWallet, nil
}

func (t *transferService) getExistingTransferResponse(ctx context.Context, idempotencyKey string) (*domain.CreateTransferResponse, error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	transferId, err := t.idempotencyRepo.GetByKey(ctx, tx, idempotencyKey)
	if err != nil {
		return nil, err
	}

	transfer, err := t.transferRepo.GetById(ctx, tx, transferId)
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return &domain.CreateTransferResponse{
		TransferId:   transfer.Id,
		Status:       string(transfer.Status),
		FromWalletId: transfer.FromWalletId,
		ToWalletId:   transfer.ToWalletId,
		Amount:       transfer.Amount,
	}, nil
}

func isWalletForeignKeyError(err error) bool {
	var pqErr *pq.Error

	if !errors.As(err, &pqErr) {
		return false
	}

	if pqErr.Code != "23503" {
		return false
	}

	return pqErr.Constraint == "fk_transfers_from_wallet" ||
		pqErr.Constraint == "fk_transfers_to_wallet"
}
