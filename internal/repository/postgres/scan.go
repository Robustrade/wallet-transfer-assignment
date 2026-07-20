package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWallet(row rowScanner) (domain.Wallet, error) {
	var wallet domain.Wallet
	err := row.Scan(
		&wallet.ID,
		&wallet.OwnerID,
		&wallet.Currency,
		&wallet.OpeningBalance,
		&wallet.Balance,
		&wallet.Status,
		&wallet.Version,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return wallet, err
}

func scanTransfer(row rowScanner) (domain.Transfer, error) {
	var transfer domain.Transfer
	err := row.Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Currency,
		&transfer.Status,
		&transfer.FailureReason,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return transfer, err
}

func scanIdempotencyRecord(row rowScanner) (domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	err := row.Scan(
		&record.Key,
		&record.RequestFingerprint,
		&record.TransferID,
		&record.Status,
		&record.ResponseSnapshot,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, domain.ErrRequestInProgress
	}
	return record, err
}

func scanLedgerEntry(row rowScanner) (domain.LedgerEntry, error) {
	var entry domain.LedgerEntry
	err := row.Scan(
		&entry.ID,
		&entry.TransferID,
		&entry.WalletID,
		&entry.Type,
		&entry.Amount,
		&entry.BalanceAfter,
		&entry.CreatedAt,
	)
	return entry, err
}
