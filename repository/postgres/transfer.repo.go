package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/lib/pq"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/repository/postgres/sqlcgen"
)

const uniqueViolationCode = "23505"

type TransferRepository struct {
	writeDB      *sql.DB
	readQueries  *sqlcgen.Queries
	writeQueries *sqlcgen.Queries
}

func NewTransferRepository(readDB *sql.DB, writeDB *sql.DB) *TransferRepository {
	return &TransferRepository{
		writeDB:      writeDB,
		readQueries:  sqlcgen.New(readDB),
		writeQueries: sqlcgen.New(writeDB),
	}
}

func (r *TransferRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	logger.Debug().Str("idempotency_key", key).Msg("looking up transfer by idempotency key")
	return withRetry(ctx, func() (*domain.Transfer, error) {
		t, err := r.readQueries.GetTransferByIdempotencyKey(ctx, key)
		if errors.Is(err, sql.ErrNoRows) {
			logger.Debug().Str("idempotency_key", key).Msg("no transfer found for idempotency key")
			return nil, nil
		}
		if err != nil {
			logger.Warn().Err(err).Str("idempotency_key", key).Msg("failed to fetch transfer by idempotency key")
			return nil, err
		}
		return toDomainTransfer(t), nil
	})
}

func (r *TransferRepository) ListByWallet(ctx context.Context, walletID string, limit, offset int32) ([]*domain.Transfer, error) {
	logger.Debug().Str("wallet_id", walletID).Int32("limit", limit).Int32("offset", offset).Msg("listing transfers for wallet")
	return withRetry(ctx, func() ([]*domain.Transfer, error) {
		rows, err := r.readQueries.ListTransfersByWallet(ctx, sqlcgen.ListTransfersByWalletParams{
			FromWalletID: walletID,
			Limit:        limit,
			Offset:       offset,
		})
		if err != nil {
			logger.Warn().Err(err).Str("wallet_id", walletID).Msg("failed to list transfers for wallet")
			return nil, err
		}
		transfers := make([]*domain.Transfer, len(rows))
		for i, row := range rows {
			transfers[i] = toDomainTransfer(row)
		}
		logger.Info().Str("wallet_id", walletID).Int("count", len(transfers)).Msg("listed transfers for wallet")
		return transfers, nil
	})
}

func (r *TransferRepository) ListPendingTransfers(ctx context.Context) ([]*domain.Transfer, error) {
	logger.Debug().Msg("listing pending transfers")
	return withRetry(ctx, func() ([]*domain.Transfer, error) {
		rows, err := r.readQueries.ListPendingTransfers(ctx)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to list pending transfers")
			return nil, err
		}
		transfers := make([]*domain.Transfer, len(rows))
		for i, row := range rows {
			transfers[i] = toDomainTransfer(row)
		}
		logger.Info().Int("count", len(transfers)).Msg("listed pending transfers")
		return transfers, nil
	})
}

func (r *TransferRepository) ResolvePendingTransfer(ctx context.Context, transfer *domain.Transfer) (*domain.Transfer, error) {
	logger.Info().Str("transfer_id", transfer.ID).Msg("resolving pending transfer")
	return withRetry(ctx, func() (*domain.Transfer, error) {
		return r.finalizePendingTransfer(ctx, transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	})
}

func (r *TransferRepository) ExecuteTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount float64, timestamp int64) (*domain.Transfer, error) {
	logger.Info().
		Str("idempotency_key", idempotencyKey).
		Str("from_wallet_id", fromWalletID).
		Str("to_wallet_id", toWalletID).
		Float64("amount", amount).
		Msg("executing transfer")
	return withRetry(ctx, func() (*domain.Transfer, error) {
		return r.executeTransferOnce(ctx, idempotencyKey, fromWalletID, toWalletID, amount, timestamp)
	})
}

func (r *TransferRepository) executeTransferOnce(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount float64, timestamp int64) (*domain.Transfer, error) {
	pending, err := insertPendingTransfer(ctx, r.writeDB, r.writeQueries, idempotencyKey, fromWalletID, toWalletID, amount)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyKeyConflict) {
			logger.Warn().Str("idempotency_key", idempotencyKey).Msg("idempotency key conflict while inserting pending transfer")
		} else {
			logger.Warn().Err(err).Str("idempotency_key", idempotencyKey).Msg("failed to insert pending transfer")
		}
		return nil, err
	}
	logger.Debug().Str("transfer_id", pending.ID).Msg("inserted pending transfer")
	return r.finalizePendingTransfer(ctx, pending.ID, fromWalletID, toWalletID, amount)
}

func (r *TransferRepository) finalizePendingTransfer(ctx context.Context, transferID, fromWalletID, toWalletID string, amount float64) (*domain.Transfer, error) {
	logger.Debug().Str("transfer_id", transferID).Msg("finalizing pending transfer")
	tx, err := r.writeDB.BeginTx(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to begin transaction for finalizing transfer")
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			logger.Error().Err(err).Msg("error rolling back transaction")
		}
	}()

	qtx := r.writeQueries.WithTx(tx)

	if err := lockWalletPair(ctx, qtx, fromWalletID, toWalletID); err != nil {
		logger.Warn().Err(err).Str("from_wallet_id", fromWalletID).Str("to_wallet_id", toWalletID).Msg("failed to lock wallet pair")
		return nil, err
	}

	current, err := qtx.GetTransferByID(ctx, transferID)
	if err != nil {
		logger.Warn().Err(err).Str("transfer_id", transferID).Msg("failed to fetch transfer by id")
		return nil, err
	}
	if current.Status != string(domain.TransferPending) {
		logger.Info().Str("transfer_id", transferID).Str("status", current.Status).Msg("transfer already resolved, skipping finalize")
		return toDomainTransfer(current), nil
	}

	result, err := r.moveAndFinalize(ctx, qtx, transferID, fromWalletID, toWalletID, amount)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		logger.Warn().Err(err).Str("transfer_id", transferID).Msg("failed to commit transfer finalization")
		return nil, err
	}
	logger.Info().Str("transfer_id", transferID).Str("status", string(result.Status)).Msg("finalized transfer")
	return result, nil
}

func (r *TransferRepository) moveAndFinalize(ctx context.Context, qtx *sqlcgen.Queries, transferID, fromWalletID, toWalletID string, amount float64) (*domain.Transfer, error) {
	moveErr := moveFunds(ctx, qtx, fromWalletID, toWalletID, amount)
	if moveErr == nil {
		processed, err := qtx.MarkTransferProcessed(ctx, transferID)
		if err != nil {
			logger.Warn().Err(err).Str("transfer_id", transferID).Msg("failed to mark transfer processed")
			return nil, err
		}
		if err := insertLedgerEntries(ctx, qtx, processed.ID, fromWalletID, toWalletID, amount); err != nil {
			logger.Warn().Err(err).Str("transfer_id", transferID).Msg("failed to insert ledger entries")
			return nil, err
		}
		logger.Debug().Str("transfer_id", transferID).Msg("moved funds and marked transfer processed")
		return toDomainTransfer(processed), nil
	}
	if !errors.Is(moveErr, domain.ErrInsufficientBalance) {
		logger.Warn().Err(moveErr).Str("transfer_id", transferID).Msg("failed to move funds")
		return nil, moveErr
	}

	logger.Warn().Str("transfer_id", transferID).Str("from_wallet_id", fromWalletID).Msg("insufficient balance, marking transfer failed")
	failed, err := qtx.MarkTransferFailed(ctx, sqlcgen.MarkTransferFailedParams{
		ID:            transferID,
		FailureReason: sql.NullString{String: moveErr.Error(), Valid: true},
	})
	if err != nil {
		logger.Warn().Err(err).Str("transfer_id", transferID).Msg("failed to mark transfer failed")
		return nil, err
	}
	return toDomainTransfer(failed), nil
}

func insertPendingTransfer(ctx context.Context, db *sql.DB, q *sqlcgen.Queries, idempotencyKey, fromWalletID, toWalletID string, amount float64) (sqlcgen.Transfer, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to begin transaction for inserting pending transfer")
		return sqlcgen.Transfer{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			logger.Error().Err(err).Msg("error rolling back transaction")
		}
	}()

	qtx := q.WithTx(tx)

	if err := lockWalletPair(ctx, qtx, fromWalletID, toWalletID); err != nil {
		logger.Warn().Err(err).Str("from_wallet_id", fromWalletID).Str("to_wallet_id", toWalletID).Msg("failed to lock wallet pair")
		return sqlcgen.Transfer{}, err
	}

	t, err := qtx.InsertPendingTransfer(ctx, sqlcgen.InsertPendingTransferParams{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == uniqueViolationCode {
			logger.Warn().Str("idempotency_key", idempotencyKey).Msg("unique violation on idempotency key")
			return sqlcgen.Transfer{}, domain.ErrIdempotencyKeyConflict
		}
		logger.Warn().Err(err).Str("idempotency_key", idempotencyKey).Msg("failed to insert pending transfer row")
		return sqlcgen.Transfer{}, err
	}

	if err := tx.Commit(); err != nil {
		logger.Warn().Err(err).Str("idempotency_key", idempotencyKey).Msg("failed to commit pending transfer insert")
		return sqlcgen.Transfer{}, err
	}
	return t, nil
}

func lockWalletPair(ctx context.Context, qtx *sqlcgen.Queries, walletA, walletB string) error {
	sortedIDs := []string{walletA, walletB}
	sort.Strings(sortedIDs)
	logger.Debug().Str("wallet_a", sortedIDs[0]).Str("wallet_b", sortedIDs[1]).Msg("locking wallet pair")
	if err := lockWallet(ctx, qtx, sortedIDs[0]); err != nil {
		return err
	}
	return lockWallet(ctx, qtx, sortedIDs[1])
}

func lockWallet(ctx context.Context, qtx *sqlcgen.Queries, walletID string) error {
	_, err := qtx.LockWalletByID(ctx, walletID)
	if errors.Is(err, sql.ErrNoRows) {
		logger.Warn().Str("wallet_id", walletID).Msg("wallet not found while locking")
		return domain.ErrWalletNotFound
	}
	return err
}

func moveFunds(ctx context.Context, qtx *sqlcgen.Queries, fromWalletID, toWalletID string, amount float64) error {
	fromBalance, err := qtx.GetWalletBalance(ctx, fromWalletID)
	if err != nil {
		logger.Warn().Err(err).Str("wallet_id", fromWalletID).Msg("failed to get wallet balance")
		return err
	}
	if fromBalance < amount {
		logger.Warn().Str("wallet_id", fromWalletID).Float64("balance", fromBalance).Float64("amount", amount).Msg("insufficient balance for transfer")
		return domain.ErrInsufficientBalance
	}

	if err := qtx.DebitWallet(ctx, sqlcgen.DebitWalletParams{Balance: amount, ID: fromWalletID}); err != nil {
		logger.Warn().Err(err).Str("wallet_id", fromWalletID).Msg("failed to debit wallet")
		return err
	}
	if err := qtx.CreditWallet(ctx, sqlcgen.CreditWalletParams{Balance: amount, ID: toWalletID}); err != nil {
		logger.Warn().Err(err).Str("wallet_id", toWalletID).Msg("failed to credit wallet")
		return err
	}
	logger.Debug().Str("from_wallet_id", fromWalletID).Str("to_wallet_id", toWalletID).Float64("amount", amount).Msg("moved funds between wallets")
	return nil
}

func insertLedgerEntries(ctx context.Context, qtx *sqlcgen.Queries, transferID, fromWalletID, toWalletID string, amount float64) error {
	if err := qtx.InsertLedgerEntry(ctx, sqlcgen.InsertLedgerEntryParams{
		TransferID: transferID,
		WalletID:   fromWalletID,
		EntryType:  string(domain.LedgerDebit),
		Amount:     amount,
	}); err != nil {
		logger.Warn().Err(err).Str("transfer_id", transferID).Str("wallet_id", fromWalletID).Msg("failed to insert debit ledger entry")
		return err
	}
	if err := qtx.InsertLedgerEntry(ctx, sqlcgen.InsertLedgerEntryParams{
		TransferID: transferID,
		WalletID:   toWalletID,
		EntryType:  string(domain.LedgerCredit),
		Amount:     amount,
	}); err != nil {
		logger.Warn().Err(err).Str("transfer_id", transferID).Str("wallet_id", toWalletID).Msg("failed to insert credit ledger entry")
		return err
	}
	return nil
}

func toDomainTransfer(t sqlcgen.Transfer) *domain.Transfer {
	var failureReason *string
	if t.FailureReason.Valid {
		failureReason = &t.FailureReason.String
	}

	return &domain.Transfer{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         domain.TransferStatus(t.Status),
		FailureReason:  failureReason,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}
