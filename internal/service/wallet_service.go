package service

import (
	"context"

	"errors"
	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type WalletService struct {
	db           *pgx.Conn
	walletRepo   *repository.WalletRepository
	transferRepo *repository.TransferRepository
	ledgerRepo   *repository.LedgerRepository
}

func NewWalletService(db *pgx.Conn, walletRepo *repository.WalletRepository, transferRepo *repository.TransferRepository, ledgerRepo *repository.LedgerRepository) *WalletService {
	return &WalletService{
		db:           db,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.walletRepo.GetWalletByID(ctx, id)
}

func (s *WalletService) CreateTransfer(fromWalletID string, toWalletID string, amount int64, idempotencyKey string) error {

	if amount <= 0 {
		return errors.New("amount must be greater than zero")
	}

	if fromWalletID == toWalletID {
		return errors.New("cannot transfer to same wallet")
	}

	if idempotencyKey == "" {
		return errors.New("idempotency key is required")
	}

	ctx := context.Background()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	_, err = s.transferRepo.GetTransferByIdempotencyKey(
		ctx,
		tx,
		idempotencyKey,
	)

	if err == nil {
		return nil
	}

	if err != pgx.ErrNoRows {
		return err
	}

	fromWallet, err := s.walletRepo.GetWalletByIDForUpdate(ctx, tx, fromWalletID)
	if err != nil {
		return err
	}

	toWallet, err := s.walletRepo.GetWalletByIDForUpdate(
		ctx,
		tx,
		toWalletID,
	)
	if err != nil {
		return err
	}

	if fromWallet.Balance < amount {
		return errors.New("insufficient balance")
	}

	newFromBalance := fromWallet.Balance - amount
	newToBalance := toWallet.Balance + amount

	err = s.walletRepo.UpdateWalletBalance(ctx, tx, fromWalletID, newFromBalance)
	if err != nil {
		return err
	}

	err = s.walletRepo.UpdateWalletBalance(ctx, tx, toWalletID, newToBalance)
	if err != nil {
		return err
	}

	transfer := &domain.Transfer{
		ID:             uuid.New().String(),
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         domain.TransferProcessed,
	}

	err = s.transferRepo.CreateTransfer(
		ctx,
		tx,
		transfer,
	)
	if err != nil {
		return err
	}

	debitEntry := &domain.LedgerEntry{
		ID:         uuid.New().String(),
		WalletID:   fromWalletID,
		TransferID: transfer.ID,
		EntryType:  domain.LedgerEntryDebit,
		Amount:     amount,
	}

	err = s.ledgerRepo.CreateLedgerEntry(
		ctx,
		tx,
		debitEntry,
	)
	if err != nil {
		return err
	}

	creditEntry := &domain.LedgerEntry{
		ID:         uuid.New().String(),
		WalletID:   toWalletID,
		TransferID: transfer.ID,
		EntryType:  domain.LedgerEntryCredit,
		Amount:     amount,
	}

	err = s.ledgerRepo.CreateLedgerEntry(
		ctx,
		tx,
		creditEntry,
	)
	if err != nil {
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	return nil

}
