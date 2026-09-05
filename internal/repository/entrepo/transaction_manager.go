package entrepo

import (
	"context"
	"fmt"

	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/internal/repository"
)

type TransactionManager struct {
	client *ent.Client
}

func NewTransactionManager(client *ent.Client) *TransactionManager {
	return &TransactionManager{
		client: client,
	}
}

func (m *TransactionManager) WithTx(
	ctx context.Context,
	fn func(repos repository.TxRepositories) error,
) error {
	tx, err := m.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	repos := NewTxRepositories(tx)

	if err := fn(repos); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf(
				"transaction failed: %v; rollback failed: %w",
				err,
				rollbackErr,
			)
		}

		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
