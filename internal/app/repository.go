package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Transact(ctx context.Context, fn func(tx *sql.Tx) error) error {
	const maxAttempts = 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			lastErr = err
			if isBusyError(err) {
				continue
			}
			return err
		}

		innerErr := func() error {
			defer func() {
				if p := recover(); p != nil {
					_ = tx.Rollback()
					panic(p)
				}
			}()

			if err := fn(tx); err != nil {
				_ = tx.Rollback()
				return err
			}
			return tx.Commit()
		}()

		if innerErr != nil {
			lastErr = innerErr
			if isBusyError(innerErr) {
				continue
			}
			return innerErr
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return sql.ErrTxDone
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "deadlocked") || strings.Contains(msg, "locked")
}

func (r *Repository) GetWallet(ctx context.Context, tx *sql.Tx, walletID string) (*Wallet, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, balance, created_at FROM wallets WHERE id = ?`, walletID)
	wallet := &Wallet{}
	if err := row.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

func (r *Repository) CreateWallet(ctx context.Context, tx *sql.Tx, wallet *Wallet) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO wallets (id, balance) VALUES (?, ?)`, wallet.ID, wallet.Balance)
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) GetWalletBalance(ctx context.Context, walletID string) (int64, error) {
	row := r.db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, walletID)
	var balance int64
	if err := row.Scan(&balance); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrWalletNotFound
		}
		return 0, err
	}
	return balance, nil
}

func (r *Repository) GetTransferByIdempotencyKey(ctx context.Context, tx *sql.Tx, idempotencyKey string) (*Transfer, error) {
	query := `SELECT id, from_wallet_id, to_wallet_id, amount, state, idempotency_key, created_at, updated_at FROM transfers WHERE idempotency_key = ?`
	row := tx.QueryRowContext(ctx, query, idempotencyKey)
	transfer := &Transfer{}
	if err := row.Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State, &transfer.IdempotencyKey, &transfer.CreatedAt, &transfer.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}
	return transfer, nil
}

func (r *Repository) CreateTransfer(ctx context.Context, tx *sql.Tx, transfer *Transfer) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state, idempotency_key) VALUES (?, ?, ?, ?, ?, ?)`,
		transfer.ID,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.State,
		transfer.IdempotencyKey,
	)
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) UpdateTransferState(ctx context.Context, tx *sql.Tx, transferID string, state TransferState) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE transfers SET state = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		state,
		transferID,
	)
	return err
}

func (r *Repository) AdjustWalletBalance(ctx context.Context, tx *sql.Tx, walletID string, delta int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance + ? WHERE id = ?`, delta, walletID)
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []LedgerEntry) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		if _, err := stmt.ExecContext(ctx, entry.WalletID, entry.TransferID, entry.Type, entry.Amount); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) GetWalletByID(ctx context.Context, walletID string) (*Wallet, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, balance, created_at FROM wallets WHERE id = ?`, walletID)
	wallet := &Wallet{}
	if err := row.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

func (r *Repository) GetTransferByID(ctx context.Context, transferID string) (*Transfer, error) {
	query := `SELECT id, from_wallet_id, to_wallet_id, amount, state, idempotency_key, created_at, updated_at FROM transfers WHERE id = ?`
	row := r.db.QueryRowContext(ctx, query, transferID)
	transfer := &Transfer{}
	if err := row.Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.State, &transfer.IdempotencyKey, &transfer.CreatedAt, &transfer.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}
	return transfer, nil
}

func (r *Repository) GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]LedgerEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, wallet_id, transfer_id, type, amount, created_at FROM ledger_entries WHERE transfer_id = ? ORDER BY id`, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var entry LedgerEntry
		if err := rows.Scan(&entry.ID, &entry.WalletID, &entry.TransferID, &entry.Type, &entry.Amount, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *Repository) GetBalanceForUpdate(ctx context.Context, tx *sql.Tx, walletID string) (*Wallet, error) {
	return r.GetWallet(ctx, tx, walletID)
}

func (r *Repository) FindWallets(ctx context.Context, tx *sql.Tx, ids ...string) (map[string]*Wallet, error) {
	wallets := make(map[string]*Wallet, len(ids))
	for _, id := range ids {
		if _, ok := wallets[id]; ok {
			continue
		}
		wallet, err := r.GetWallet(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		wallets[id] = wallet
	}
	return wallets, nil
}

func (r *Repository) CreateWalletIfNotExists(ctx context.Context, tx *sql.Tx, wallet *Wallet) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO wallets (id, balance) VALUES (?, ?)`, wallet.ID, wallet.Balance)
	return err
}

func (r *Repository) QueryTransferByIdempotencyKey(ctx context.Context, key string) (*Transfer, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return r.GetTransferByIdempotencyKey(ctx, tx, key)
}

func (r *Repository) QueryWallet(ctx context.Context, walletID string) (*Wallet, error) {
	return r.GetWalletByID(ctx, walletID)
}

func (r *Repository) QueryTransferByID(ctx context.Context, transferID string) (*Transfer, error) {
	return r.GetTransferByID(ctx, transferID)
}

func (r *Repository) QueryLedgerEntries(ctx context.Context, transferID string) ([]LedgerEntry, error) {
	return r.GetLedgerEntriesByTransferID(ctx, transferID)
}

func (r *Repository) CreateWalletInTx(ctx context.Context, tx *sql.Tx, wallet *Wallet) error {
	return r.CreateWallet(ctx, tx, wallet)
}

func (r *Repository) WalletExists(ctx context.Context, tx *sql.Tx, walletID string) (bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT 1 FROM wallets WHERE id = ?`, walletID)
	var one int
	if err := row.Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *Repository) MakeTransferStateFailed(ctx context.Context, tx *sql.Tx, transferID string) error {
	return r.UpdateTransferState(ctx, tx, transferID, StateFailed)
}

func (r *Repository) EnsureWalletsExist(ctx context.Context, tx *sql.Tx, walletIDs ...string) error {
	for _, id := range walletIDs {
		if _, err := r.GetWallet(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) NotifyIfBalanceNegative(ctx context.Context, tx *sql.Tx, walletID string) error {
	wallet, err := r.GetWallet(ctx, tx, walletID)
	if err != nil {
		return err
	}
	if wallet.Balance < 0 {
		return fmt.Errorf("wallet %s negative balance", walletID)
	}
	return nil
}
