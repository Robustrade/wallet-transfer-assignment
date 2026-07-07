package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/service"
)

const maxRequestBodyBytes = 1 << 20

type TransferUseCase interface {
	CreateTransfer(context.Context, service.TransferCommand) (*service.TransferResult, error)
}

type Handler struct {
	transfers TransferUseCase
	logger    *slog.Logger
}

func NewHandler(transfers TransferUseCase, logger *slog.Logger) *Handler {
	return &Handler{transfers: transfers, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /healthz", h.health)
	return h.logging(mux)
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	TransferID     string                `json:"transferId"`
	IdempotencyKey string                `json:"idempotencyKey"`
	FromWalletID   string                `json:"fromWalletId"`
	ToWalletID     string                `json:"toWalletId"`
	Amount         int64                 `json:"amount"`
	Status         domain.TransferStatus `json:"status"`
	FailureCode    domain.FailureCode    `json:"failureCode,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *Handler) createTransfer(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var payload createTransferRequest
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{
			Code: "INVALID_JSON", Message: "request body must be valid JSON",
		})
		return
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{
			Code: "INVALID_JSON", Message: "request body must contain one JSON object",
		})
		return
	}

	result, err := h.transfers.CreateTransfer(request.Context(), service.TransferCommand{
		IdempotencyKey: payload.IdempotencyKey,
		FromWalletID:   payload.FromWalletID,
		ToWalletID:     payload.ToWalletID,
		Amount:         payload.Amount,
	})
	if err != nil {
		h.writeServiceError(writer, err)
		return
	}

	statusCode := http.StatusCreated
	switch result.Transfer.FailureCode {
	case domain.FailureCodeInsufficientBalance:
		statusCode = http.StatusUnprocessableEntity
	case domain.FailureCodeWalletNotFound:
		statusCode = http.StatusNotFound
	}

	transfer := result.Transfer
	writeJSON(writer, statusCode, transferResponse{
		TransferID:     transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         transfer.Status,
		FailureCode:    transfer.FailureCode,
		CreatedAt:      transfer.CreatedAt,
		UpdatedAt:      transfer.UpdatedAt,
	})
}

func (h *Handler) writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeJSON(writer, http.StatusConflict, errorResponse{
			Code:    "IDEMPOTENCY_CONFLICT",
			Message: domain.ErrIdempotencyConflict.Error(),
		})
	case errors.Is(err, domain.ErrInvalidIdempotencyKey),
		errors.Is(err, domain.ErrInvalidWalletID),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount):
		writeJSON(writer, http.StatusBadRequest, errorResponse{
			Code:    "VALIDATION_ERROR",
			Message: err.Error(),
		})
	default:
		h.logger.Error("create transfer failed", "error", err)
		writeJSON(writer, http.StatusInternalServerError, errorResponse{
			Code:    "INTERNAL_ERROR",
			Message: "an internal error occurred",
		})
	}
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(writer, request)
		h.logger.Info("request completed",
			"method", request.Method,
			"path", request.URL.Path,
			"duration", time.Since(started),
		)
	})
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}
