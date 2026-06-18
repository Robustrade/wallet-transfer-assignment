package service

import (
	"context"
	"errors"
	"wallet-transfer/apperrors"
	"wallet-transfer/domain"
	"wallet-transfer/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidAmount      = errors.New("invalid amount")
	ErrSameWallet         = errors.New("same wallet")
	ErrWalletNotFound     = errors.New("wallet not found")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrMissingIdempotency = errors.New("missing idempotency key")
)

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferService struct {
	db *pgxpool.Pool
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
}

func NewTransferService(
	db *pgxpool.Pool,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		db:           db,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (*domain.Transfer, error) {

	if req.IdempotencyKey == "" {
		return nil, ErrMissingIdempotency
	}

	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, ErrSameWallet
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	wallets, err := s.walletRepo.LockByIDs(
		ctx,
		tx,
		[]string{req.FromWalletID, req.ToWalletID},
	)
	if err != nil {
		return nil, err
	}

	if len(wallets) != 2 {
		return nil, ErrWalletNotFound
	}

	walletMap := make(map[string]domain.Wallet)
	for _, w := range wallets {
		walletMap[w.ID] = w
	}

	source := walletMap[req.FromWalletID]
	destination := walletMap[req.ToWalletID]

	transfer := &domain.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.Pending,
	}

	err = s.transferRepo.Create(ctx, tx, transfer)
	if err != nil {

		if errors.Is(err, apperrors.ErrDuplicateIdempotencyKey) {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return nil, rollbackErr
			}
			committed = true
			return s.transferRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
		}

		return nil, err
	}

	if source.Balance < req.Amount {

		transfer.MarkFailed()

		if err = s.transferRepo.UpdateStatus(ctx, tx, transfer.ID, domain.Failed); err != nil {
			return nil, err
		}

		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		committed = true

		return nil, ErrInsufficientFunds
	}

	if err = s.walletRepo.UpdateBalance(ctx, tx, source.ID, -req.Amount); err != nil {
		return nil, err
	}

	if err = s.walletRepo.UpdateBalance(ctx, tx, destination.ID, req.Amount); err != nil {
		return nil, err
	}

	debit := &domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transfer.ID,
		WalletID:   source.ID,
		Type:       domain.Debit,
		Amount:     req.Amount,
	}

	if err = s.ledgerRepo.Create(ctx, tx, debit); err != nil {
		return nil, err
	}

	credit := &domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transfer.ID,
		WalletID:   destination.ID,
		Type:       domain.Credit,
		Amount:     req.Amount,
	}

	if err = s.ledgerRepo.Create(ctx, tx, credit); err != nil {
		return nil, err
	}

	if err = transfer.MarkProcessed(); err != nil {
		return nil, err
	}

	if err = s.transferRepo.UpdateStatus(ctx, tx, transfer.ID, domain.Processed); err != nil {
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	return transfer, nil
}
