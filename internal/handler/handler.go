// Package handler is the HTTP transport layer: it decodes and validates the
// wire format, calls the services and maps results and errors to status codes.
// It contains no business rules.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
)

const (
	maxBodyBytes = 1 << 20
	// IdempotencyKeyHeader is accepted as an alternative to the idempotencyKey
	// body field.
	IdempotencyKeyHeader = "Idempotency-Key"
	// ReplayedHeader is set on responses that replay an earlier result.
	ReplayedHeader = "Idempotent-Replayed"
)

// TransferService is the transfer behaviour the handlers depend on.
type TransferService interface {
	CreateTransfer(ctx context.Context, in service.CreateTransferInput) (*service.TransferResult, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (*service.TransferDetails, error)
}

// WalletService is the wallet behaviour the handlers depend on.
type WalletService interface {
	CreateWallet(ctx context.Context, id string, openingBalance int64) (*domain.Wallet, error)
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
}

// Handler serves the HTTP API.
type Handler struct {
	transfers TransferService
	wallets   WalletService
	log       *slog.Logger
}

// New returns the API as an http.Handler, wrapped with request logging and
// panic recovery.
func New(transfers TransferService, wallets WalletService, log *slog.Logger) http.Handler {
	h := &Handler{transfers: transfers, wallets: wallets, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /transfers/{id}", h.getTransfer)
	mux.HandleFunc("POST /wallets", h.createWallet)
	mux.HandleFunc("GET /wallets/{id}", h.getWallet)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return h.middleware(mux)
}

// --- wire types -------------------------------------------------------------

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	ID             string              `json:"id"`
	IdempotencyKey string              `json:"idempotencyKey"`
	FromWalletID   string              `json:"fromWalletId"`
	ToWalletID     string              `json:"toWalletId"`
	Amount         int64               `json:"amount"`
	Status         string              `json:"status"`
	FailureReason  string              `json:"failureReason,omitempty"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
	LedgerEntries  []ledgerEntryResult `json:"ledgerEntries,omitempty"`
	Error          *errorBody          `json:"error,omitempty"`
}

type ledgerEntryResult struct {
	ID       int64  `json:"id"`
	WalletID string `json:"walletId"`
	Type     string `json:"type"`
	Amount   int64  `json:"amount"`
}

type createWalletRequest struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

type walletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func toTransferResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		ID:             t.ID.String(),
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		FailureReason:  string(t.FailureReason),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

// --- transfers --------------------------------------------------------------

func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// The body field is the contract; the header is an accepted alternative.
	key := req.IdempotencyKey
	if hdr := r.Header.Get(IdempotencyKeyHeader); hdr != "" {
		if key != "" && key != hdr {
			writeError(w, http.StatusBadRequest, "invalid_request", "idempotencyKey and Idempotency-Key header disagree")
			return
		}
		key = hdr
	}

	res, err := h.transfers.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: key,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	body := toTransferResponse(res.Transfer)
	status := http.StatusCreated
	switch {
	case res.Transfer.Status == domain.TransferFailed:
		status = http.StatusUnprocessableEntity
		body.Error = failureBody(res.Transfer.FailureReason)
	case res.Replayed:
		status = http.StatusOK
	}
	if res.Replayed {
		w.Header().Set(ReplayedHeader, "true")
	}
	h.log.InfoContext(r.Context(), "transfer handled",
		"transferId", res.Transfer.ID, "status", res.Transfer.Status,
		"failureReason", res.Transfer.FailureReason, "replayed", res.Replayed)
	writeJSON(w, status, body)
}

func failureBody(reason domain.FailureReason) *errorBody {
	switch reason {
	case domain.FailureInsufficientFunds:
		return &errorBody{Code: "insufficient_funds", Message: "source wallet has insufficient funds"}
	case domain.FailureBalanceOverflow:
		return &errorBody{Code: "balance_overflow", Message: "destination balance would overflow"}
	default:
		return &errorBody{Code: "transfer_failed", Message: string(reason)}
	}
}

func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "transfer id must be a UUID")
		return
	}
	details, err := h.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	body := toTransferResponse(details.Transfer)
	for _, e := range details.Entries {
		body.LedgerEntries = append(body.LedgerEntries, ledgerEntryResult{
			ID: e.ID, WalletID: e.WalletID, Type: string(e.Type), Amount: e.Amount,
		})
	}
	writeJSON(w, http.StatusOK, body)
}

// --- wallets ----------------------------------------------------------------

func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	wallet, err := h.wallets.CreateWallet(r.Context(), req.ID, req.Balance)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}

func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

func toWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{ID: w.ID, Balance: w.Balance, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}

// --- helpers ----------------------------------------------------------------

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "wallet_not_found", "wallet not found")
	case errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, http.StatusNotFound, "transfer_not_found", "transfer not found")
	case errors.Is(err, domain.ErrWalletExists):
		writeError(w, http.StatusConflict, "wallet_exists", "wallet already exists")
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		writeError(w, http.StatusUnprocessableEntity, "idempotency_key_reused", err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusServiceUnavailable, "unavailable", "request was cancelled or timed out; retry with the same idempotency key")
	default:
		h.log.ErrorContext(r.Context(), "unhandled error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

// decodeJSON reads exactly one JSON object, rejecting unknown fields, invalid
// UTF-8 and oversized bodies. It writes the error response itself and reports
// success.
//
// The raw body is checked for valid UTF-8 first because encoding/json silently
// replaces invalid bytes with U+FFFD, which would let distinct malformed keys or
// wallet ids collapse into one identity.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large",
				fmt.Sprintf("request body must be at most %d bytes", maxBodyBytes))
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read request body")
		return false
	}
	if !utf8.Valid(raw) {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid UTF-8")
		return false
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("malformed JSON body: %v", err))
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "body must contain a single JSON object")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
