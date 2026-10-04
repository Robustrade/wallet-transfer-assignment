package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/domain"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []fieldError `json:"details,omitempty"`
}

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type walletResponse struct {
	ID             string    `json:"id"`
	Balance        int64     `json:"balance"`
	InitialBalance int64     `json:"initialBalance"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type transferResponse struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         string    `json:"status"`
	FailureReason  string    `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type ledgerResponse struct {
	TransferID string                `json:"transferId"`
	Entries    []ledgerEntryResponse `json:"entries"`
}

type ledgerEntryResponse struct {
	ID         string    `json:"id"`
	WalletID   string    `json:"walletId"`
	TransferID string    `json:"transferId"`
	Type       string    `json:"type"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

func newWalletResponse(wallet domain.Wallet) walletResponse {
	return walletResponse{
		ID:             wallet.ID,
		Balance:        wallet.Balance,
		InitialBalance: wallet.InitialBalance,
		CreatedAt:      wallet.CreatedAt,
		UpdatedAt:      wallet.UpdatedAt,
	}
}

func newTransferResponse(transfer domain.Transfer) transferResponse {
	return transferResponse{
		ID:             transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         string(transfer.Status),
		FailureReason:  transfer.FailureReason,
		CreatedAt:      transfer.CreatedAt,
		UpdatedAt:      transfer.UpdatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, details []fieldError) {
	writeJSON(w, status, errorBody{Error: errorPayload{Code: code, Message: message, Details: details}})
}

func mapClientError(err error) (status int, code, message string, details []fieldError, ok bool) {
	var media unsupportedMediaError
	if errors.As(err, &media) {
		return http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", media.Error(), nil, true
	}
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		details = make([]fieldError, len(validation.Details))
		for i, detail := range validation.Details {
			details[i] = fieldError{Field: detail.Field, Message: detail.Message}
		}
		return http.StatusBadRequest, domain.CodeValidation, "request validation failed", details, true
	}
	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		switch notFound.Entity {
		case "wallet":
			return http.StatusNotFound, domain.CodeWalletNotFound, notFound.Error(), nil, true
		case "transfer":
			return http.StatusNotFound, domain.CodeTransferNotFound, notFound.Error(), nil, true
		default:
			return http.StatusNotFound, "NOT_FOUND", notFound.Error(), nil, true
		}
	}
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		if conflict.Entity == "wallet" {
			return http.StatusConflict, domain.CodeWalletExists, conflict.Error(), nil, true
		}
		return http.StatusConflict, "CONFLICT", conflict.Error(), nil, true
	}
	switch {
	case errors.Is(err, domain.ErrIdempotencyMismatch):
		return http.StatusConflict, domain.CodeIdempotencyReused, "idempotency key was already used with a different request", nil, true
	case errors.Is(err, domain.ErrAmountOverflow):
		return http.StatusUnprocessableEntity, domain.CodeAmountOverflow, "credit would overflow the destination balance", nil, true
	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, domain.CodeInsufficientFunds, "source wallet has insufficient balance", nil, true
	case errors.Is(err, domain.ErrBusy):
		return http.StatusServiceUnavailable, domain.CodeBusy, "the service is busy, retry the request", nil, true
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return http.StatusServiceUnavailable, "TIMEOUT", "the request timed out, retry the request", nil, true
	default:
		return 0, "", "", nil, false
	}
}
