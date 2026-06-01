package wallet

import (
    "context"
    "fmt"
    "github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
    "gorm.io/gorm"
)

// Repository implements the wallet.Repository interface using GORM.
type Repository struct {
    db *gorm.DB
}

// New creates a new wallet repository.
func New(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

func (r *Repository) GetByID(ctx context.Context, id string) (*wallet.Wallet, error) {
    var m Model
    if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
        return nil, err
    }
    return m.ToDomain(), nil
}

func (r *Repository) ValidateWallets(ctx context.Context, ids []string) error {
    if len(ids) == 0 {
        return nil
    }
    var count int64
    if err := r.db.WithContext(ctx).
        Model(&Model{}).
        Where("id IN ? AND status = ?", ids, wallet.StatusActive).
        Count(&count).Error; err != nil {
        return err
    }
    if count != int64(len(ids)) {
        return fmt.Errorf("wallet validation failed: missing or inactive wallets")
    }
    return nil
}

func (r *Repository) Create(ctx context.Context, w *wallet.Wallet) error {
    m := FromDomain(w)
    return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 1. Create the wallet
        if err := tx.Create(m).Error; err != nil {
            return err
        }

        // 2. Pragmatic Persistence: Directly initialize the zero balance
        // This avoids cross-domain coupling in Go while ensuring database-level atomicity.
        const insertBalanceSQL = "INSERT INTO wallet_balances (wallet_id, amount, updated_at) VALUES (?, 0, CURRENT_TIMESTAMP)"
        if err := tx.Exec(insertBalanceSQL, w.ID).Error; err != nil {
            return fmt.Errorf("failed to initialize wallet balance: %w", err)
        }

        return nil
    })
}

func (r *Repository) List(ctx context.Context, ownerID string) ([]*wallet.Wallet, error) {
    var models []Model
    if err := r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Find(&models).Error; err != nil {
        return nil, err
    }
    out := make([]*wallet.Wallet, len(models))
    for i, m := range models {
        out[i] = m.ToDomain()
    }
    return out, nil
}
