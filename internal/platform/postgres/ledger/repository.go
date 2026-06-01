package ledger

import (
	"context"
	"fmt"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/ledger"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/tx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// getTx extracts the injected transaction from the context if it exists.
func getTx(ctx context.Context, defaultDB *gorm.DB) *gorm.DB {
	if txDB, ok := tx.ExtractTx(ctx).(*gorm.DB); ok {
		return txDB
	}
	return defaultDB
}

// LedgerRepository implements the ledger.LedgerRepository interface using GORM.
type LedgerRepository struct {
	db *gorm.DB
}

// NewLedger creates a new ledger repository.
func NewLedger(db *gorm.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// InsertDoubleEntry inserts a debit and a credit entry atomically.
// The caller passes a context that may contain an ongoing transaction.
func (r *LedgerRepository) InsertDoubleEntry(ctx context.Context, debit, credit *ledger.Entry) error {
	// Convert domain entries to GORM models.
	debitM := EntryModel{
		TransferID: debit.TransferID,
		WalletID:   debit.WalletID,
		Type:       string(debit.Type),
		Amount:     debit.Amount,
		CreatedAt:  debit.CreatedAt,
	}
	creditM := EntryModel{
		TransferID: credit.TransferID,
		WalletID:   credit.WalletID,
		Type:       string(credit.Type),
		Amount:     credit.Amount,
		CreatedAt:  credit.CreatedAt,
	}
	// Batch insert both rows.
	db := getTx(ctx, r.db)
	return db.WithContext(ctx).Create([]EntryModel{debitM, creditM}).Error
}

// GetByWalletID returns all ledger entries for a wallet, newest first.
func (r *LedgerRepository) GetByWalletID(ctx context.Context, walletID string) ([]*ledger.Entry, error) {
	var rows []EntryModel
	if err := r.db.WithContext(ctx).
		Where("wallet_id = ?", walletID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*ledger.Entry, len(rows))
	for i, m := range rows {
		out[i] = m.ToDomain()
	}
	return out, nil
}

// ------------------------------------------------------------------
// BalanceRepository implementation (same package)
// ------------------------------------------------------------------

type BalanceRepository struct {
	db *gorm.DB
}

// NewBalance creates a new repository for wallet balances.
func NewBalance(db *gorm.DB) *BalanceRepository {
	return &BalanceRepository{db: db}
}

// GetBalance returns the current balance for a wallet.
func (r *BalanceRepository) GetBalance(ctx context.Context, walletID string) (*ledger.Balance, error) {
	var m BalanceModel
	err := r.db.WithContext(ctx).First(&m, "wallet_id = ?", walletID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ledger.ErrWalletNotFound
		}
		return nil, err
	}
	return &ledger.Balance{WalletID: m.WalletID, Amount: m.Amount, UpdatedAt: m.UpdatedAt}, nil
}

// DebitAndCredit debit from the source wallet and credit the destination wallet
// inside a single transaction, using row‑level locks for concurrency safety.
func (r *BalanceRepository) DebitAndCredit(ctx context.Context, fromWalletID, toWalletID string, amount int64) error {
	var fromBal BalanceModel
	db := getTx(ctx, r.db)

	// 1. Determine consistent locking order to prevent deadlocks
	firstLockID, secondLockID := fromWalletID, toWalletID
	if fromWalletID > toWalletID {
		firstLockID, secondLockID = toWalletID, fromWalletID
	}

	// 2. Acquire the first lock
	var firstBal, secondBal BalanceModel
	if err := db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&firstBal, "wallet_id = ?", firstLockID).Error; err != nil {
		return fmt.Errorf("balance not found for wallet %s: %w", firstLockID, err)
	}

	// 3. Acquire the second lock
	if err := db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&secondBal, "wallet_id = ?", secondLockID).Error; err != nil {
		return fmt.Errorf("balance not found for wallet %s: %w", secondLockID, err)
	}

	// 4. Map the locks back to from/to balances for the business logic
	if fromWalletID == firstLockID {
		fromBal = firstBal
	} else {
		fromBal = secondBal
	}
	// Ensure sufficient funds.
	if fromBal.Amount < amount {
		return ledger.ErrInsufficientBalance
	}
	// Apply relative updates atomically.
	// Debit source wallet.
	if err := db.WithContext(ctx).
		Model(&BalanceModel{}).
		Where("wallet_id = ?", fromWalletID).
		Updates(map[string]any{
			"amount": gorm.Expr("amount - ?", amount),
		}).Error; err != nil {
		return err
	}
	// Credit destination wallet.
	if err := db.WithContext(ctx).
		Model(&BalanceModel{}).
		Where("wallet_id = ?", toWalletID).
		Updates(map[string]any{
			"amount": gorm.Expr("amount + ?", amount),
		}).Error; err != nil {
		return err
	}
	return nil
}

// ------------------------------------------------------------------
// Helper conversion for ledger entries (model → domain)
// ------------------------------------------------------------------

func (e *EntryModel) ToDomain() *ledger.Entry {
	return &ledger.Entry{
		TransferID: e.TransferID,
		WalletID:   e.WalletID,
		Type:       ledger.EntryType(e.Type),
		Amount:     e.Amount,
		CreatedAt:  e.CreatedAt,
	}
}
