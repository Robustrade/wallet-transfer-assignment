package postgres

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

func mapRowError(err error, entity string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("%s: %w", entity, err)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanWallet(row scannable) (*domain.Wallet, error) {
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.UserID, &w.Balance, &w.Currency, &w.CreatedAt)
	if err != nil {
		return nil, mapRowError(err, "wallet")
	}
	return &w, nil
}

func scanTransfer(row scannable) (*domain.Transfer, error) {
	var t domain.Transfer
	var state string
	err := row.Scan(
		&t.ID,
		&t.SourceWalletID,
		&t.TargetWalletID,
		&t.Amount,
		&t.Currency,
		&state,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, mapRowError(err, "transfer")
	}
	t.State = domain.TransferState(state)
	return &t, nil
}
