package entrepo

import (
	"context"

	"github.com/google/uuid"

	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/ent/wallet"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type WalletRepository struct {
	client *ent.Client
}

func NewWalletRepository(client *ent.Client) *WalletRepository {
	return &WalletRepository{
		client: client,
	}
}

func (r *WalletRepository) FindByID(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Wallet, error) {
	w, err := r.client.Wallet.
		Query().
		Where(wallet.IDEQ(id)).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, repository.ErrNotFound
		}

		return nil, err
	}

	return toDomainWallet(w), nil
}

func (r *WalletRepository) FindByIDsForUpdate(
	ctx context.Context,
	ids ...uuid.UUID,
) ([]*domain.Wallet, error) {
	wallets, err := r.client.Wallet.
		Query().
		Where(wallet.IDIn(ids...)).
		Order(ent.Asc(wallet.FieldID)).
		ForUpdate().
		All(ctx)

	if err != nil {
		return nil, err
	}

	result := make([]*domain.Wallet, 0, len(wallets))

	for _, w := range wallets {
		result = append(result, toDomainWallet(w))
	}

	return result, nil
}

func (r *WalletRepository) Debit(
	ctx context.Context,
	id uuid.UUID,
	amount int64,
) error {
	affected, err := r.client.Wallet.
		Update().
		Where(
			wallet.IDEQ(id),
			wallet.BalanceGTE(amount),
		).
		AddBalance(-amount).
		Save(ctx)

	if err != nil {
		return err
	}

	if affected == 0 {
		return domain.ErrInsufficientFunds
	}

	return nil
}

func (r *WalletRepository) Credit(
	ctx context.Context,
	id uuid.UUID,
	amount int64,
) error {
	affected, err := r.client.Wallet.
		Update().
		Where(wallet.IDEQ(id)).
		AddBalance(amount).
		Save(ctx)

	if err != nil {
		return err
	}

	if affected == 0 {
		return repository.ErrNotFound
	}

	return nil
}

func toDomainWallet(w *ent.Wallet) *domain.Wallet {
	return &domain.Wallet{
		ID:        w.ID,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
}
