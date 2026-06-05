package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

var ErrInsufficientFunds = errors.New("insufficient funds")

type TransferService struct {
	db *pgxpool.Pool

	walletRepo repository.WalletRepository

	transferRepo repository.TransferRepository

	ledgerRepo repository.LedgerRepository
}

func NewTransferService(
	db *pgxpool.Pool,
	w repository.WalletRepository,
	t repository.TransferRepository,
	l repository.LedgerRepository,
) *TransferService {

	return &TransferService{
		db: db,

		walletRepo:   w,
		transferRepo: t,
		ledgerRepo:   l,
	}
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (*domain.Transfer, error) {

	tx, err := s.db.Begin(ctx)

	if err != nil {
		return nil, err
	}

	defer tx.Rollback(ctx)

	transfer := &domain.Transfer{
		ID: uuid.New(),

		IdempotencyKey: req.IdempotencyKey,

		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,

		Amount: req.Amount,

		State: domain.Pending,
	}

	inserted, err :=
		s.transferRepo.CreateIfNotExists(
			ctx,
			tx,
			transfer,
		)

	if err != nil {
		return nil, err
	}

	/*
		Idempotency hit.
		Return existing transfer.
	*/

	if !inserted {

		existing, err :=
			s.transferRepo.GetByIdempotencyKey(
				ctx,
				tx,
				req.IdempotencyKey,
			)

		if err != nil {
			return nil, err
		}

		err = tx.Commit(ctx)

		if err != nil {
			return nil, err
		}

		return existing, nil
	}

	/*
		Deadlock prevention.

		Always acquire locks
		in deterministic order.
	*/

	firstID := req.FromWalletID
	secondID := req.ToWalletID

	if firstID.String() > secondID.String() {
		firstID, secondID =
			secondID, firstID
	}

	firstWallet, err :=
		s.walletRepo.GetForUpdate(
			ctx,
			tx,
			firstID,
		)

	if err != nil {
		return nil, err
	}

	secondWallet, err :=
		s.walletRepo.GetForUpdate(
			ctx,
			tx,
			secondID,
		)

	if err != nil {
		return nil, err
	}

	var fromWallet *domain.Wallet
	var toWallet *domain.Wallet

	if firstWallet.ID ==
		req.FromWalletID {

		fromWallet = firstWallet
		toWallet = secondWallet

	} else {

		fromWallet = secondWallet
		toWallet = firstWallet
	}

	//Insufficient Funds Check
	if fromWallet.Balance <
		req.Amount {

		_ = transfer.MarkFailed()

		err =
			s.transferRepo.UpdateState(
				ctx,
				tx,
				transfer.ID,
				domain.Failed,
			)

		if err != nil {
			return nil, err
		}

		err = tx.Commit(ctx)

		if err != nil {
			return nil, err
		}

		return nil,
			ErrInsufficientFunds
	}

	//Balance Update
	fromWallet.Balance -= req.Amount

	toWallet.Balance += req.Amount

	err =
		s.walletRepo.UpdateBalance(
			ctx,
			tx,
			fromWallet.ID,
			fromWallet.Balance,
		)

	if err != nil {
		return nil, err
	}

	err =
		s.walletRepo.UpdateBalance(
			ctx,
			tx,
			toWallet.ID,
			toWallet.Balance,
		)

	if err != nil {
		return nil, err
	}

	//Ledger Entry Creation
	debitEntry :=
		&domain.LedgerEntry{
			ID: uuid.New(),

			TransferID: transfer.ID,

			WalletID: fromWallet.ID,

			Type: domain.Debit,

			Amount: req.Amount,
		}

	creditEntry :=
		&domain.LedgerEntry{
			ID: uuid.New(),

			TransferID: transfer.ID,

			WalletID: toWallet.ID,

			Type: domain.Credit,

			Amount: req.Amount,
		}

	err =
		s.ledgerRepo.CreateEntry(
			ctx,
			tx,
			debitEntry,
		)

	if err != nil {
		return nil, err
	}

	err =
		s.ledgerRepo.CreateEntry(
			ctx,
			tx,
			creditEntry,
		)

	if err != nil {
		return nil, err
	}

	//Transfer State Update
	err = transfer.MarkProcessed()

	if err != nil {
		return nil, err
	}

	err =
		s.transferRepo.UpdateState(
			ctx,
			tx,
			transfer.ID,
			domain.Processed,
		)

	if err != nil {
		return nil, err
	}

	err = tx.Commit(ctx)

	if err != nil {
		return nil, err
	}

	return transfer, nil
}
