package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// txStore implements repository.TxStore bound to one pgx transaction.
type txStore struct {
	tx pgx.Tx
}

func (s *txStore) GetTransferForUpdate(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	const q = `SELECT ` + transferCols + ` FROM transfer WHERE id = $1 FOR UPDATE`
	t, err := scanTransfer(s.tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return t, err
}

// LockWalletPair locks both rows in a single statement. ORDER BY id fixes the
// lock-acquisition order regardless of transfer direction, preventing deadlock.
func (s *txStore) LockWalletPair(ctx context.Context, fromID, toID uuid.UUID) (domain.Wallet, domain.Wallet, error) {
	const q = `
		SELECT id, balance, is_active, created_at, updated_at
		FROM wallet
		WHERE id IN ($1, $2)
		ORDER BY id
		FOR UPDATE`

	rows, err := s.tx.Query(ctx, q, fromID, toID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, classify(err)
	}
	defer rows.Close()

	found := make(map[uuid.UUID]domain.Wallet, 2)
	for rows.Next() {
		w, err := scanWallet(rows)
		if err != nil {
			return domain.Wallet{}, domain.Wallet{}, err
		}
		found[w.ID] = w
	}
	if err := rows.Err(); err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}

	from, ok := found[fromID]
	if !ok {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	to, ok := found[toID]
	if !ok {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	return from, to, nil
}

func (s *txStore) ApplyBalanceDelta(ctx context.Context, walletID uuid.UUID, delta decimal.Decimal) error {
	const q = `UPDATE wallet SET balance = balance + $2 WHERE id = $1`
	tag, err := s.tx.Exec(ctx, q, walletID, delta)
	if err != nil {
		return classify(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (s *txStore) InsertLedgerEntry(ctx context.Context, e domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES ($1, $2, $3, $4)`
	_, err := s.tx.Exec(ctx, q, e.WalletID, e.TransferID, e.Type, e.Amount)
	return classify(err)
}

func (s *txStore) SetProcessed(ctx context.Context, transferID uuid.UUID) error {
	return s.guardedState(ctx, transferID,
		`UPDATE transfer SET state = 'PROCESSED', failure_reason = NULL
		 WHERE id = $1 AND state = 'PROCESSING'`)
}

func (s *txStore) SetFailed(ctx context.Context, transferID uuid.UUID, reason string) error {
	const q = `UPDATE transfer SET state = 'FAILED', failure_reason = $2
		WHERE id = $1 AND state = 'PROCESSING'`
	tag, err := s.tx.Exec(ctx, q, transferID, reason)
	if err != nil {
		return classify(err)
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrConcurrentModification
	}
	return nil
}

func (s *txStore) guardedState(ctx context.Context, transferID uuid.UUID, q string) error {
	tag, err := s.tx.Exec(ctx, q, transferID)
	if err != nil {
		return classify(err)
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrConcurrentModification
	}
	return nil
}
