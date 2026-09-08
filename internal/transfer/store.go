package transfer

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrInsufficientFunds = errors.New("insufficient funds")
var ErrWalletNotFound = errors.New("wallet not found")
var ErrIdempotencyKeyReuse = errors.New("idempotency key already used with different transfer")

const schema = `
CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    balance_cents INTEGER NOT NULL CHECK (balance_cents >= 0),
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS transfers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ledger_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    UNIQUE (transfer_id, entry_type)
);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id ON ledger_entries(transfer_id);
`

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Initialize() error {
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) CreateWallet(ctx context.Context, id string, balanceCents int64) error {
	if id == "" || balanceCents < 0 {
		return errors.New("invalid wallet")
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO wallets (id, balance_cents, created_at) VALUES (?, ?, ?)", id, balanceCents, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetWallet(ctx context.Context, id string) (Wallet, error) {
	var wallet Wallet
	err := s.db.QueryRowContext(ctx, "SELECT id, balance_cents FROM wallets WHERE id = ?", id).Scan(&wallet.ID, &wallet.BalanceCents)
	if errors.Is(err, sql.ErrNoRows) {
		return Wallet{}, ErrWalletNotFound
	}
	return wallet, err
}

func (s *Store) GetAllWallets(ctx context.Context) ([]Wallet, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, balance_cents FROM wallets ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	wallets := make([]Wallet, 0)
	for rows.Next() {
		var wallet Wallet
		if err := rows.Scan(&wallet.ID, &wallet.BalanceCents); err != nil {
			return nil, err
		}
		wallets = append(wallets, wallet)
	}
	return wallets, rows.Err()
}

func (s *Store) Transfer(ctx context.Context, request TransferRequest) (Transfer, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Transfer{}, false, err
	}
	defer tx.Rollback()

	var existing Transfer
	err = tx.QueryRowContext(ctx, `SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount_cents, status
		FROM transfers WHERE idempotency_key = ?`, request.IdempotencyKey).Scan(
		&existing.ID, &existing.IdempotencyKey, &existing.FromWalletID, &existing.ToWalletID, &existing.AmountCents, &existing.Status)
	if err == nil {
		if existing.FromWalletID != request.FromWalletID || existing.ToWalletID != request.ToWalletID || existing.AmountCents != request.AmountCents {
			return Transfer{}, false, ErrIdempotencyKeyReuse
		}
		return existing, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Transfer{}, false, err
	}

	transfer := Transfer{
		ID:             newID(),
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		AmountCents:    request.AmountCents,
		Status:         "PENDING",
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount_cents, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.AmountCents, transfer.Status, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Transfer{}, false, err
	}

	result, err := tx.ExecContext(ctx, "UPDATE wallets SET balance_cents = balance_cents - ? WHERE id = ? AND balance_cents >= ?", request.AmountCents, request.FromWalletID, request.AmountCents)
	if err != nil {
		return Transfer{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Transfer{}, false, err
	}
	if rows != 1 {
		if _, err = tx.ExecContext(ctx, "UPDATE transfers SET status = 'FAILED' WHERE id = ?", transfer.ID); err != nil {
			return Transfer{}, false, err
		}
		transfer.Status = "FAILED"
		if err = tx.Commit(); err != nil {
			return Transfer{}, false, err
		}
		return transfer, true, ErrInsufficientFunds
	}

	result, err = tx.ExecContext(ctx, "UPDATE wallets SET balance_cents = balance_cents + ? WHERE id = ?", request.AmountCents, request.ToWalletID)
	if err != nil {
		return Transfer{}, false, err
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return Transfer{}, false, err
	}
	if rows != 1 {
		return Transfer{}, false, ErrWalletNotFound
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_cents) VALUES (?, ?, 'DEBIT', ?), (?, ?, 'CREDIT', ?)`, transfer.ID, request.FromWalletID, request.AmountCents, transfer.ID, request.ToWalletID, request.AmountCents); err != nil {
		return Transfer{}, false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE transfers SET status = 'PROCESSED' WHERE id = ?", transfer.ID); err != nil {
		return Transfer{}, false, err
	}
	transfer.Status = "PROCESSED"
	if err = tx.Commit(); err != nil {
		return Transfer{}, false, err
	}
	return transfer, true, nil
}

func (s *Store) LedgerEntries(ctx context.Context, transferID string) ([]LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT transfer_id, wallet_id, entry_type, amount_cents FROM ledger_entries WHERE transfer_id = ? ORDER BY id", transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var entry LedgerEntry
		if err := rows.Scan(&entry.TransferID, &entry.WalletID, &entry.EntryType, &entry.AmountCents); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) GetAllLedgerEntries(ctx context.Context) ([]LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT transfer_id, wallet_id, entry_type, amount_cents FROM ledger_entries ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]LedgerEntry, 0)
	for rows.Next() {
		var entry LedgerEntry
		if err := rows.Scan(&entry.TransferID, &entry.WalletID, &entry.EntryType, &entry.AmountCents); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
