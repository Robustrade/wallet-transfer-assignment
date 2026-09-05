package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"wallet-transfer/internal/transfer/domain"

	"github.com/google/uuid"
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Create(ctx context.Context, request domain.CreateRequest) (domain.Transfer, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Transfer{}, err
	}
	defer tx.Rollback()
	hash := requestHash(request)
	_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records (idempotency_key,request_hash) VALUES ($1,$2) ON CONFLICT (idempotency_key) DO NOTHING`, request.IdempotencyKey, hash)
	if err != nil {
		return domain.Transfer{}, err
	}
	var stored string
	var id sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT request_hash,transfer_id FROM idempotency_records WHERE idempotency_key=$1 FOR UPDATE`, request.IdempotencyKey).Scan(&stored, &id)
	if err != nil {
		return domain.Transfer{}, err
	}
	if stored != hash {
		return domain.Transfer{}, domain.ErrIdempotencyReuse
	}
	if id.Valid {
		t, e := read(ctx, tx, id.String)
		if e != nil {
			return domain.Transfer{}, e
		}
		if e = tx.Commit(); e != nil {
			return domain.Transfer{}, e
		}
		return t, replayError(t)
	}
	ids := []string{request.FromWalletID, request.ToWalletID}
	sort.Strings(ids)
	for _, walletID := range ids {
		var balance int64
		if err = tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id=$1 FOR UPDATE`, walletID).Scan(&balance); errors.Is(err, sql.ErrNoRows) {
			return domain.Transfer{}, domain.ErrWalletNotFound
		} else if err != nil {
			return domain.Transfer{}, err
		}
	}
	transferID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO transfers (id,idempotency_key,from_wallet_id,to_wallet_id,amount,status) VALUES ($1,$2,$3,$4,$5,$6)`, transferID, request.IdempotencyKey, request.FromWalletID, request.ToWalletID, request.Amount, domain.Pending)
	if err != nil {
		return domain.Transfer{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE idempotency_records SET transfer_id=$1 WHERE idempotency_key=$2`, transferID, request.IdempotencyKey)
	if err != nil {
		return domain.Transfer{}, err
	}
	var balance int64
	if err = tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id=$1`, request.FromWalletID).Scan(&balance); err != nil {
		return domain.Transfer{}, err
	}
	if balance < request.Amount {
		return fail(ctx, tx, transferID, domain.ErrInsufficientFunds)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE wallets SET balance=balance-$1,updated_at=NOW() WHERE id=$2`, request.Amount, request.FromWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE wallets SET balance=balance+$1,updated_at=NOW() WHERE id=$2`, request.Amount, request.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entries (transfer_id,wallet_id,entry_type,amount) VALUES ($1,$2,'DEBIT',$3),($1,$4,'CREDIT',$3)`, transferID, request.FromWalletID, request.Amount, request.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}
	var entryCount int
	var debitTotal, creditTotal int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(amount) FILTER (WHERE entry_type = 'DEBIT'), 0), COALESCE(SUM(amount) FILTER (WHERE entry_type = 'CREDIT'), 0) FROM ledger_entries WHERE transfer_id = $1`, transferID).Scan(&entryCount, &debitTotal, &creditTotal); err != nil {
		return domain.Transfer{}, err
	}
	if entryCount != 2 || debitTotal != creditTotal || debitTotal != request.Amount {
		return domain.Transfer{}, domain.ErrLedgerUnbalanced
	}
	if _, err = tx.ExecContext(ctx, `UPDATE transfers SET status=$1,updated_at=NOW() WHERE id=$2`, domain.Processed, transferID); err != nil {
		return domain.Transfer{}, err
	}
	t, err := read(ctx, tx, transferID)
	if err != nil {
		return domain.Transfer{}, err
	}
	if err = persistResponse(ctx, tx, request.IdempotencyKey, t, 201); err != nil {
		return domain.Transfer{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Transfer{}, err
	}
	return t, nil
}
func (r *Repository) ListAll(ctx context.Context) ([]domain.Transfer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,failure_reason,created_at,updated_at FROM transfers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Transfer, 0)
	for rows.Next() {
		var t domain.Transfer
		var reason sql.NullString
		if err := rows.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &reason, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.FailureReason = reason.String
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Transfer, error) {
	var transfer domain.Transfer
	var reason sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,failure_reason,created_at,updated_at FROM transfers WHERE id=$1`, id).Scan(&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.Status, &reason, &transfer.CreatedAt, &transfer.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("get transfer: %w", err)
	}
	transfer.FailureReason = reason.String
	return transfer, nil
}
func read(ctx context.Context, tx *sql.Tx, id string) (domain.Transfer, error) {
	var t domain.Transfer
	var reason sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,failure_reason,created_at,updated_at FROM transfers WHERE id=$1`, id).Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &reason, &t.CreatedAt, &t.UpdatedAt)
	t.FailureReason = reason.String
	return t, err
}
func fail(ctx context.Context, tx *sql.Tx, id string, e error) (domain.Transfer, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE transfers SET status=$1,failure_reason=$2,updated_at=NOW() WHERE id=$3`, domain.Failed, e.Error(), id); err != nil {
		return domain.Transfer{}, err
	}
	t, err := read(ctx, tx, id)
	if err != nil {
		return domain.Transfer{}, err
	}
	if err = persistResponse(ctx, tx, t.IdempotencyKey, t, 409); err != nil {
		return domain.Transfer{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Transfer{}, err
	}
	return t, e
}

func persistResponse(ctx context.Context, tx *sql.Tx, key string, transfer domain.Transfer, status int) error {
	body, err := json.Marshal(transfer)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE idempotency_records SET response_status=$1,response_body=$2 WHERE idempotency_key=$3`, status, body, key)
	return err
}
func replayError(t domain.Transfer) error {
	if t.Status == domain.Failed {
		return domain.ErrInsufficientFunds
	}
	return nil
}
func requestHash(r domain.CreateRequest) string {
	canonical := fmt.Appendf(nil, "%s|%s|%d", r.FromWalletID, r.ToWalletID, r.Amount)
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:])
}
