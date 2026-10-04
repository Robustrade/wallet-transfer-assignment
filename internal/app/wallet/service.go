package wallet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/amantiwari/wallet-transfer/internal/app/port"
	"github.com/amantiwari/wallet-transfer/internal/domain"
	"github.com/amantiwari/wallet-transfer/internal/platform/requestid"
)

// CreateCommand opens a wallet.
type CreateCommand struct {
	ID             string
	InitialBalance int64
}

// Service creates and reads wallets.
type Service struct {
	uow     port.UnitOfWork
	wallets port.WalletReader
	clock   port.Clock
	log     *slog.Logger
}

// NewService constructs the wallet use case.
func NewService(uow port.UnitOfWork, wallets port.WalletReader, clock port.Clock, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{uow: uow, wallets: wallets, clock: clock, log: log}
}

// Create persists a new wallet. The opening balance is not a ledger transfer.
func (s *Service) Create(ctx context.Context, cmd CreateCommand) (domain.Wallet, error) {
	wallet, err := domain.NewWallet(cmd.ID, cmd.InitialBalance, s.clock.Now())
	if err != nil {
		return domain.Wallet{}, err
	}
	err = s.uow.WithinTransaction(ctx, func(ctx context.Context, repos port.Repositories) error {
		if err := repos.Wallets().Create(ctx, wallet); err != nil {
			return fmt.Errorf("insert wallet: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Wallet{}, err
	}
	s.log.InfoContext(ctx, "wallet created",
		"request_id", requestid.From(ctx),
		"wallet_id", wallet.ID,
		"initial_balance", wallet.InitialBalance,
	)
	return wallet, nil
}

// Get returns one wallet.
func (s *Service) Get(ctx context.Context, id string) (domain.Wallet, error) {
	if err := domain.ValidateWalletID(id); err != nil {
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			for i := range validation.Details {
				validation.Details[i].Field = "walletId"
			}
		}
		return domain.Wallet{}, err
	}
	wallet, err := s.wallets.GetByID(ctx, id)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}
