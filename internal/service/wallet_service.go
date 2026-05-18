package service

import (
	"context"

	"errors"
	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"

	"github.com/jackc/pgx/v5"
)

type WalletService struct {
	db         *pgx.Conn
	walletRepo *repository.WalletRepository
}

func NewWalletService(db *pgx.Conn, walletRepo *repository.WalletRepository) *WalletService {
	return &WalletService{
		db:         db,
		walletRepo: walletRepo,
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

	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	return nil

}
