package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"wallet-transfer-assignment/internal/domain"
)

const (
	claimInProgress = "IN_PROGRESS"
	claimCompleted  = "COMPLETED"
	claimLeaseSQL   = "INTERVAL '30 seconds'"
)

func (s *Store) ExecuteTransfer(ctx context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	requestHash, err := hashRequest(request)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("hash transfer request: %w", err)
	}
	if err := s.ensureWallets(ctx, request.FromWalletID, request.ToWalletID); err != nil {
		return domain.TransferResult{}, err
	}

	for attempt := 0; attempt < s.maxClaimAttempts; attempt++ {
		ownerToken, claimed, err := s.claimIdempotencyKey(ctx, request.IdempotencyKey, requestHash)
		if err != nil {
			return domain.TransferResult{}, err
		}
		if claimed {
			result, err := s.processClaimedTransfer(ctx, request, ownerToken)
			if err != nil {
				cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				cleanupErr := s.releaseClaim(cleanupContext, request.IdempotencyKey, ownerToken)
				cancel()
				if cleanupErr != nil {
					return domain.TransferResult{}, errors.Join(err, cleanupErr)
				}
				return domain.TransferResult{}, err
			}
			return result, nil
		}

		result, completed, err := s.completedResult(ctx, request.IdempotencyKey, requestHash)
		if err != nil {
			return domain.TransferResult{}, err
		}
		if completed {
			return result, nil
		}
		if attempt+1 < s.maxClaimAttempts {
			timer := time.NewTimer(s.claimRetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return domain.TransferResult{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return domain.TransferResult{}, domain.ErrIdempotencyInProgress
}

func hashRequest(request domain.TransferRequest) ([32]byte, error) {
	payload, err := json.Marshal(struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{request.FromWalletID, request.ToWalletID, request.Amount})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

func (s *Store) ensureWallets(ctx context.Context, walletIDs ...string) error {
	var firstExists, secondExists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM wallets WHERE wallet_id = $1),
       EXISTS (SELECT 1 FROM wallets WHERE wallet_id = $2)`, walletIDs[0], walletIDs[1]).
		Scan(&firstExists, &secondExists)
	if err != nil {
		return fmt.Errorf("validate transfer wallets: %w", err)
	}
	if !firstExists {
		return domain.NewWalletNotFoundError(walletIDs[0])
	}
	if !secondExists {
		return domain.NewWalletNotFoundError(walletIDs[1])
	}
	return nil
}

func (s *Store) claimIdempotencyKey(ctx context.Context, key string, requestHash [32]byte) (string, bool, error) {
	ownerToken, err := newClaimToken()
	if err != nil {
		return "", false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, fmt.Errorf("begin idempotency claim: %w", err)
	}
	defer tx.Rollback()
	var insertedKey string
	err = tx.QueryRowContext(ctx, `
INSERT INTO idempotency_records(key, request_hash, state, owner_token, lease_expires_at)
VALUES ($1, $2, $3, $4, NOW() + `+claimLeaseSQL+`)
ON CONFLICT (key) DO NOTHING
RETURNING key`, key, requestHash[:], claimInProgress, ownerToken).Scan(&insertedKey)
	if errors.Is(err, sql.ErrNoRows) {
		var storedHash []byte
		var state string
		var leaseExpired bool
		if err := tx.QueryRowContext(ctx, `
SELECT request_hash, state, COALESCE(lease_expires_at <= NOW(), FALSE)
FROM idempotency_records WHERE key = $1 FOR UPDATE`, key).
			Scan(&storedHash, &state, &leaseExpired); err != nil {
			return "", false, fmt.Errorf("read existing idempotency claim: %w", err)
		}
		if !equalBytes(storedHash, requestHash[:]) {
			return "", false, domain.ErrIdempotencyConflict
		}
		if state == claimInProgress && leaseExpired {
			result, err := tx.ExecContext(ctx, `
UPDATE idempotency_records
SET owner_token = $1, lease_expires_at = NOW() + `+claimLeaseSQL+`
WHERE key = $2 AND state = $3 AND lease_expires_at <= NOW()`, ownerToken, key, claimInProgress)
			if err != nil {
				return "", false, fmt.Errorf("take over expired idempotency claim: %w", err)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return "", false, fmt.Errorf("check idempotency claim takeover: %w", err)
			}
			if updated == 1 {
				if err := tx.Commit(); err != nil {
					return "", false, fmt.Errorf("commit idempotency claim takeover: %w", err)
				}
				return ownerToken, true, nil
			}
		}
		if err := tx.Commit(); err != nil {
			return "", false, fmt.Errorf("finish duplicate idempotency claim: %w", err)
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("insert idempotency claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit idempotency claim: %w", err)
	}
	return ownerToken, true, nil
}

func (s *Store) completedResult(ctx context.Context, key string, requestHash [32]byte) (domain.TransferResult, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TransferResult{}, false, fmt.Errorf("begin idempotency lookup: %w", err)
	}
	defer tx.Rollback()
	var storedHash []byte
	var state string
	var responseStatus sql.NullInt64
	var responseBody []byte
	err = tx.QueryRowContext(ctx, `
SELECT request_hash, state, response_status, response_body
FROM idempotency_records WHERE key = $1 FOR UPDATE`, key).
		Scan(&storedHash, &state, &responseStatus, &responseBody)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TransferResult{}, false, errors.New("idempotency claim disappeared")
		}
		return domain.TransferResult{}, false, fmt.Errorf("read idempotency claim: %w", err)
	}
	if !equalBytes(storedHash, requestHash[:]) {
		return domain.TransferResult{}, false, domain.ErrIdempotencyConflict
	}
	if state != claimCompleted {
		if err := tx.Commit(); err != nil {
			return domain.TransferResult{}, false, fmt.Errorf("finish incomplete idempotency lookup: %w", err)
		}
		return domain.TransferResult{}, false, nil
	}
	var transfer domain.Transfer
	if err := json.Unmarshal(responseBody, &transfer); err != nil {
		return domain.TransferResult{}, false, fmt.Errorf("decode saved transfer result: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.TransferResult{}, false, fmt.Errorf("finish idempotency replay lookup: %w", err)
	}
	return domain.TransferResult{Transfer: transfer, HTTPStatus: int(responseStatus.Int64)}, true, nil
}

func (s *Store) processClaimedTransfer(ctx context.Context, request domain.TransferRequest, ownerToken string) (domain.TransferResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer tx.Rollback()
	var claimState, currentOwner string
	if err := tx.QueryRowContext(ctx, `
SELECT state, owner_token FROM idempotency_records WHERE key = $1 FOR UPDATE`, request.IdempotencyKey).
		Scan(&claimState, &currentOwner); err != nil {
		return domain.TransferResult{}, fmt.Errorf("lock idempotency claim for transfer: %w", err)
	}
	if claimState != claimInProgress || currentOwner != ownerToken {
		return domain.TransferResult{}, domain.ErrIdempotencyInProgress
	}

	walletIDs := []string{request.FromWalletID, request.ToWalletID}
	sort.Strings(walletIDs)
	rows, err := tx.QueryContext(ctx, `
SELECT wallet_id, balance FROM wallets
WHERE wallet_id = $1 OR wallet_id = $2
ORDER BY wallet_id
FOR UPDATE`, walletIDs[0], walletIDs[1])
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("lock transfer wallets: %w", err)
	}
	balances := make(map[string]int64, 2)
	for rows.Next() {
		var walletID string
		var balance int64
		if err := rows.Scan(&walletID, &balance); err != nil {
			rows.Close()
			return domain.TransferResult{}, fmt.Errorf("read locked wallet: %w", err)
		}
		balances[walletID] = balance
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.TransferResult{}, fmt.Errorf("read locked wallets: %w", err)
	}
	if err := rows.Close(); err != nil {
		return domain.TransferResult{}, fmt.Errorf("close locked wallet rows: %w", err)
	}
	if len(balances) != 2 {
		for _, walletID := range walletIDs {
			if _, exists := balances[walletID]; !exists {
				return domain.TransferResult{}, domain.NewWalletNotFoundError(walletID)
			}
		}
	}

	transfer := domain.Transfer{
		ID:             newTransferID(),
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
		Status:         domain.TransferPending,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO transfers(transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID,
		transfer.Amount, transfer.Status, transfer.CreatedAt,
	); err != nil {
		return domain.TransferResult{}, fmt.Errorf("create pending transfer: %w", err)
	}

	httpStatus := 201
	if balances[request.FromWalletID] < request.Amount {
		transfer.Status = domain.TransferFailed
		transfer.FailureReason = "insufficient funds"
		httpStatus = 422
	} else if balances[request.ToWalletID] > math.MaxInt64-request.Amount {
		transfer.Status = domain.TransferFailed
		transfer.FailureReason = "destination balance overflow"
		httpStatus = 422
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = $1 WHERE wallet_id = $2`, balances[request.FromWalletID]-request.Amount, request.FromWalletID); err != nil {
			return domain.TransferResult{}, fmt.Errorf("debit source wallet: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = $1 WHERE wallet_id = $2`, balances[request.ToWalletID]+request.Amount, request.ToWalletID); err != nil {
			return domain.TransferResult{}, fmt.Errorf("credit destination wallet: %w", err)
		}
		for _, entry := range []struct {
			walletID string
			kind     string
		}{{request.FromWalletID, "DEBIT"}, {request.ToWalletID, "CREDIT"}} {
			var entryID int64
			if err := tx.QueryRowContext(ctx, `
INSERT INTO ledger_entries(account_id, wallet_id, transfer_id, operation_id, entry_type, amount)
				VALUES ($1, $1, $2, $2, $3, $4)
				RETURNING entry_id`, entry.walletID, transfer.ID, entry.kind, request.Amount).Scan(&entryID); err != nil {
				return domain.TransferResult{}, fmt.Errorf("write ledger entry: %w", err)
			}
			transfer.LedgerEntries = append(transfer.LedgerEntries, domain.LedgerEntry{
				ID: entryID, WalletID: entry.walletID, TransferID: transfer.ID, Type: entry.kind, Amount: request.Amount,
			})
		}
		transfer.Status = domain.TransferProcessed
	}
	stateUpdate, err := tx.ExecContext(ctx, `
UPDATE transfers SET status = $1, failure_reason = $2
		WHERE transfer_id = $3 AND status = 'PENDING'`, transfer.Status, transfer.FailureReason, transfer.ID)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("complete transfer state: %w", err)
	}
	stateRows, err := stateUpdate.RowsAffected()
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("check transfer state update: %w", err)
	}
	if stateRows != 1 {
		return domain.TransferResult{}, errors.New("transfer is no longer pending")
	}

	responseBody, err := json.Marshal(transfer)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("encode transfer response: %w", err)
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE idempotency_records
SET state = $1, response_status = $2, response_body = $3::jsonb, completed_at = NOW(),
    owner_token = NULL, lease_expires_at = NULL
WHERE key = $4 AND state = $5 AND owner_token = $6`, claimCompleted, httpStatus, string(responseBody), request.IdempotencyKey, claimInProgress, ownerToken)
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("complete idempotency claim: %w", err)
	}
	rowsAffected, err := updated.RowsAffected()
	if err != nil {
		return domain.TransferResult{}, fmt.Errorf("check completed idempotency claim: %w", err)
	}
	if rowsAffected != 1 {
		return domain.TransferResult{}, errors.New("idempotency claim is no longer active")
	}
	if err := tx.Commit(); err != nil {
		return domain.TransferResult{}, fmt.Errorf("commit transfer transaction: %w", err)
	}
	return domain.TransferResult{Transfer: transfer, HTTPStatus: httpStatus}, nil
}

func (s *Store) releaseClaim(ctx context.Context, key, ownerToken string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM idempotency_records WHERE key = $1 AND state = $2 AND owner_token = $3`, key, claimInProgress, ownerToken)
	if err != nil {
		return fmt.Errorf("release incomplete idempotency claim: %w", err)
	}
	return nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}
