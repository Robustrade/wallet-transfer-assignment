package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// LedgerRepository persists double-entry ledger rows.
type LedgerRepository struct{ *DB }

// NewLedgerRepository wires the repository to a DB.
func NewLedgerRepository(db *DB) *LedgerRepository { return &LedgerRepository{DB: db} }

// InsertPair writes the debit and credit entries in a single statement. The
// unique index on (transfer_id, type) guarantees a transfer can never have more
// than one of each; a violation here means the pair was already written.
func (r *LedgerRepository) InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	_, err := r.q(ctx).Exec(ctx,
		`INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		 VALUES ($1, $2, $3, $4), ($5, $6, $7, $8)`,
		debit.WalletID, debit.TransferID, string(debit.Type), debit.Amount.Amount,
		credit.WalletID, credit.TransferID, string(credit.Type), credit.Amount.Amount)
	if err != nil {
		if isUniqueViolation(err, "uq_ledger_transfer_type") {
			return fmt.Errorf("postgres: ledger pair already exists for transfer %s: %w", debit.TransferID, err)
		}
		return fmt.Errorf("postgres: insert ledger pair: %w", err)
	}
	return nil
}

// SumByWallet returns the net ledger position for a wallet (credits minus
// debits). Used for reconciliation and tests, not the transfer hot path.
func (r *LedgerRepository) SumByWallet(ctx context.Context, walletID string) (domain.Money, error) {
	var (
		net      int64
		currency string
	)
	err := r.q(ctx).QueryRow(ctx,
		`SELECT
		     COALESCE(SUM(CASE WHEN le.type = 'CREDIT' THEN le.amount ELSE -le.amount END), 0),
		     (SELECT currency FROM wallets WHERE id = $1)
		   FROM ledger_entries le
		  WHERE le.wallet_id = $1`, walletID).
		Scan(&net, &currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Money{}, domain.ErrWalletNotFound
		}
		return domain.Money{}, fmt.Errorf("postgres: sum ledger by wallet: %w", err)
	}
	return domain.Money{Amount: net, Currency: currency}, nil
}
