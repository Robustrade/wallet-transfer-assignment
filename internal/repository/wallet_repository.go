package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
)

type WalletRepository struct {
	db *pgxpool.Pool
}

func NewWalletRepository(db *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) (*domain.Wallet, error) {
	if wallet.ID == "" {
		return r.createWithGeneratedID(ctx, wallet.Balance)
	}

	return r.createWithProvidedID(ctx, wallet)
}

func (r *WalletRepository) createWithGeneratedID(ctx context.Context, balance int64) (*domain.Wallet, error) {
	var wallet domain.Wallet

	err := r.db.QueryRow(ctx, `
		INSERT INTO wallets (balance)
		VALUES ($1)
		RETURNING wallet_id, balance
	`, balance).Scan(&wallet.ID, &wallet.Balance)
	if err != nil {
		return nil, err
	}

	return &wallet, nil
}

func (r *WalletRepository) createWithProvidedID(ctx context.Context, wallet *domain.Wallet) (*domain.Wallet, error) {
	var created domain.Wallet

	err := r.db.QueryRow(ctx, `
		INSERT INTO wallets (wallet_id, balance)
		VALUES ($1, $2)
		RETURNING wallet_id, balance
	`, wallet.ID, wallet.Balance).Scan(&created.ID, &created.Balance)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {

			if pgErr.Code == "23505" {

				return nil, domain.ErrWalletAlreadyExists
			}

		}
		return nil, err
	}

	return &created, nil
}

func (r *WalletRepository) Get(ctx context.Context, walletID string) (*domain.Wallet, error) {
	var wallet domain.Wallet

	err := r.db.QueryRow(ctx, `
		SELECT wallet_id, balance
		FROM wallets
		WHERE wallet_id = $1
	`, walletID).Scan(&wallet.ID, &wallet.Balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}

		return nil, err
	}

	return &wallet, nil
}
