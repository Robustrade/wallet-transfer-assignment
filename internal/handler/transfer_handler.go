package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"

	"github.com/google/uuid"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{service: svc}
}

type createTransferRequestBody struct {
	IdempotencyKey string      `json:"idempotencyKey"`
	FromWalletID   string      `json:"fromWalletId"`
	ToWalletID     string      `json:"toWalletId"`
	Amount         json.Number `json:"amount"`
}

type transferResponseBody struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
}

type errorResponseBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponseBody{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequestBody
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber() // avoid decoding amount into float64; keep it as an exact decimal string
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return
	}

	fromID, err := uuid.Parse(body.FromWalletID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "fromWalletId must be a valid UUID")
		return
	}
	toID, err := uuid.Parse(body.ToWalletID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "toWalletId must be a valid UUID")
		return
	}

	// Amount arrives as a JSON number of whole currency units (e.g. 100 or
	// 100.50). json.Number preserves the original decimal text exactly, so
	// this parses straight to integer cents with no float64 involved at all.
	amountCents, err := domain.NumericToCents(body.Amount.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "amount must be a positive number with at most 2 decimal places")
		return
	}

	req := domain.CreateTransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         amountCents,
	}

	result, err := h.service.CreateTransfer(r.Context(), req)
	if err != nil {
		mapDomainError(w, err)
		return
	}

	if result.ConflictReason != "" {
		writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", result.ConflictReason)
		return
	}

	// Whether this is a fresh execution or a replay, the response reflects
	// the transfer's actual outcome — replays return the exact same status
	// the original request received, so retries are indistinguishable from
	// the original response (see docs/design.md for the alternative considered).
	if result.Transfer.Status == domain.StatusFailed {
		writeError(w, http.StatusConflict, "INSUFFICIENT_FUNDS", "insufficient wallet balance")
		return
	}

	writeJSON(w, http.StatusCreated, transferResponseBody{
		TransferID: result.Transfer.ID.String(),
		Status:     string(result.Transfer.Status),
	})
}

func mapDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred")
	}
}
