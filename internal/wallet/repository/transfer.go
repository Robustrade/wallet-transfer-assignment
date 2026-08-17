package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/domain"
)

type WalletRepository interface {
	CreateTransfer(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error)
}

func NewWalletRepo(pool *pgxpool.Pool) WalletRepository {
	return &walletRepository{
		pool: pool,
	}
}

type walletRepository struct {
	pool *pgxpool.Pool
}

func (t *walletRepository) CreateTransfer(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error) {
	// check of existing idempotency key
	query := `SELECT id, from_wallet_id, to_wallet_id, amount, status, idempotency_key
							FROM wallet.transfers
							WHERE idempotency_key = $1;`
	tr := domain.Transfer{}
	err := t.pool.QueryRow(ctx, query, transfer.IdempotencyKey).Scan(&tr.ID, &tr.FromWallet, &tr.ToWallet, &tr.Amount, &tr.Status, &tr.IdempotencyKey)
	if err == nil {
		return tr, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("Failed to check idempotency key: %w", err)
	}

	// start transfer using transaction
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("error while initializing transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// get wallet data for update
	query = `SELECT id, balance 
				FROM wallet.wallets 
				WHERE id = ANY($1)
				FOR UPDATE;`

	rows, err := tx.Query(ctx, query, []string{transfer.FromWallet, transfer.ToWallet})
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Failed to get wallets data: %w", err)
	}

	wallets, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Wallet])
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("unable to collect rows in tx: %w", err)
	}
	if len(wallets) < 2 {
		return domain.Transfer{}, fmt.Errorf("One of the wallets is invalid")
	}

	var debitWallet, creditWallet domain.Wallet
	for _, w := range wallets {
		switch w.ID {
		case transfer.FromWallet:
			debitWallet = w
		case transfer.ToWallet:
			creditWallet = w
		}
	}

	// create transfer entry
	transferEntryQuery := `INSERT INTO wallet.transfers (id, from_wallet_id, to_wallet_id, amount, status, idempotency_key)
							VALUES ($1, $2, $3, $4, $5, $6)
							RETURNING id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at;`

	transferId := uuid.NewString()
	_, err = tx.Exec(ctx, transferEntryQuery, transferId, transfer.FromWallet, transfer.ToWallet, transfer.Amount, domain.StatusPending, transfer.IdempotencyKey)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Couldn't create transfer entry")
	}

	// update debit and credit wallets
	if debitWallet.Balance < tr.Amount {
		return domain.Transfer{}, fmt.Errorf("Insufficient fund")
	}
	updateWalletQuery := `UPDATE wallet.wallets
							SET balance = $1, updated_at = CURRENT_TIMESTAMP
							WHERE id = $2;`
	_, err = tx.Exec(ctx, updateWalletQuery, debitWallet.Balance-transfer.Amount, debitWallet.ID)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Error while updating debit wallet balance")
	}
	_, err = tx.Exec(ctx, updateWalletQuery, creditWallet.Balance+transfer.Amount, creditWallet.ID)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Error while updating credit wallet balance")
	}

	// create ledger entries
	ledgerEntryQuery := `INSERT INTO wallet.ledger_entries (id, transfer_id, wallet_id, entry_type, amount)
							VALUES ($1, $2, $3, $4, $5);`
	_, err = tx.Exec(ctx, ledgerEntryQuery, uuid.NewString(), transferId, debitWallet.ID, "DEBIT", transfer.Amount)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Error creating ledger for debit wallet")
	}
	_, err = tx.Exec(ctx, ledgerEntryQuery, uuid.NewString(), transferId, creditWallet.ID, "CREDIT", transfer.Amount)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Error creating ledger for credit wallet")
	}

	// update transfer status
	updateStatusQuery := `UPDATE wallet.transfers
							SET status = $1, updated_at = CURRENT_TIMESTAMP
							WHERE id = $2;`
	_, err = tx.Exec(ctx, updateStatusQuery, domain.StatusProcessed, transferId)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("Error while updating transfer status")
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Transfer{}, fmt.Errorf("failed to commit transaction")
	}

	return domain.Transfer{
		ID:             transferId,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWallet:     transfer.FromWallet,
		ToWallet:       transfer.ToWallet,
		Amount:         transfer.Amount,
		Status:         domain.StatusProcessed,
	}, nil
}
