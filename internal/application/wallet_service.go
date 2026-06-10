package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
	"github.com/google/uuid"
)

type WalletService struct {
	walletRepo repository.WalletRepository
	ledgerRepo repository.LedgerRepository
}

func NewWalletService(walletRepo repository.WalletRepository, ledgerRepo repository.LedgerRepository) *WalletService {
	return &WalletService{walletRepo: walletRepo, ledgerRepo: ledgerRepo}
}

func (s *WalletService) Create(ctx context.Context, id string, balance int64) (*domain.Wallet, error) {
	if id == "" {
		id = uuid.NewString()
	} else {
		if _, err := uuid.Parse(id); err != nil {
			return nil, domain.ErrInvalidWalletIDFormat
		}
	}
	wallet, err := domain.NewWallet(id, balance)
	if err != nil {
		return nil, err
	}
	if err := s.walletRepo.Create(ctx, wallet); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, err
		}
		return nil, err
	}
	return wallet, nil
}

func (s *WalletService) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	if id == "" {
		return nil, domain.ErrEmptyWalletID
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, domain.ErrInvalidWalletIDFormat
	}
	return s.walletRepo.GetByID(ctx, id)
}

type WalletReconciliation struct {
	WalletID      string
	StoredBalance int64
	LedgerBalance int64
	Match         bool
}

func (s *WalletService) Reconcile(ctx context.Context, id string) (*WalletReconciliation, error) {
	if id == "" {
		return nil, domain.ErrEmptyWalletID
	}

	wallet, err := s.walletRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	entries, err := s.ledgerRepo.GetByWalletID(ctx, id)
	if err != nil {
		return nil, err
	}

	ledgerBalance, err := calculateLedgerBalance(entries)
	if err != nil {
		return nil, err
	}

	return &WalletReconciliation{
		WalletID:      wallet.ID,
		StoredBalance: wallet.Balance,
		LedgerBalance: ledgerBalance,
		Match:         wallet.Balance == ledgerBalance,
	}, nil
}

func calculateLedgerBalance(entries []domain.LedgerEntry) (int64, error) {
	var balance int64
	for _, entry := range entries {
		switch entry.EntryType {
		case domain.LedgerEntryTypeDebit:
			balance -= entry.Amount
		case domain.LedgerEntryTypeCredit:
			balance += entry.Amount
		default:
			return 0, fmt.Errorf("invalid ledger entry type: %s", entry.EntryType)
		}
	}
	return balance, nil
}
