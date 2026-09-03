package repository

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

const walletColumns = "id, balance, created_at, updated_at"

func (c *queries) CreateWallet(ctx context.Context, w *domain.Wallet) error {
	_, err := c.exec(ctx,
		`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		w.ID, w.Balance, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		if c.db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: %s", domain.ErrWalletExists, w.ID)
		}
		return fmt.Errorf("insert wallet %s: %w", w.ID, err)
	}
	return nil
}

func (c *queries) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	w, err := scanWallet(c.queryRow(ctx, `SELECT `+walletColumns+` FROM wallets WHERE id = ?`, id))
	if err != nil {
		if noRows(err) {
			return nil, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
		}
		return nil, fmt.Errorf("select wallet %s: %w", id, err)
	}
	return w, nil
}

func (c *queries) AdjustWalletBalance(ctx context.Context, id string, delta int64, now time.Time) error {
	res, err := c.exec(ctx,
		`UPDATE wallets SET balance = balance + ?, updated_at = ? WHERE id = ?`,
		delta, now, id)
	if err != nil {
		return fmt.Errorf("adjust balance of wallet %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("adjust balance of wallet %s: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	}
	return nil
}

func (t *Tx) LockWallets(ctx context.Context, ids ...string) (map[string]*domain.Wallet, error) {
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)

	wallets := make(map[string]*domain.Wallet, len(ordered))
	for _, id := range ordered {
		if _, seen := wallets[id]; seen {
			continue
		}
		w, err := scanWallet(t.queryRow(ctx,
			`SELECT `+walletColumns+` FROM wallets WHERE id = ?`+t.db.LockClause(), id))
		if err != nil {
			if noRows(err) {
				return nil, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
			}
			return nil, fmt.Errorf("lock wallet %s: %w", id, err)
		}
		wallets[id] = w
	}
	return wallets, nil
}

func scanWallet(row interface{ Scan(...any) error }) (*domain.Wallet, error) {
	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}
