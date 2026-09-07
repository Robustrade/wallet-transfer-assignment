package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"wallet-transfer-service/internal/apperror"
	"wallet-transfer-service/internal/config"
	"wallet-transfer-service/internal/model"
	"wallet-transfer-service/internal/repository"
)

type TransferService interface {
	Transfer(ctx context.Context, req *model.CreateTransferRequest) (*model.CreateTransferResponse, int, error)
}

type transferService struct {
	transferRepo    repository.TransferRepository
	idempotencyRepo repository.IdempotencyRepository
	cfg             *config.Config
}

func NewTransferService(
	transferRepo repository.TransferRepository,
	idempotencyRepo repository.IdempotencyRepository,
	cfg *config.Config,
) TransferService {
	return &transferService{
		transferRepo:    transferRepo,
		idempotencyRepo: idempotencyRepo,
		cfg:             cfg,
	}
}

func (s *transferService) Transfer(ctx context.Context, req *model.CreateTransferRequest) (*model.CreateTransferResponse, int, error) {
	logger := slog.With(
		"idempotency_key", req.IdempotencyKey,
		"from_wallet_id", req.FromWalletID,
		"to_wallet_id", req.ToWalletID,
		"amount", req.Amount,
	)

	// Parse string amount into minor units (int64)
	amountMinor, err := model.ParseToMinorUnits(req.Amount)
	if err != nil || amountMinor <= 0 {
		logger.WarnContext(ctx, "transfer rejected: non-positive or invalid amount format")
		return nil, http.StatusBadRequest, apperror.ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		logger.WarnContext(ctx, "transfer rejected: self transfer attempt")
		return nil, http.StatusBadRequest, apperror.ErrSelfTransfer
	}

	// Generate deterministic SHA-256 fingerprint using normalized minor units
	reqHash := s.computeRequestHash(req.FromWalletID, req.ToWalletID, amountMinor)
	logger.DebugContext(ctx, "computed payload fingerprint", "hash", reqHash)

	// Acquire or check idempotency lease
	record, acquired, err := s.idempotencyRepo.TryAcquire(
		ctx,
		req.IdempotencyKey,
		reqHash,
		s.cfg.IdempotencyLockTTL,
		s.cfg.IdempotencyRetention,
	)
	if err != nil {
		if errors.Is(err, apperror.ErrPayloadMismatch) {
			logger.WarnContext(ctx, "idempotency key reused with mismatched payload")
			return nil, http.StatusBadRequest, err
		}
		if errors.Is(err, apperror.ErrIdempotencyKeyExpired) {
			logger.WarnContext(ctx, "idempotency key expired beyond retention window")
			return nil, http.StatusBadRequest, err
		}
		if errors.Is(err, apperror.ErrRequestInProgress) {
			logger.InfoContext(ctx, "concurrent request detected; in progress lock active")
			return nil, http.StatusConflict, err
		}
		logger.ErrorContext(ctx, "idempotency check error", "error", err)
		return nil, http.StatusInternalServerError, fmt.Errorf("idempotency check failed: %w", err)
	}

	// If not acquired, return the cached historical response (success or failure)
	if !acquired {
		logger.InfoContext(ctx, "returning cached idempotent response",
			"cached_status", record.Status,
			"cached_code", record.ResponseCode,
		)
		if record.Status == model.IdempotencyStatusFailed {
			return nil, record.ResponseCode, errors.New(extractErrorMessage(record.ResponseBody))
		}

		var cachedResp model.CreateTransferResponse
		if err := json.Unmarshal([]byte(record.ResponseBody), &cachedResp); err != nil {
			logger.ErrorContext(ctx, "failed to unmarshal cached response body",
				"error", err,
				"response_body", record.ResponseBody,
			)
			return nil, http.StatusInternalServerError, fmt.Errorf("corrupted idempotency payload: %w", err)
		}
		return &cachedResp, record.ResponseCode, nil
	}

	logger.InfoContext(ctx, "idempotency lease acquired; executing transfer")

	// Construct transfer domain entity and expected response payload
	transferID := uuid.New().String()
	transfer := &model.Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         amountMinor,
		Status:         model.TransferStatusPending,
	}

	successResponse := model.CreateTransferResponse{
		TransferID:   transferID,
		Status:       model.TransferStatusProcessed,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       model.FormatMinorUnits(amountMinor),
	}
	responseBytes, err := json.Marshal(successResponse)
	if err != nil {
		logger.ErrorContext(ctx, "failed marshaling transfer response", "error", err)
		return nil, http.StatusInternalServerError, fmt.Errorf("failed marshaling response: %w", err)
	}

	// Execute atomic transfer in PostgreSQL.
	// ExecuteTransfer handles ledger writes, failure auditing, and idempotency status finalization atomically.
	err = s.transferRepo.ExecuteTransfer(ctx, transfer, string(responseBytes))
	if err != nil {
		if errors.Is(err, apperror.ErrInsufficientBalance) ||
			errors.Is(err, apperror.ErrWalletNotFound) ||
			errors.Is(err, apperror.ErrSourceDebitBlocked) ||
			errors.Is(err, apperror.ErrDestCreditBlocked) ||
			errors.Is(err, apperror.ErrWalletFrozen) ||
			errors.Is(err, apperror.ErrWalletClosed) ||
			errors.Is(err, apperror.ErrBalanceOverflow) {

			statusCode := http.StatusBadRequest
			if errors.Is(err, apperror.ErrWalletNotFound) {
				statusCode = http.StatusNotFound
			}

			logger.WarnContext(ctx, "transfer rejected by business rules",
				"reason", err.Error(),
				"http_status", statusCode,
			)

			// ExecuteTransfer has already audited the failure and updated idempotency_records atomically.
			// Return directly to preserve identical state on idempotency replay.
			return nil, statusCode, err
		}

		logger.ErrorContext(ctx, "system failure during transfer execution", "error", err)

		// Persist a generic 500 FAILED idempotency outcome if an unhandled infrastructure error occurs
		if saveErr := s.idempotencyRepo.SaveResult(
			ctx,
			req.IdempotencyKey,
			model.IdempotencyStatusFailed,
			http.StatusInternalServerError,
			`{"error":"internal system error during transfer execution"}`,
		); saveErr != nil {
			logger.WarnContext(ctx, "failed persisting 500 idempotency outcome", "error", saveErr)
		}

		return nil, http.StatusInternalServerError, fmt.Errorf("transfer execution failed: %w", err)
	}

	logger.InfoContext(ctx, "transfer completed and committed successfully",
		"transfer_id", transferID,
	)
	return &successResponse, http.StatusCreated, nil
}

func (s *transferService) computeRequestHash(fromID, toID string, amountMinor int64) string {
	raw := fmt.Sprintf("%s:%s:%d", fromID, toID, amountMinor)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// extractErrorMessage extracts the plain error message from a JSON payload like {"error":"..."},
// falling back to raw body if it is not formatted as JSON.
func extractErrorMessage(body string) string {
	var errPayload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &errPayload); err == nil && errPayload.Error != "" {
		return errPayload.Error
	}
	return body
}
