package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/amantiwari/wallet-transfer/internal/app/port"
	"github.com/amantiwari/wallet-transfer/internal/domain"
)

type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txRepos struct {
	wallets     walletRepo
	transfers   transferRepo
	ledger      ledgerRepo
	idempotency idempotencyRepo
}

func newTxRepos(q querier) txRepos {
	return txRepos{
		wallets:     walletRepo{q: q},
		transfers:   transferRepo{q: q},
		ledger:      ledgerRepo{q: q},
		idempotency: idempotencyRepo{q: q},
	}
}

func (t txRepos) Wallets() port.WalletRepository          { return t.wallets }
func (t txRepos) Transfers() port.TransferRepository      { return t.transfers }
func (t txRepos) Ledger() port.LedgerRepository           { return t.ledger }
func (t txRepos) Idempotency() port.IdempotencyRepository { return t.idempotency }

type walletRepo struct{ q querier }

func (r walletRepo) Create(ctx context.Context, wallet domain.Wallet) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO wallets (id, initial_balance, balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, wallet.ID, wallet.InitialBalance, wallet.Balance, wallet.CreatedAt, wallet.UpdatedAt)
	if err != nil {
		return mapError(err, "wallet", wallet.ID)
	}
	return nil
}

func (r walletRepo) Lock(ctx context.Context, id string) (domain.Wallet, error) {
	row := r.q.QueryRow(ctx, `
		SELECT id, initial_balance, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, id)
	wallet, err := scanWallet(row)
	if err != nil {
		return domain.Wallet{}, mapError(err, "wallet", id)
	}
	return wallet, nil
}

func (r walletRepo) Debit(ctx context.Context, id string, amount int64, at time.Time) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE wallets
		SET balance = balance - $2, updated_at = $3
		WHERE id = $1 AND balance >= $2
	`, id, amount, at.UTC())
	if err != nil {
		return mapError(err, "wallet", id)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInsufficientFunds
	}
	return nil
}

func (r walletRepo) Credit(ctx context.Context, id string, amount int64, at time.Time) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $2, updated_at = $3
		WHERE id = $1
	`, id, amount, at.UTC())
	if err != nil {
		return mapError(err, "wallet", id)
	}
	if tag.RowsAffected() != 1 {
		return &domain.NotFoundError{Entity: "wallet", ID: id}
	}
	return nil
}

type walletReader struct{ q querier }

func (r walletReader) GetByID(ctx context.Context, id string) (domain.Wallet, error) {
	row := r.q.QueryRow(ctx, `
		SELECT id, initial_balance, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, id)
	wallet, err := scanWallet(row)
	if err != nil {
		return domain.Wallet{}, mapError(err, "wallet", id)
	}
	return wallet, nil
}

func scanWallet(row pgx.Row) (domain.Wallet, error) {
	var wallet domain.Wallet
	if err := row.Scan(&wallet.ID, &wallet.InitialBalance, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt); err != nil {
		return domain.Wallet{}, err
	}
	wallet.CreatedAt = wallet.CreatedAt.UTC()
	wallet.UpdatedAt = wallet.UpdatedAt.UTC()
	return wallet, nil
}

type transferRepo struct{ q querier }

func (r transferRepo) Insert(ctx context.Context, transfer domain.Transfer) error {
	id, err := parseUUID(transfer.ID, "transfer")
	if err != nil {
		return err
	}
	_, err = r.q.Exec(ctx, `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount,
		string(transfer.Status), nullString(transfer.FailureReason), transfer.CreatedAt, transfer.UpdatedAt)
	if err != nil {
		return mapError(err, "transfer", transfer.ID)
	}
	return nil
}

func (r transferRepo) GetByID(ctx context.Context, id string) (domain.Transfer, error) {
	parsed, err := parseUUID(id, "transfer")
	if err != nil {
		return domain.Transfer{}, err
	}
	row := r.q.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`, parsed)
	transfer, err := scanTransfer(row)
	if err != nil {
		return domain.Transfer{}, mapError(err, "transfer", id)
	}
	return transfer, nil
}

func (r transferRepo) Transition(ctx context.Context, updated domain.Transfer, from domain.Status) error {
	id, err := parseUUID(updated.ID, "transfer")
	if err != nil {
		return err
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE transfers
		SET status = $2, failure_reason = $3, updated_at = $4
		WHERE id = $1 AND status = $5
	`, id, string(updated.Status), nullString(updated.FailureReason), updated.UpdatedAt.UTC(), string(from))
	if err != nil {
		return mapError(err, "transfer", updated.ID)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidTransition
	}
	return nil
}

type transferReader struct{ q querier }

func (r transferReader) GetByID(ctx context.Context, id string) (domain.Transfer, error) {
	return transferRepo(r).GetByID(ctx, id)
}

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var (
		transfer domain.Transfer
		id       pgtype.UUID
		status   string
		reason   *string
	)
	if err := row.Scan(
		&id,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&status,
		&reason,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	); err != nil {
		return domain.Transfer{}, err
	}
	parsed, err := uuidString(id)
	if err != nil {
		return domain.Transfer{}, err
	}
	transfer.ID = parsed
	transfer.Status = domain.Status(status)
	if reason != nil {
		transfer.FailureReason = *reason
	}
	transfer.CreatedAt = transfer.CreatedAt.UTC()
	transfer.UpdatedAt = transfer.UpdatedAt.UTC()
	return transfer, nil
}

type ledgerRepo struct{ q querier }

func (r ledgerRepo) InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	debitID, err := parseUUID(debit.ID, "ledger entry")
	if err != nil {
		return err
	}
	creditID, err := parseUUID(credit.ID, "ledger entry")
	if err != nil {
		return err
	}
	transferID, err := parseUUID(debit.TransferID, "transfer")
	if err != nil {
		return err
	}
	creditTransferID, err := parseUUID(credit.TransferID, "transfer")
	if err != nil {
		return err
	}
	_, err = r.q.Exec(ctx, `
		INSERT INTO ledger_entries (id, wallet_id, transfer_id, entry_type, amount, created_at)
		VALUES
			($1, $2, $3, $4, $5, $6),
			($7, $8, $9, $10, $11, $12)
	`, debitID, debit.WalletID, transferID, string(debit.Type), debit.Amount, debit.CreatedAt,
		creditID, credit.WalletID, creditTransferID, string(credit.Type), credit.Amount, credit.CreatedAt)
	if err != nil {
		return mapError(err, "ledger entry", debit.TransferID)
	}
	return nil
}

type ledgerReader struct{ q querier }

func (r ledgerReader) ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	parsed, err := parseUUID(transferID, "transfer")
	if err != nil {
		return nil, err
	}
	rows, err := r.q.Query(ctx, `
		SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY CASE entry_type WHEN 'DEBIT' THEN 0 ELSE 1 END, id
	`, parsed)
	if err != nil {
		return nil, mapError(err, "ledger entry", transferID)
	}
	defer rows.Close()

	entries := make([]domain.LedgerEntry, 0, 2)
	for rows.Next() {
		var (
			entry     domain.LedgerEntry
			id        pgtype.UUID
			transfer  pgtype.UUID
			entryType string
		)
		if err := rows.Scan(&id, &entry.WalletID, &transfer, &entryType, &entry.Amount, &entry.CreatedAt); err != nil {
			return nil, mapError(err, "ledger entry", transferID)
		}
		entry.ID, err = uuidString(id)
		if err != nil {
			return nil, err
		}
		entry.TransferID, err = uuidString(transfer)
		if err != nil {
			return nil, err
		}
		entry.Type = domain.EntryType(entryType)
		entry.CreatedAt = entry.CreatedAt.UTC()
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err, "ledger entry", transferID)
	}
	return entries, nil
}

type idempotencyRepo struct{ q querier }

func (r idempotencyRepo) TryInsert(ctx context.Context, record port.IdempotencyRecord) (port.IdempotencyRecord, bool, error) {
	tag, err := r.q.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, record.Key, record.RequestHash, record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return port.IdempotencyRecord{}, false, mapError(err, "idempotency key", record.Key)
	}
	if tag.RowsAffected() == 1 {
		return record, true, nil
	}
	existing, err := r.get(ctx, record.Key)
	if err != nil {
		return port.IdempotencyRecord{}, false, err
	}
	return existing, false, nil
}

func (r idempotencyRepo) AttachTransfer(ctx context.Context, key, transferID string, at time.Time) error {
	parsed, err := parseUUID(transferID, "transfer")
	if err != nil {
		return err
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE idempotency_records
		SET transfer_id = $2, updated_at = $3
		WHERE idempotency_key = $1 AND transfer_id IS NULL
	`, key, parsed, at.UTC())
	if err != nil {
		return mapError(err, "idempotency key", key)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvariant
	}
	return nil
}

func (r idempotencyRepo) get(ctx context.Context, key string) (port.IdempotencyRecord, error) {
	var (
		record     port.IdempotencyRecord
		transferID pgtype.UUID
	)
	err := r.q.QueryRow(ctx, `
		SELECT idempotency_key, request_hash, transfer_id, created_at, updated_at
		FROM idempotency_records
		WHERE idempotency_key = $1
	`, key).Scan(&record.Key, &record.RequestHash, &transferID, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return port.IdempotencyRecord{}, mapError(err, "idempotency key", key)
	}
	record.TransferID, err = uuidString(transferID)
	if err != nil {
		return port.IdempotencyRecord{}, err
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}

func nullString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
