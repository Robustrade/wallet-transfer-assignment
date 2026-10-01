package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/models"
	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/service"
)

const (
	maxBodyBytes = 1 << 20 // 1 MiB
	maxKeyLength = 255
)

// TransferCreator is what the handler needs from the service layer. Depending
// on an interface keeps the handler testable without a database.
type TransferCreator interface {
	CreateTransfer(ctx context.Context, in service.TransferInput) (*service.TransferResult, error)
}

type TransferHandler struct {
	svc    TransferCreator
	logger *slog.Logger
}

// NewTransferHandler builds the handler. A nil logger falls back to slog's
// default logger.
func NewTransferHandler(svc TransferCreator, logger *slog.Logger) *TransferHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TransferHandler{svc: svc, logger: logger}
}

// Register mounts the routes on mux. Method-qualified patterns need go.mod to
// declare go 1.22 or newer; other methods on /transfers get an automatic 405.
func (h *TransferHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.CreateTransfer)
}

// ---------------------------------------------------------------------------
// Wire types (the transport contract; deliberately separate from models)
// ---------------------------------------------------------------------------

type createTransferRequest struct {
	IdempotencyKey string          `json:"idempotencyKey"`
	FromWalletID   string          `json:"fromWalletId"`
	ToWalletID     string          `json:"toWalletId"`
	Amount         decimal.Decimal `json:"amount"`
}

type transferResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// POST /transfers
// ---------------------------------------------------------------------------

// CreateTransfer parses and shape-checks the request, calls the service, and
// maps the outcome to an HTTP response. Business rules live in the service.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	in, err := parseCreateTransfer(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	res, err := h.svc.CreateTransfer(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	status := http.StatusCreated
	if res.Replayed {
		// Same body as the original response, flagged so clients can tell.
		status = http.StatusOK
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, status, transferResponse{
		TransferID: res.TransferID.String(),
		Status:     string(res.Status),
	})
}

// parseCreateTransfer decodes the body and checks its shape only: valid JSON,
// no unknown fields, a usable key, parseable UUIDs. It does not judge the
// amount; a missing or bad amount is rejected by the service.
func parseCreateTransfer(w http.ResponseWriter, r *http.Request) (service.TransferInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createTransferRequest
	if err := dec.Decode(&req); err != nil {
		return service.TransferInput{}, fmt.Errorf("invalid JSON body: %v", err)
	}
	// Reject trailing content after the JSON object.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return service.TransferInput{}, errors.New("invalid JSON body: unexpected data after object")
	}

	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return service.TransferInput{}, errors.New("idempotencyKey is required")
	}
	if len(req.IdempotencyKey) > maxKeyLength {
		return service.TransferInput{}, fmt.Errorf("idempotencyKey must be at most %d characters", maxKeyLength)
	}

	from, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		return service.TransferInput{}, errors.New("fromWalletId must be a valid UUID")
	}
	to, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		return service.TransferInput{}, errors.New("toWalletId must be a valid UUID")
	}

	return service.TransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         req.Amount,
	}, nil
}

// writeServiceError translates service errors into HTTP responses. Messages
// are fixed strings per error; internal error text is never sent to clients.
func (h *TransferHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrSameWallet):
		writeError(w, http.StatusBadRequest, "SAME_WALLET", "sender and receiver must be different wallets")
	case errors.Is(err, models.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "INVALID_AMOUNT", "amount must be positive, have at most two decimal places, and not exceed the maximum")
	case errors.Is(err, models.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "one or both wallets do not exist")
	case errors.Is(err, models.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS", "sender has insufficient funds")
	case errors.Is(err, models.ErrKeyMismatch):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "this idempotency key was already used with different parameters")
	case errors.Is(err, models.ErrInProgress):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusConflict, "REQUEST_IN_PROGRESS", "a request with this idempotency key is still being processed; retry shortly")
	default:
		h.logger.Error("create transfer failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
