package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	repoiface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/repo/iface"
)

// txManager is a closure-based transaction wrapper. It runs the supplied
// function inside a single GORM transaction at READ COMMITTED isolation and
// stores the transactional *gorm.DB in the function's context so repository
// methods can transparently participate.
//
// READ COMMITTED is sufficient because the transfer flow uses
// `SELECT ... FOR NO KEY UPDATE` to lock the participating wallet rows;
// once the rows are locked, a stable snapshot of *unlocked* rows (which
// REPEATABLE READ would buy) is irrelevant.
type txManager struct {
	db     *gorm.DB
	logger *slog.Logger
}

// NewTxManager constructs a transaction manager bound to the supplied GORM
// connection.
func NewTxManager(db *gorm.DB, logger *slog.Logger) repoiface.TxManager {
	return &txManager{db: db, logger: logger}
}

// Run executes fn inside a transaction. On a nil return the transaction is
// committed; otherwise it is rolled back. GORM's Transaction also rolls back
// on panic, so we get panic-safety for free.
func (m *txManager) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	err := m.db.WithContext(ctx).Transaction(
		func(tx *gorm.DB) error {
			return fn(withTx(ctx, tx))
		},
		&sql.TxOptions{Isolation: sql.LevelReadCommitted},
	)
	if err != nil {
		return fmt.Errorf("repository: tx failed: %w", err)
	}
	return nil
}
