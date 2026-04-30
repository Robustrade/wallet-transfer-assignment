package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gerry1818/wallet-transfer-assignment/internal/logger"
	"github.com/gerry1818/wallet-transfer-assignment/internal/metrics"
	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"github.com/gerry1818/wallet-transfer-assignment/internal/repository"
	"github.com/jackc/pgx/v5"

	"go.uber.org/zap"
)

type TransferService struct {
	repo repository.Repository
}

func NewTransferService(r repository.Repository) *TransferService {
	return &TransferService{repo: r}
}

func hash(req model.TransferRequest) string {
	b, _ := json.Marshal(req)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}

func (s *TransferService) Transfer(
	ctx context.Context,
	req model.TransferRequest,
) (*model.TransferResponse, int, error) {

	logger.Log.Info("transfer start",
		zap.String("key", req.IdempotencyKey),
	)

	var (
		resp      *model.TransferResponse
		httpCode  int
		err       error
		status    string
		respBytes []byte
		tx        repository.Tx
	)

	// -----------------------------
	// 1. Idempotency Claim
	// -----------------------------
	ok, err := s.repo.ClaimIdempotency(ctx, req.IdempotencyKey, hash(req))
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyHashMismatch) {
			metrics.Failure.Inc()
			return nil, 409, fmt.Errorf("idempotency key hash mismatch")
		}
		metrics.Failure.Inc()
		logger.Log.Error(err.Error(),
			zap.String("idempotency_key", req.IdempotencyKey),
			zap.String("request_hash", hash(req)),
			zap.String("error", err.Error()),
		)
		return nil, 500, err
	}

	if !ok {
		resp, code, err := s.repo.GetIdempotency(ctx, req.IdempotencyKey)
		if err != nil {
			return nil, 500, err
		}

		// CASE 1: already completed → return cached response
		if resp != "" {
			var parsed model.TransferResponse

			if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
				logger.Log.Error("failed to parse cached idempotency response",
					zap.String("idempotency_key", req.IdempotencyKey),
					zap.Error(err),
				)
				return nil, 500, err
			}

			logger.Log.Info("idempotency cache hit - returning stored response",
				zap.String("idempotency_key", req.IdempotencyKey),
				zap.Int64("transfer_id", parsed.TransferID),
				zap.Int("status_code", code),
			)
			metrics.IdempotencyHits.Inc()

			return &parsed, code, nil
		}

		// CASE 2: truly in-progress OR first insert race → retry once
		return nil, 409, fmt.Errorf("request in progress")
	}

	// -----------------------------
	// 2. Defer idempotency update
	// -----------------------------
	defer func() {
		if err != nil {
			status = "FAILED"

			if respBytes == nil {
				errorResp, marshalErr := json.Marshal(map[string]string{
					"error": err.Error(),
				})
				if marshalErr != nil {
					respBytes = []byte(`{"error":"failed to encode error response"}`)
				} else {
					respBytes = errorResp
				}
			}

			if tx != nil {
				_ = tx.Rollback(ctx)
			}
		} else {
			status = "COMPLETED"
		}

		_ = s.repo.UpdateIdempotency(
			ctx,
			req.IdempotencyKey,
			status,
			string(respBytes),
			httpCode,
		)
	}()

	// -----------------------------
	// 3. Input validation
	// -----------------------------
	if req.Amount <= 0 {
		metrics.Failure.Inc()
		err = fmt.Errorf("amount must be greater than 0")
		httpCode = 400
		return nil, httpCode, err
	}

	if req.FromWalletID == req.ToWalletID {
		metrics.Failure.Inc()
		err = fmt.Errorf("from and to wallet cannot be same")
		httpCode = 400
		return nil, httpCode, err
	}

	// -----------------------------
	// 4. Begin transaction
	// -----------------------------
	tx, err = s.repo.BeginTx(ctx)
	if err != nil {
		httpCode = 500
		return nil, httpCode, err
	}

	// -----------------------------
	// 5. Lock wallets
	// -----------------------------
	// ✅ lock both wallets in deterministic order to avoid deadlock
	firstWalletID := req.FromWalletID
	secondWalletID := req.ToWalletID
	if firstWalletID > secondWalletID {
		firstWalletID, secondWalletID = secondWalletID, firstWalletID
	}
	firstBal, err := tx.GetWalletForUpdate(ctx, firstWalletID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 404, fmt.Errorf("wallet %d not found", firstWalletID)
		}
		return nil, 500, err
	}
	secondBal, err := tx.GetWalletForUpdate(ctx, secondWalletID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 404, fmt.Errorf("wallet %d not found", secondWalletID)
		}
		return nil, 500, err
	}
	var fromBal, toBal int64
	if req.FromWalletID == firstWalletID {
		fromBal = firstBal
		toBal = secondBal
	} else {
		fromBal = secondBal
		toBal = firstBal
	}

	// -----------------------------
	// 6. Balance check
	// -----------------------------
	if fromBal < req.Amount {
		metrics.Failure.Inc()
		err = fmt.Errorf("insufficient balance")
		httpCode = 400

		logger.Log.Error("transfer rejected due to insufficient balance",
			zap.String("idempotency_key", req.IdempotencyKey),
			zap.Int64("from_wallet_id", req.FromWalletID),
			zap.Int64("to_wallet_id", req.ToWalletID),
			zap.Int64("requested_amount", req.Amount),
			zap.Int64("available_balance", fromBal),
		)

		return nil, httpCode, err
	}

	// -----------------------------
	// 7. Create transfer
	// -----------------------------
	tid, err := tx.CreateTransfer(
		ctx,
		req.FromWalletID,
		req.ToWalletID,
		req.Amount,
		req.IdempotencyKey,
	)
	if err != nil {
		httpCode = 500
		return nil, httpCode, err
	}

	// -----------------------------
	// 8. Update balances
	// -----------------------------
	if err = tx.UpdateWallet(ctx, req.FromWalletID, fromBal-req.Amount); err != nil {
		httpCode = 500
		return nil, httpCode, err
	}

	if err = tx.UpdateWallet(ctx, req.ToWalletID, toBal+req.Amount); err != nil {
		httpCode = 500
		return nil, httpCode, err
	}

	// -----------------------------
	// 9. Ledger entries
	// -----------------------------
	_ = tx.InsertLedgerEntry(ctx, req.FromWalletID, tid, "DEBIT", req.Amount)
	_ = tx.InsertLedgerEntry(ctx, req.ToWalletID, tid, "CREDIT", req.Amount)

	// -----------------------------
	// 10. Update transfer state
	// -----------------------------
	_ = tx.UpdateTransferState(ctx, tid, "PROCESSED")

	// -----------------------------
	// 11. Commit
	// -----------------------------
	if err = tx.Commit(ctx); err != nil {
		httpCode = 500
		return nil, httpCode, err
	}

	// -----------------------------
	// 12. Response
	// -----------------------------
	resp = &model.TransferResponse{
		TransferID: tid,
		Status:     "PROCESSED",
	}

	respBytes, _ = json.Marshal(resp)

	httpCode = 200

	metrics.Success.Inc()

	logger.Log.Info("transfer success",
		zap.Int64("transfer_id", tid),
	)

	return resp, httpCode, nil
}
