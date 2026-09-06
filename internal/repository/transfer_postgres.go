package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const pqUniqueViolation = "23505"

type PostgresTransferRepository struct {
	db *sql.DB
}

func NewPostgresTransferRepository(db *sql.DB) *PostgresTransferRepository {
	return &PostgresTransferRepository{db: db}
}

// transferResponse is the JSON shape stored in idempotency_records.response_body
// and returned verbatim on replay. Keeping it separate from domain.Transfer
// means the stored payload is decoupled from internal field naming.
type transferResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
	Error      *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func requestHash(req domain.CreateTransferRequest) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", req.FromWalletID, req.ToWalletID, req.Amount)))
	return hex.EncodeToString(sum[:])
}

func (r *PostgresTransferRepository) ExecuteTransfer(ctx context.Context, req domain.CreateTransferRequest) (*ExecutionResult, error) {
	hash := requestHash(req)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	// Safe no-op if we already committed below.
	defer tx.Rollback()

	var idempotencyID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		 VALUES ($1, $2, 'IN_PROGRESS')
		 RETURNING id`,
		req.IdempotencyKey, hash,
	).Scan(&idempotencyID)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			// Someone else already owns (or owned) this idempotency key.
			// Our own transaction is now aborted; roll it back explicitly
			// before reading the winning record with a fresh query.
			tx.Rollback()
			return r.resolveExistingIdempotencyRecord(ctx, req, hash)
		}
		return nil, fmt.Errorf("claiming idempotency key: %w", err)
	}

	// We own this key. Execute the transfer for the first time.
	result, err := r.executeNewTransfer(ctx, tx, req, idempotencyID)
	if err != nil {
		return nil, err // tx rolled back by deferred Rollback()
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transfer: %w", err)
	}

	return result, nil
}

// resolveExistingIdempotencyRecord is called after losing the race for an
// idempotency key. By the time our blocked INSERT above returned a unique
// violation, the winning transaction had already committed (Postgres only
// raises the violation once the conflicting row is durably committed and
// visible), so this record is guaranteed to be in a terminal state.
func (r *PostgresTransferRepository) resolveExistingIdempotencyRecord(ctx context.Context, req domain.CreateTransferRequest, hash string) (*ExecutionResult, error) {
	var (
		existingHash   string
		status         string
		responseStatus sql.NullInt64
		responseBody   []byte
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT request_hash, status, response_status, response_body
		 FROM idempotency_records WHERE idempotency_key = $1`,
		req.IdempotencyKey,
	).Scan(&existingHash, &status, &responseStatus, &responseBody)
	if err != nil {
		return nil, fmt.Errorf("reading existing idempotency record: %w", err)
	}

	if existingHash != hash {
		return &ExecutionResult{
			Outcome:        OutcomeConflict,
			ConflictReason: "idempotency key already used with different request parameters",
		}, nil
	}

	if status == string(domain.IdempotencyInProgress) {
		// Should be rare in practice (see note in design.md): a request
		// with this exact key+params is currently mid-flight elsewhere.
		return &ExecutionResult{
			Outcome:        OutcomeConflict,
			ConflictReason: "a request with this idempotency key is already being processed",
		}, nil
	}

	var replay transferResponse
	if err := json.Unmarshal(responseBody, &replay); err != nil {
		return nil, fmt.Errorf("unmarshalling stored response: %w", err)
	}

	transferID, err := uuid.Parse(replay.TransferID)
	if err != nil {
		return nil, fmt.Errorf("parsing stored transfer id: %w", err)
	}

	return &ExecutionResult{
		Outcome: OutcomeReplayed,
		Transfer: &domain.Transfer{
			ID:           transferID,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       domain.TransferStatus(replay.Status),
		},
	}, nil
}

// executeNewTransfer performs the actual transfer logic within tx: lock
// wallets in deterministic order, validate, move balances, write the
// double-entry ledger, transition transfer state, and record the
// idempotency result. All of it commits or rolls back atomically.
func (r *PostgresTransferRepository) executeNewTransfer(ctx context.Context, tx *sql.Tx, req domain.CreateTransferRequest, idempotencyID uuid.UUID) (*ExecutionResult, error) {
	// Deterministic lock order (by wallet ID) regardless of transfer
	// direction: two transfers A->B and B->A both attempt to lock the
	// lower-ID wallet first, so they block on each other in a consistent
	// order instead of each holding one lock and waiting on the other.
	ids := []uuid.UUID{req.FromWalletID, req.ToWalletID}
	if ids[0].String() > ids[1].String() {
		ids[0], ids[1] = ids[1], ids[0]
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, balance FROM wallets WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`,
		ids[0], ids[1],
	)
	if err != nil {
		return nil, fmt.Errorf("locking wallets: %w", err)
	}
	balances := make(map[uuid.UUID]int64)
	for rows.Next() {
		var (
			id          uuid.UUID
			balanceText string
		)
		if err := rows.Scan(&id, &balanceText); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning wallet row: %w", err)
		}
		cents, err := domain.NumericToCents(balanceText)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("parsing wallet balance: %w", err)
		}
		balances[id] = cents
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating wallet rows: %w", err)
	}
	rows.Close()

	fromBalance, fromOK := balances[req.FromWalletID]
	_, toOK := balances[req.ToWalletID]
	if !fromOK || !toOK {
		return nil, domain.ErrWalletNotFound
	}

	var transferID uuid.UUID
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO transfers (from_wallet_id, to_wallet_id, amount, status)
		 VALUES ($1, $2, $3, 'PENDING')
		 RETURNING id`,
		req.FromWalletID, req.ToWalletID, domain.CentsToNumeric(req.Amount),
	).Scan(&transferID); err != nil {
		return nil, fmt.Errorf("creating transfer: %w", err)
	}

	if fromBalance < req.Amount {
		if err := r.transitionTransfer(ctx, tx, transferID, domain.StatusFailed, "insufficient funds"); err != nil {
			return nil, err
		}
		transfer := &domain.Transfer{
			ID:            transferID,
			FromWalletID:  req.FromWalletID,
			ToWalletID:    req.ToWalletID,
			Amount:        req.Amount,
			Status:        domain.StatusFailed,
			FailureReason: "insufficient funds",
		}
		if err := r.recordIdempotencyResult(ctx, tx, idempotencyID, domain.IdempotencyFailed, 409, transferResponse{
			TransferID: transferID.String(),
			Status:     string(domain.StatusFailed),
			Error: &struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}{Code: "INSUFFICIENT_FUNDS", Message: "insufficient wallet balance"},
		}, &transferID); err != nil {
			return nil, err
		}
		return &ExecutionResult{Outcome: OutcomeExecuted, Transfer: transfer}, nil
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance - $1, updated_at = now() WHERE id = $2`,
		domain.CentsToNumeric(req.Amount), req.FromWalletID,
	); err != nil {
		return nil, fmt.Errorf("debiting source wallet: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + $1, updated_at = now() WHERE id = $2`,
		domain.CentsToNumeric(req.Amount), req.ToWalletID,
	); err != nil {
		return nil, fmt.Errorf("crediting destination wallet: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		 VALUES ($1, $2, 'DEBIT', $3), ($1, $4, 'CREDIT', $3)`,
		transferID, req.FromWalletID, domain.CentsToNumeric(req.Amount), req.ToWalletID,
	); err != nil {
		return nil, fmt.Errorf("writing ledger entries: %w", err)
	}

	if err := r.transitionTransfer(ctx, tx, transferID, domain.StatusProcessed, ""); err != nil {
		return nil, err
	}

	transfer := &domain.Transfer{
		ID:           transferID,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       domain.StatusProcessed,
	}

	if err := r.recordIdempotencyResult(ctx, tx, idempotencyID, domain.IdempotencyCompleted, 201, transferResponse{
		TransferID: transferID.String(),
		Status:     string(domain.StatusProcessed),
	}, &transferID); err != nil {
		return nil, err
	}

	return &ExecutionResult{Outcome: OutcomeExecuted, Transfer: transfer}, nil
}

// transitionTransfer performs a guarded state transition: the WHERE clause
// only matches rows currently in a state from which `to` is a legal move,
// so a bug elsewhere that tries to double-process a transfer fails loudly
// (RowsAffected == 0) instead of corrupting state. The DB trigger installed
// in migrations is a second, independent enforcement of the same rule.
func (r *PostgresTransferRepository) transitionTransfer(ctx context.Context, tx *sql.Tx, id uuid.UUID, to domain.TransferStatus, failureReason string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE transfers SET status = $1, failure_reason = NULLIF($2, ''), updated_at = now()
		 WHERE id = $3 AND status = 'PENDING'`,
		string(to), failureReason, id,
	)
	if err != nil {
		return fmt.Errorf("transitioning transfer to %s: %w", to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking transition result: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: transfer %s was not in PENDING state", domain.ErrInvalidTransition, id)
	}
	return nil
}

func (r *PostgresTransferRepository) recordIdempotencyResult(ctx context.Context, tx *sql.Tx, id uuid.UUID, status domain.IdempotencyStatus, httpStatus int, body transferResponse, transferID *uuid.UUID) error {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshalling idempotency response: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE idempotency_records
		 SET status = $1, response_status = $2, response_body = $3, transfer_id = $4, updated_at = now()
		 WHERE id = $5`,
		string(status), httpStatus, bodyJSON, transferID, id,
	); err != nil {
		return fmt.Errorf("recording idempotency result: %w", err)
	}
	return nil
}
