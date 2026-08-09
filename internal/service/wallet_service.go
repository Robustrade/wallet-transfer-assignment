package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/repository"
)

// WalletService manages wallet operations.
type WalletService struct {
	txMgr        repository.TxManager
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
}

// NewWalletService constructs a WalletService instance.
func NewWalletService(
	txMgr repository.TxManager,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
) *WalletService {
	return &WalletService{
		txMgr:        txMgr,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

// CreateWalletInput holds parameter values for wallet creation.
type CreateWalletInput struct {
	ID             string `json:"id,omitempty"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency,omitempty"`
}

// CreateWallet creates a new wallet account and records initial balance ledger credit if balance > 0.
func (s *WalletService) CreateWallet(ctx context.Context, input CreateWalletInput) (*domain.Wallet, error) {
	if input.InitialBalance < 0 {
		return nil, fmt.Errorf("initial balance cannot be negative")
	}

	walletID := input.ID
	if walletID == "" {
		rawUUID := strings.ReplaceAll(uuid.New().String(), "-", "")
		walletID = fmt.Sprintf("wal_%s", rawUUID)
	}

	currency := input.Currency
	if currency == "" {
		currency = "USD"
	}

	now := time.Now().UTC()
	wallet := &domain.Wallet{
		ID:        walletID,
		Balance:   input.InitialBalance,
		Currency:  currency,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := s.txMgr.ExecTx(ctx, func(tx *sql.Tx) error {
		if err := s.walletRepo.Create(ctx, tx, wallet); err != nil {
			return err
		}

		if input.InitialBalance > 0 {
			rawUUID := strings.ReplaceAll(uuid.New().String(), "-", "")
			initTransferID := fmt.Sprintf("trn_init_%s", rawUUID)
			initTransfer := &domain.Transfer{
				ID:             initTransferID,
				IdempotencyKey: fmt.Sprintf("idem_init_%s", walletID),
				FromWalletID:   walletID,
				ToWalletID:     walletID,
				Amount:         input.InitialBalance,
				Status:         domain.TransferStatusProcessed,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if err := s.transferRepo.Create(ctx, tx, initTransfer); err != nil {
				return fmt.Errorf("failed to create initial funding transfer: %w", err)
			}

			creditID := fmt.Sprintf("led_init_c_%s", rawUUID)
			entries := []domain.LedgerEntry{
				{
					ID:         creditID,
					TransferID: initTransferID,
					WalletID:   walletID,
					Type:       domain.LedgerEntryTypeCredit,
					Amount:     input.InitialBalance,
					CreatedAt:  now,
				},
			}
			if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
				return fmt.Errorf("failed to create initial ledger entries: %w", err)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	return wallet, nil
}

// GetWallet retrieves a wallet by ID.
func (s *WalletService) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	if id == "" {
		return nil, domain.ErrInvalidWalletID
	}
	return s.walletRepo.GetByID(ctx, nil, id)
}

// WalletAuditSummary holds wallet details alongside derived ledger audit calculations.
type WalletAuditSummary struct {
	Wallet        *domain.Wallet `json:"wallet"`
	LedgerBalance int64          `json:"ledgerBalance"`
	IsBalanced    bool           `json:"isBalanced"`
}

// AuditWalletBalance compares the stored wallet balance against derived double-entry ledger totals.
func (s *WalletService) AuditWalletBalance(ctx context.Context, id string) (*WalletAuditSummary, error) {
	if id == "" {
		return nil, domain.ErrInvalidWalletID
	}

	wallet, err := s.walletRepo.GetByID(ctx, nil, id)
	if err != nil {
		return nil, err
	}

	ledgerBalance, err := s.walletRepo.CalculateLedgerBalance(ctx, nil, id)
	if err != nil {
		return nil, err
	}

	return &WalletAuditSummary{
		Wallet:        wallet,
		LedgerBalance: ledgerBalance,
		IsBalanced:    wallet.Balance == ledgerBalance,
	}, nil
}
