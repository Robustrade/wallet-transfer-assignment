package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/transfer"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, request transfer.CreateRequest, requestHash [32]byte) (transfer.Outcome, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer tx.Rollback()

	insertResult, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, state)
		VALUES ($1, $2, 'IN_PROGRESS')
		ON CONFLICT (idempotency_key) DO NOTHING`,
		request.IdempotencyKey, requestHash[:],
	)
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("claim idempotency key: %w", err)
	}
	claimed, err := insertResult.RowsAffected()
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("check idempotency claim: %w", err)
	}
	if claimed == 0 {
		return readIdempotentOutcome(ctx, tx, request.IdempotencyKey, requestHash)
	}

	balances, err := lockWallets(ctx, tx, request.FromWalletID, request.ToWalletID)
	if err != nil {
		return transfer.Outcome{}, err
	}
	if len(balances) != 2 {
		return r.completeFailure(ctx, tx, request.IdempotencyKey, nil, 404, transfer.APIError{
			Code:    "wallet_not_found",
			Message: "one or more wallets do not exist",
		})
	}

	fromBalance := balances[request.FromWalletID]
	toBalance := balances[request.ToWalletID]
	if fromBalance < request.Amount {
		transferID, err := newUUID()
		if err != nil {
			return transfer.Outcome{}, fmt.Errorf("create transfer id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state, failure_code, completed_at)
			VALUES ($1, $2, $3, $4, 'FAILED', 'INSUFFICIENT_FUNDS', now())`,
			transferID, request.FromWalletID, request.ToWalletID, request.Amount,
		); err != nil {
			return transfer.Outcome{}, fmt.Errorf("record failed transfer: %w", err)
		}
		return r.completeFailure(ctx, tx, request.IdempotencyKey, &transferID, 409, transfer.APIError{
			Code:       "insufficient_funds",
			Message:    "source wallet has insufficient funds",
			TransferID: transferID,
		})
	}

	transferID, err := newUUID()
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("create transfer id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state)
		VALUES ($1, $2, $3, $4, 'PENDING')`,
		transferID, request.FromWalletID, request.ToWalletID, request.Amount,
	); err != nil {
		return transfer.Outcome{}, fmt.Errorf("create pending transfer: %w", err)
	}

	newFromBalance := fromBalance - request.Amount
	newToBalance := toBalance + request.Amount
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = $1 WHERE id = $2`, newFromBalance, request.FromWalletID); err != nil {
		return transfer.Outcome{}, fmt.Errorf("debit source wallet: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = $1 WHERE id = $2`, newToBalance, request.ToWalletID); err != nil {
		return transfer.Outcome{}, fmt.Errorf("credit destination wallet: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, 'DEBIT', $3)`, transferID, request.FromWalletID, request.Amount,
	); err != nil {
		return transfer.Outcome{}, fmt.Errorf("write debit ledger entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, 'CREDIT', $3)`, transferID, request.ToWalletID, request.Amount,
	); err != nil {
		return transfer.Outcome{}, fmt.Errorf("write credit ledger entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE transfers
		SET state = 'PROCESSED', completed_at = now()
		WHERE id = $1 AND state = 'PENDING'`, transferID,
	); err != nil {
		return transfer.Outcome{}, fmt.Errorf("complete transfer: %w", err)
	}

	response := transfer.Result{
		TransferID:   transferID,
		State:        "PROCESSED",
		FromWalletID: request.FromWalletID,
		ToWalletID:   request.ToWalletID,
		Amount:       request.Amount,
		FromBalance:  newFromBalance,
		ToBalance:    newToBalance,
	}
	body, err := json.Marshal(response)
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("encode transfer response: %w", err)
	}
	storedBody, err := completeIdempotency(ctx, tx, request.IdempotencyKey, &transferID, 201, body)
	if err != nil {
		return transfer.Outcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return transfer.Outcome{}, fmt.Errorf("commit transfer: %w", err)
	}

	return transfer.Outcome{StatusCode: 201, Body: storedBody}, nil
}

func lockWallets(ctx context.Context, tx *sql.Tx, fromWalletID, toWalletID string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, balance
		FROM wallets
		WHERE id = $1 OR id = $2
		ORDER BY id
		FOR UPDATE`, fromWalletID, toWalletID,
	)
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	defer rows.Close()

	balances := make(map[string]int64, 2)
	for rows.Next() {
		var walletID string
		var balance int64
		if err := rows.Scan(&walletID, &balance); err != nil {
			return nil, fmt.Errorf("read wallet balance: %w", err)
		}
		balances[walletID] = balance
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read locked wallets: %w", err)
	}
	return balances, nil
}

func readIdempotentOutcome(ctx context.Context, tx *sql.Tx, key string, requestHash [32]byte) (transfer.Outcome, error) {
	var storedHash []byte
	var state string
	var status sql.NullInt64
	var body []byte
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, state, http_status, response_body
		FROM idempotency_records
		WHERE idempotency_key = $1`, key,
	).Scan(&storedHash, &state, &status, &body)
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("read idempotency record: %w", err)
	}
	if !bytes.Equal(storedHash, requestHash[:]) {
		return transfer.Outcome{}, &transfer.Error{
			StatusCode: 409,
			Code:       "idempotency_key_reused",
			Message:    "idempotencyKey was already used with different request data",
		}
	}
	if state != "COMPLETED" || !status.Valid || len(body) == 0 {
		return transfer.Outcome{}, errors.New("idempotency record is not complete")
	}
	if err := tx.Commit(); err != nil {
		return transfer.Outcome{}, fmt.Errorf("finish idempotent replay: %w", err)
	}
	return transfer.Outcome{StatusCode: int(status.Int64), Body: body}, nil
}

func (r *Repository) completeFailure(ctx context.Context, tx *sql.Tx, key string, transferID *string, status int, response transfer.APIError) (transfer.Outcome, error) {
	body, err := json.Marshal(response)
	if err != nil {
		return transfer.Outcome{}, fmt.Errorf("encode transfer failure: %w", err)
	}
	storedBody, err := completeIdempotency(ctx, tx, key, transferID, status, body)
	if err != nil {
		return transfer.Outcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return transfer.Outcome{}, fmt.Errorf("commit failed transfer result: %w", err)
	}
	return transfer.Outcome{StatusCode: status, Body: storedBody}, nil
}

func completeIdempotency(ctx context.Context, tx *sql.Tx, key string, transferID *string, status int, body []byte) ([]byte, error) {
	var transferReference any
	if transferID != nil {
		transferReference = *transferID
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE idempotency_records
		SET state = 'COMPLETED', transfer_id = $2, http_status = $3,
		    response_body = $4::jsonb, completed_at = now()
		WHERE idempotency_key = $1 AND state = 'IN_PROGRESS'`,
		key, transferReference, status, string(body),
	)
	if err != nil {
		return nil, fmt.Errorf("save idempotency result: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check idempotency completion: %w", err)
	}
	if rowsAffected != 1 {
		return nil, errors.New("idempotency record was not in progress")
	}
	var storedBody []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT response_body
		FROM idempotency_records
		WHERE idempotency_key = $1`, key,
	).Scan(&storedBody); err != nil {
		return nil, fmt.Errorf("read saved idempotency response: %w", err)
	}
	return storedBody, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded), nil
}
