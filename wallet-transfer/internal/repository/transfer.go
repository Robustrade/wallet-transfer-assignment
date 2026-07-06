package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type TransferRepository struct {
	db *sql.DB
}

func NewTransferRepository(db *sql.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

// GetIdempotencyRecord fetches an existing idempotency record by key
func (r *TransferRepository) GetIdempotencyRecord(ctx context.Context, key string) (string, *domain.TransferResponse, error) {
	var hash, body string
	err := r.db.QueryRowContext(ctx, "SELECT request_hash, response_body FROM idempotency_records WHERE idempotency_key = $1", key).Scan(&hash, &body)
	if err != nil {
		log.Printf("[Repository] GetIdempotencyRecord - DB Error for key %s: %v", key, err)
		return "", nil, err
	}

	if body == "" {
		return hash, nil, sql.ErrNoRows // Should not happen with successful transaction commit
	}

	var resp domain.TransferResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", nil, err
	}

	return hash, &resp, nil
}

// ExecuteTransferTx handles the atomic database transaction for a transfer.
func (r *TransferRepository) ExecuteTransferTx(ctx context.Context, req domain.TransferRequest, reqHash string) (*domain.TransferResponse, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[Repository] ExecuteTransferTx - BeginTx Error: %v", err)
		return nil, err
	}
	defer tx.Rollback() // Safe to call even if committed

	// 1. Idempotency Gatekeeper: Try to insert the idempotency key.
	// Postgres will block concurrent requests here if they use the same key.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, response_code, response_body)
		VALUES ($1, $2, 0, '')
	`, req.IdempotencyKey, reqHash)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // 23505 is unique_violation
			log.Printf("[Repository] ExecuteTransferTx - Idempotency duplicate key %s", req.IdempotencyKey)
			return nil, domain.ErrDuplicateRequest
		}
		log.Printf("[Repository] ExecuteTransferTx - Insert Idempotency Error: %v", err)
		return nil, err
	}

	// 2. Lock rows: Sort IDs lexicographically to prevent deadlocks
	id1, id2 := req.FromWalletId, req.ToWalletId
	if id1 > id2 {
		id1, id2 = id2, id1
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, balance FROM wallets WHERE id IN ($1, $2) ORDER BY id FOR UPDATE
	`, id1, id2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	balances := make(map[string]float64)
	for rows.Next() {
		var id string
		var bal float64
		if err := rows.Scan(&id, &bal); err != nil {
			return nil, err
		}
		balances[id] = bal
	}

	// Verify both wallets exist
	if _, ok := balances[req.FromWalletId]; !ok {
		log.Printf("[Repository] ExecuteTransferTx - Sender Wallet %s not found", req.FromWalletId)
		return nil, domain.ErrWalletNotFound
	}
	if _, ok := balances[req.ToWalletId]; !ok {
		log.Printf("[Repository] ExecuteTransferTx - Receiver Wallet %s not found", req.ToWalletId)
		return nil, domain.ErrWalletNotFound
	}

	// 3. START STATE MACHINE: Insert as PENDING
	transferID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5, 'PENDING')
	`, transferID, req.IdempotencyKey, req.FromWalletId, req.ToWalletId, req.Amount)
	if err != nil {
		return nil, err
	}

	// 4. Verify balance
	if balances[req.FromWalletId] < req.Amount {
		// Log failed transfer and commit as per DESIGN_FINAL.MD
		// STATE TRANSITION: PENDING -> FAILED
		_, err = tx.ExecContext(ctx, "UPDATE transfers SET status = 'FAILED' WHERE id = $1", transferID)
		if err != nil {
			return nil, err
		}

		resp := domain.TransferResponse{
			TransferId:     transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletId:   req.FromWalletId,
			ToWalletId:     req.ToWalletId,
			Amount:         req.Amount,
			Status:         domain.TransferStatusFailed,
			CreatedAt:      time.Now(),
		}
		respJSON, _ := json.Marshal(resp)

		_, err = tx.ExecContext(ctx, `
			UPDATE idempotency_records SET response_code = 422, response_body = $1 WHERE idempotency_key = $2
		`, string(respJSON), req.IdempotencyKey)

		if err == nil {
			tx.Commit() // Commit the failure state!
		}

		return &resp, domain.ErrInsufficientFunds
	}

	// 5. Apply Financial Mutations
	// Update balances
	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromWalletId)
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, "UPDATE wallets SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToWalletId)
	if err != nil {
		return nil, err
	}

	// Insert Ledger entries (Safe now, because the PENDING transfer record already exists!)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount) 
		VALUES ($1, $2, $3, 'DEBIT', $4), ($5, $6, $7, 'CREDIT', $8)
	`,
		uuid.New().String(), transferID, req.FromWalletId, req.Amount,
		uuid.New().String(), transferID, req.ToWalletId, req.Amount,
	)
	if err != nil {
		return nil, err
	}

	// 6. STATE TRANSITION: PENDING -> PROCESSED
	_, err = tx.ExecContext(ctx, "UPDATE transfers SET status = 'PROCESSED' WHERE id = $1", transferID)
	if err != nil {
		return nil, err
	}

	// 7. Cache Output and Commit
	resp := domain.TransferResponse{
		TransferId:     transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletId:   req.FromWalletId,
		ToWalletId:     req.ToWalletId,
		Amount:         req.Amount,
		Status:         domain.TransferStatusProcessed,
		CreatedAt:      time.Now(),
	}
	respJSON, _ := json.Marshal(resp)

	_, err = tx.ExecContext(ctx, `
		UPDATE idempotency_records SET response_code = 200, response_body = $1 WHERE idempotency_key = $2
	`, string(respJSON), req.IdempotencyKey)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[Repository] ExecuteTransferTx - Commit Error: %v", err)
		return nil, err
	}

	return &resp, nil
}
