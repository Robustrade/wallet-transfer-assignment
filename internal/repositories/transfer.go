package repositories

import (
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/utils"
)

// TransferRepository handles atomic transactional logic for wallet-to-wallet transfers.
type TransferRepository struct {
	db *gorm.DB
}

func NewTransferRepository(db *gorm.DB) Repository {
	return &TransferRepository{db: db}
}

// lockWallets acquires exclusive locks on the source and destination wallets.
// To prevent deadlocks in concurrent transfers (e.g., A->B and B->A),
// wallet IDs are sorted lexicographically before acquisition.
func lockWallets(tx *gorm.DB, sourceID, destID string) (map[string]*models.Wallet, error) {
	walletIDs := []string{sourceID, destID}
	sort.Strings(walletIDs)

	wallets := make(map[string]*models.Wallet)
	for _, id := range walletIDs {
		var wallet models.Wallet
		// Use SELECT ... FOR UPDATE to lock the rows until transaction commit/rollback.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&wallet, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("wallet %s not found", id)
			}
			return nil, err
		}
		wallets[id] = &wallet
	}
	return wallets, nil
}

// validateProcessTransfer checks if the source wallet has sufficient balance.
func validateProcessTransfer(source *models.Wallet, amount int64) error {
	if source.Balance < amount {
		return errors.New("insufficient funds")
	}
	return nil
}

// handleProcessFailure marks a transfer as permanently failed with a reason.
func handleProcessFailure(tx *gorm.DB, transfer *models.Transfer, reason string) error {
	transfer.Status = models.TransferStatusFailed
	transfer.FailureReason = reason
	return tx.Save(transfer).Error
}

// updateBalances performs the atomic balance shift between the two wallets.
func updateBalances(tx *gorm.DB, source, dest *models.Wallet, amount int64) error {
	source.Balance -= amount
	dest.Balance += amount

	if err := tx.Save(source).Error; err != nil {
		return err
	}
	return tx.Save(dest).Error
}

// finalizeTransfer creates immutable ledger entries for audit trails.
// The ledger table has a BEFORE UPDATE/DELETE trigger to ensure immutability.
func finalizeTransfer(tx *gorm.DB, transfer *models.Transfer) error {
	entries := []models.Ledger{
		{
			WalletID:   transfer.SourceWalletID,
			TransferID: transfer.ID,
			Amount:     transfer.Amount,
			Type:       models.EntryTypeDebit,
		},
		{
			WalletID:   transfer.DestWalletID,
			TransferID: transfer.ID,
			Amount:     transfer.Amount,
			Type:       models.EntryTypeCredit,
		},
	}

	for _, entry := range entries {
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
	}

	transfer.Status = models.TransferStatusProcessed
	return tx.Save(transfer).Error
}

// ProcessTransfer executes the core 5-step transactional transfer process:
// 1. Create initial PENDING record (Tier 2 Idempotency Check).
// 2. Acquire ordered row-level locks on wallets (Deadlock Prevention).
// 3. Validate system state (Insufficient Funds Check).
// 4. Atomic Balance Update.
// 5. Generate Audit Ledgers & Mark PROCESSED.
func (r *TransferRepository) ProcessTransfer(transfer *models.Transfer) (*models.Transfer, error) {
	// Step 1: Record initial intent. This provides Tier 2 (DB) Idempotency.
	transfer.Status = models.TransferStatusPending
	if err := r.db.Create(transfer).Error; err != nil {
		// If the ID already exists, it means the request is a duplicate.
		if utils.IsUniqueConstraintViolation(err) {
			existing, getErr := r.GetTransferByIdempotencyKey(transfer.IdempotencyKey)
			if getErr == nil {
				return existing, nil
			}
		}
		return nil, err
	}

	var businessErr error
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Step 2: Acquire Locks.
		wallets, err := lockWallets(tx, transfer.SourceWalletID, transfer.DestWalletID)
		if err != nil {
			return err
		}

		sourceWallet := wallets[transfer.SourceWalletID]
		destWallet := wallets[transfer.DestWalletID]

		// Step 3: Validate Balance.
		if err := validateProcessTransfer(sourceWallet, transfer.Amount); err != nil {
			// Record the failure permanently so retries don't re-run the logic.
			_ = handleProcessFailure(tx, transfer, err.Error())
			businessErr = err
			return nil
		}

		// Step 4: Update Balances.
		if err := updateBalances(tx, sourceWallet, destWallet, transfer.Amount); err != nil {
			return err
		}

		// Step 5: Finalize & Ledger.
		return finalizeTransfer(tx, transfer)
	})

	// If a business error occurred, we committed the 'FAILED' status.
	if err == nil && businessErr != nil {
		return transfer, businessErr
	}

	return transfer, err
}

// GetTransferByIdempotencyKey retrieves a transfer by its unique idempotency token.
func (r *TransferRepository) GetTransferByIdempotencyKey(key string) (*models.Transfer, error) {
	var transfer models.Transfer
	if err := r.db.First(&transfer, "idempotency_key = ?", key).Error; err != nil {
		return nil, err
	}
	return &transfer, nil
}
