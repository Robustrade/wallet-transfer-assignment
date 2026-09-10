package service

import (
	"context"
	"log/slog"
	"sort"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/jackc/pgx/v5"
)

// TransferService handles the business logic for wallet transfers.
type TransferService struct {
	txManager    repository.TxManager
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
	logger       *slog.Logger
}

// NewTransferService creates a new TransferService.
func NewTransferService(
	txManager repository.TxManager,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
	logger *slog.Logger,
) *TransferService {
	return &TransferService{
		txManager:    txManager,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
		logger:       logger,
	}
}

// TransferResult encapsulates the result of a transfer operation.
type TransferResult struct {
	Transfer      *domain.Transfer
	LedgerEntries []*domain.LedgerEntry
	IsDuplicate   bool
}

// CreateTransfer executes a wallet-to-wallet transfer atomically.
//
// The flow:
// 1. Validate inputs and create domain transfer object
// 2. Begin transaction
// 3. Insert transfer (detect idempotency duplicate via unique constraint)
// 4. If duplicate, return existing result
// 5. Lock wallets in deterministic order (by ID) to prevent deadlocks
// 6. Validate sufficient funds
// 7. Debit source, credit destination
// 8. Create ledger entries
// 9. Mark transfer PROCESSED
// 10. Commit
func (s *TransferService) CreateTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*TransferResult, error) {
	// Domain-level validation
	transfer, err := domain.NewTransfer(idempotencyKey, fromWalletID, toWalletID, amount)
	if err != nil {
		return nil, err
	}

	var result *TransferResult
	var businessErr error

	err = s.txManager.WithTx(ctx, func(tx pgx.Tx) error {
		// Step 1: Lock wallets in deterministic order to prevent deadlocks and FK lock escalation.
		// Always lock the wallet with the smaller ID first.
		walletIDs := []string{fromWalletID, toWalletID}
		sort.Strings(walletIDs)

		wallets := make(map[string]*domain.Wallet, 2)
		for _, wid := range walletIDs {
			w, lockErr := s.walletRepo.GetByIDForUpdate(ctx, tx, wid)
			if lockErr != nil {
				return lockErr
			}
			wallets[wid] = w
		}

		fromWallet := wallets[fromWalletID]
		toWallet := wallets[toWalletID]

		// Step 2: Attempt to create the transfer record.
		// If the idempotency key already exists, we get the existing transfer back.
		created, isDuplicate, createErr := s.transferRepo.Create(ctx, tx, transfer)
		if createErr != nil {
			return createErr
		}

		// Step 3: Handle idempotent replay.
		if isDuplicate {
			s.logger.InfoContext(ctx, "idempotent replay detected",
				slog.String("idempotency_key", idempotencyKey),
				slog.String("transfer_id", created.ID),
			)

			entries, entriesErr := s.ledgerRepo.GetByTransferID(ctx, tx, created.ID)
			if entriesErr != nil {
				return entriesErr
			}

			result = &TransferResult{
				Transfer:      created,
				LedgerEntries: entries,
				IsDuplicate:   true,
			}
			return nil
		}

		transfer = created

		s.logger.InfoContext(ctx, "processing transfer",
			slog.String("transfer_id", transfer.ID),
			slog.String("from_wallet", fromWalletID),
			slog.String("to_wallet", toWalletID),
			slog.Int64("amount", amount),
		)

		// Step 4: Check sufficient funds.
		if !fromWallet.HasSufficientFunds(amount) {
			failErr := s.failTransfer(ctx, tx, transfer.ID, "insufficient funds")
			if failErr != nil {
				return failErr
			}
			transfer.Status = domain.TransferStatusFailed
			transfer.FailureReason = "insufficient funds"
			result = &TransferResult{
				Transfer:      transfer,
				LedgerEntries: []*domain.LedgerEntry{},
				IsDuplicate:   false,
			}
			businessErr = domain.ErrInsufficientFunds
			return nil
		}

		// Step 5: Perform the balance updates.
		fromWallet.Debit(amount)
		toWallet.Credit(amount)

		if updateErr := s.walletRepo.UpdateBalance(ctx, tx, fromWalletID, fromWallet.Balance); updateErr != nil {
			return updateErr
		}
		if updateErr := s.walletRepo.UpdateBalance(ctx, tx, toWalletID, toWallet.Balance); updateErr != nil {
			return updateErr
		}

		// Step 6: Create double-entry ledger entries.
		debitEntry := domain.NewDebitEntry(transfer.ID, fromWalletID, amount)
		creditEntry := domain.NewCreditEntry(transfer.ID, toWalletID, amount)

		if ledgerErr := s.ledgerRepo.CreateEntries(ctx, tx, []*domain.LedgerEntry{debitEntry, creditEntry}); ledgerErr != nil {
			return ledgerErr
		}

		// Step 7: Mark transfer as PROCESSED.
		if statusErr := s.transferRepo.UpdateStatus(ctx, tx, transfer.ID, domain.TransferStatusProcessed, ""); statusErr != nil {
			return statusErr
		}

		transfer.Status = domain.TransferStatusProcessed
		result = &TransferResult{
			Transfer:      transfer,
			LedgerEntries: []*domain.LedgerEntry{debitEntry, creditEntry},
			IsDuplicate:   false,
		}

		s.logger.InfoContext(ctx, "transfer processed successfully",
			slog.String("transfer_id", transfer.ID),
		)

		return nil
	})

	if err != nil {
		return nil, err
	}

	if businessErr != nil {
		return result, businessErr
	}

	return result, nil
}

// GetTransfer retrieves a transfer by its ID.
func (s *TransferService) GetTransfer(ctx context.Context, transferID string) (*TransferResult, error) {
	var result *TransferResult

	err := s.txManager.WithTx(ctx, func(tx pgx.Tx) error {
		transfer, err := s.transferRepo.GetByID(ctx, tx, transferID)
		if err != nil {
			return err
		}

		entries, err := s.ledgerRepo.GetByTransferID(ctx, tx, transferID)
		if err != nil {
			return err
		}

		result = &TransferResult{
			Transfer:      transfer,
			LedgerEntries: entries,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// failTransfer marks a transfer as FAILED with the given reason.
func (s *TransferService) failTransfer(ctx context.Context, tx pgx.Tx, transferID, reason string) error {
	s.logger.WarnContext(ctx, "transfer failed",
		slog.String("transfer_id", transferID),
		slog.String("reason", reason),
	)
	return s.transferRepo.UpdateStatus(ctx, tx, transferID, domain.TransferStatusFailed, reason)
}
