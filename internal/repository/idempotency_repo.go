package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"wallet-transfer-service/internal/apperror"
	"wallet-transfer-service/internal/model"
)

// var (
// 	ErrRequestInProgress = errors.New("request currently in progress, please retry shortly")
// 	ErrPayloadMismatch   = errors.New("idempotency key already used with different payload")
// )

type IdempotencyRepository interface {
	TryAcquire(ctx context.Context, key, requestHash string, lockTTL, retention time.Duration) (*model.IdempotencyRecord, bool, error)
	SaveResult(ctx context.Context, key string, status model.IdempotencyStatus, code int, body string) error
}

type idempotencyRepository struct {
	db *sql.DB
}

func NewIdempotencyRepository(db *sql.DB) IdempotencyRepository {
	return &idempotencyRepository{db: db}
}

func (r *idempotencyRepository) TryAcquire(
	ctx context.Context,
	key string,
	requestHash string,
	lockTTL time.Duration,
	retention time.Duration,
) (*model.IdempotencyRecord, bool, error) {
	slog.DebugContext(ctx, "idempotency repository acquire started", "idempotency_key", key)

	// Atomic insert for first-time callers
	insertQuery := `
		INSERT INTO idempotency_records (key, request_hash, status, created_at, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (key) DO NOTHING
	`
	res, err := r.db.ExecContext(ctx, insertQuery, key, requestHash, model.IdempotencyStatusStarted)
	if err != nil {
		slog.ErrorContext(ctx, "idempotency repository insert failed", "idempotency_key", key, "error", err)
		return nil, false, fmt.Errorf("failed to insert idempotency record: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("failed checking rows affected: %w", err)
	}

	// First time caller got the key
	if rowsAffected == 1 {
		slog.DebugContext(ctx, "idempotency repository lease acquired", "idempotency_key", key)
		return nil, true, nil
	}

	// Row exists: Read existing record
	selectQuery := `
		SELECT key, request_hash, status, COALESCE(response_code, 0), COALESCE(response_body, ''), created_at, updated_at
		FROM idempotency_records
		WHERE key = $1
	`
	var rec model.IdempotencyRecord
	err = r.db.QueryRowContext(ctx, selectQuery, key).Scan(
		&rec.Key,
		&rec.RequestHash,
		&rec.Status,
		&rec.ResponseCode,
		&rec.ResponseBody,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		slog.ErrorContext(ctx, "idempotency repository lookup failed", "idempotency_key", key, "error", err)
		return nil, false, fmt.Errorf("failed fetching existing idempotency record: %w", err)
	}

	// Payload integrity check (prevent key reuse with different parameters)
	if rec.RequestHash != requestHash {
		slog.WarnContext(ctx, "idempotency repository payload mismatch", "idempotency_key", key)
		return nil, false, apperror.ErrPayloadMismatch
	}

	// Expiration check (Already marked EXPIRED, or exceeds retention window)
	if rec.Status == model.IdempotencyStatusExpired || time.Since(rec.CreatedAt) > retention {
		if rec.Status != model.IdempotencyStatusExpired {
			expireQuery := `
				UPDATE idempotency_records
				SET status = $1, updated_at = CURRENT_TIMESTAMP
				WHERE key = $2
			`
			if _, err := r.db.ExecContext(ctx, expireQuery, model.IdempotencyStatusExpired, key); err != nil {
				slog.WarnContext(ctx, "failed to transition expired idempotency record", "idempotency_key", key, "error", err)
			}
		}
		return nil, false, apperror.ErrIdempotencyKeyExpired
	}

	// Active lease / crash recovery check
	if rec.Status == model.IdempotencyStatusStarted {
		if time.Since(rec.UpdatedAt) > lockTTL {
			// Attempt safe lease takeover:
			// Non-blocking row-level lock check (FOR UPDATE NOWAIT) ensures we do not
			// steal a lease from a worker actively executing a long-running transaction.
			tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				slog.ErrorContext(ctx, "failed starting transaction for lease takeover", "idempotency_key", key, "error", err)
				return nil, false, fmt.Errorf("failed starting lease takeover tx: %w", err)
			}
			defer tx.Rollback()

			lockQuery := `
				SELECT status, COALESCE(response_code, 0), COALESCE(response_body, '')
				FROM idempotency_records 
				WHERE key = $1 
				FOR UPDATE NOWAIT
			`
			var currentStatus model.IdempotencyStatus
			var respCode int
			var respBody string
			err = tx.QueryRowContext(ctx, lockQuery, key).Scan(&currentStatus, &respCode, &respBody)
			if err != nil {
				// Interface check for PostgreSQL error code "55P03" (lock_not_available)
				// Compatible with lib/pq (*pq.Error) and pgx (*pgconn.PgError)
				type pgErrorCode interface {
					Code() string
				}
				type pqErrorCode interface {
					Get(byte) string
				}

				var pgErr pgErrorCode
				var pqErr pqErrorCode
				isLockNotAvailable := false

				if errors.As(err, &pgErr) && pgErr.Code() == "55P03" {
					isLockNotAvailable = true
				} else if errors.As(err, &pqErr) && pqErr.Get('C') == "55P03" {
					isLockNotAvailable = true
				}

				if isLockNotAvailable {
					slog.DebugContext(ctx, "idempotency row lock held by active worker", "idempotency_key", key)
					return nil, false, apperror.ErrRequestInProgress
				}

				slog.ErrorContext(ctx, "failed probing idempotency lock", "idempotency_key", key, "error", err)
				return nil, false, fmt.Errorf("failed probing idempotency lock: %w", err)
			}

			// If the original transaction completed or failed right before we locked,
			// return the newly finalized cached response rather than a spurious 409.
			if currentStatus != model.IdempotencyStatusStarted {
				_ = tx.Commit()
				rec.Status = currentStatus
				rec.ResponseCode = respCode
				rec.ResponseBody = respBody
				slog.InfoContext(ctx, "idempotency transaction resolved during takeover lock", "idempotency_key", key, "status", currentStatus)
				return &rec, false, nil
			}

			takeoverQuery := `
				UPDATE idempotency_records
				SET updated_at = CURRENT_TIMESTAMP
				WHERE key = $1 AND status = $2 AND updated_at = $3
			`
			updateRes, updateErr := tx.ExecContext(ctx, takeoverQuery, key, model.IdempotencyStatusStarted, rec.UpdatedAt)
			if updateErr != nil {
				slog.ErrorContext(ctx, "failed updating record for lease takeover", "idempotency_key", key, "error", updateErr)
				return nil, false, fmt.Errorf("failed executing lease takeover: %w", updateErr)
			}

			count, rowsErr := updateRes.RowsAffected()
			if rowsErr != nil {
				return nil, false, fmt.Errorf("failed checking rows affected on lease takeover: %w", rowsErr)
			}

			if count == 1 {
				if commitErr := tx.Commit(); commitErr != nil {
					slog.ErrorContext(ctx, "failed committing lease takeover tx", "idempotency_key", key, "error", commitErr)
					return nil, false, fmt.Errorf("failed committing lease takeover: %w", commitErr)
				}
				slog.InfoContext(ctx, "idempotency repository reclaimed expired lease", "idempotency_key", key)
				return nil, true, nil
			}
		}
		return nil, false, apperror.ErrRequestInProgress
	}

	// Return cached completed/failed record
	slog.DebugContext(ctx, "idempotency repository returning cached record", "idempotency_key", key, "status", rec.Status)
	return &rec, false, nil
}

func (r *idempotencyRepository) SaveResult(ctx context.Context, key string, status model.IdempotencyStatus, code int, body string) error {
	slog.DebugContext(ctx, "idempotency repository save result started", "idempotency_key", key, "status", status, "response_code", code)
	query := `
		UPDATE idempotency_records
		SET status = $1, response_code = $2, response_body = $3, updated_at = CURRENT_TIMESTAMP
		WHERE key = $4
	`
	res, err := r.db.ExecContext(ctx, query, status, code, body, key)
	if err != nil {
		slog.ErrorContext(ctx, "idempotency repository save result failed", "idempotency_key", key, "error", err)
		return fmt.Errorf("failed saving idempotency outcome: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed checking rows affected for key %s: %w", key, err)
	}
	if rows != 1 {
		return fmt.Errorf("idempotency record update affected %d rows; expected 1 for key %s", rows, key)
	}

	slog.DebugContext(ctx, "idempotency repository save result completed", "idempotency_key", key)
	return nil
}
