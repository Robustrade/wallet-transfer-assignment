package repository

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
)

func (t *TransferTx) LockWallets(ctx context.Context, fromWalletID, toWalletID domain.WalletID) (*domain.Wallet, *domain.Wallet, error) {
	firstWalletID := fromWalletID
	secondWalletID := toWalletID

	if firstWalletID > secondWalletID {
		firstWalletID, secondWalletID = secondWalletID, firstWalletID
	}

	rows, err := t.tx.Query(ctx, `
		SELECT wallet_id, balance
		FROM wallets
		WHERE wallet_id = $1 OR wallet_id = $2
		ORDER BY wallet_id
		FOR UPDATE
	`, firstWalletID, secondWalletID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	wallets := make(map[domain.WalletID]*domain.Wallet, 2)
	for rows.Next() {
		var wallet domain.Wallet
		if err := rows.Scan(&wallet.ID, &wallet.Balance); err != nil {
			return nil, nil, err
		}

		wallets[wallet.ID] = &wallet
	}

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	if len(wallets) != 2 {
		return nil, nil, domain.ErrWalletNotFound
	}

	return wallets[fromWalletID], wallets[toWalletID], nil
}

func (t *TransferTx) UpdateWalletBalance(ctx context.Context, walletID domain.WalletID, balance int64) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE wallets
		SET balance = $1, updated_at = now()
		WHERE wallet_id = $2
	`, balance, walletID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}

	return nil
}
