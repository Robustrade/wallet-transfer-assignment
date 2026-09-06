package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

// WalletService covers wallet lifecycle operations that sit outside the
// transfer workflow: creation (for test/demo setup) and balance lookup.
type WalletService struct {
	uow repository.UnitOfWork
	now func() time.Time
}

func NewWalletService(uow repository.UnitOfWork) *WalletService {
	return &WalletService{uow: uow, now: time.Now}
}

var ErrWalletAlreadyExists = errors.New("wallet already exists")

// CreateWallet opens a wallet with the given starting balance. This is a
// setup/demo convenience — the assignment does not require a wallet
// lifecycle API, only that transfers move funds between existing wallets.
func (s *WalletService) CreateWallet(ctx context.Context, id string, initialBalance int64) (*domain.Wallet, error) {
	if initialBalance < 0 {
		return nil, domain.ErrInvalidAmount
	}
	now := s.now()
	w := &domain.Wallet{ID: id, Balance: initialBalance, CreatedAt: now, UpdatedAt: now}

	err := s.uow.RunWriteTx(ctx, func(ctx context.Context, repos repository.Repos) error {
		if _, err := repos.Wallets.Get(ctx, id); err == nil {
			return ErrWalletAlreadyExists
		} else if !errors.Is(err, domain.ErrWalletNotFound) {
			return err
		}
		return repos.Wallets.Create(ctx, w)
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	var w *domain.Wallet
	err := s.uow.RunReadTx(ctx, func(ctx context.Context, repos repository.Repos) error {
		found, err := repos.Wallets.Get(ctx, id)
		if err != nil {
			return err
		}
		w = found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// GetTransfer returns a transfer and its ledger entries, for the optional
// GET /transfers/{id} endpoint.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, []domain.LedgerEntry, error) {
	var (
		t       *domain.Transfer
		entries []domain.LedgerEntry
	)
	err := s.uow.RunReadTx(ctx, func(ctx context.Context, repos repository.Repos) error {
		found, err := repos.Transfers.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrTransferNotFound
			}
			return err
		}
		t = found

		found2, err := repos.Ledger.ListByTransfer(ctx, t.ID)
		if err != nil {
			return err
		}
		entries = found2
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return t, entries, nil
}
