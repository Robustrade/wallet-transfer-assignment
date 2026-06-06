package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// Handler holds HTTP handler methods and their dependencies.
type Handler struct {
	svc TransferSvc
}

// New constructs a Handler.
func New(svc TransferSvc) *Handler {
	return &Handler{svc: svc}
}

// Register mounts all routes onto the supplied router.
func (h *Handler) Register(r chi.Router) {
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Post("/wallets", h.createWallet)
	r.Post("/transfers", h.createTransfer)
	r.Get("/transfers/{id}", h.getTransfer)
	r.Get("/wallets/{id}", h.getWallet)
	r.Post("/magic", h.magicDeposit)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// createWallet handles POST /wallets.
// No request body is required. A new wallet with a zero balance and a freshly
// generated UUID v7 is created and returned.
func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.svc.CreateWallet(r.Context())
	if err != nil {
		slog.Error("create wallet failed", "layer", "handler", "error", err)
		writeServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusCreated, WalletResponse{
		ID:      wallet.ID,
		Balance: wallet.Balance,
	})
}

// createTransfer handles POST /transfers.
func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if body.IdempotencyKey == "" {
		writeError(w, r, http.StatusBadRequest, "idempotencyKey is required")

		return
	}

	if len(body.IdempotencyKey) > service.MaxIdempotencyKeyLength {
		writeError(w, r, http.StatusBadRequest, "idempotencyKey exceeds maximum length")

		return
	}

	req := service.TransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	}

	resp, err := h.svc.Execute(r.Context(), req)
	if err != nil {
		slog.Error("create transfer failed", "layer", "handler", "error", err,
			"idempotencyKey", body.IdempotencyKey)
		writeServiceError(w, r, err)

		return
	}

	// Return 200 for idempotent replays, 201 for freshly created transfers.
	statusCode := http.StatusCreated
	if resp.FromCache {
		statusCode = http.StatusOK
	}

	writeJSON(w, statusCode, CreateTransferResponse{
		ID:     resp.TransferID,
		Status: resp.Status,
		Amount: resp.Amount,
	})
}

// getTransfer handles GET /transfers/{id}.
func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid transfer ID")

		return
	}

	t, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		slog.Error("get transfer failed", "layer", "handler", "error", err, "transferID", id)
		writeServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, TransferResponse{
		ID:     t.ID,
		Status: t.Status,
		Amount: t.Amount,
	})
}

// getWallet handles GET /wallets/{id}.
func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid wallet ID")

		return
	}

	wallet, err := h.svc.GetWallet(r.Context(), id)
	if err != nil {
		slog.Error("get wallet failed", "layer", "handler", "error", err, "walletID", id)
		writeServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, WalletResponse{
		ID:      wallet.ID,
		Balance: wallet.Balance,
	})
}

// magicDeposit handles POST /magic.
// It deposits a random amount between 100 and 10 000 cents from the nature
// wallet into the specified destination wallet. The response is identical to
// that of POST /transfers, and the endpoint is idempotent via idempotencyKey.
func (h *Handler) magicDeposit(w http.ResponseWriter, r *http.Request) {
	var body MagicDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if body.IdempotencyKey == "" {
		writeError(w, r, http.StatusBadRequest, "idempotencyKey is required")

		return
	}

	if len(body.IdempotencyKey) > service.MaxIdempotencyKeyLength {
		writeError(w, r, http.StatusBadRequest, "idempotencyKey exceeds maximum length")

		return
	}

	if body.ToWalletID == (uuid.UUID{}) {
		writeError(w, r, http.StatusBadRequest, "toWalletId is required")

		return
	}

	resp, err := h.svc.Magic(r.Context(), service.MagicRequest{
		IdempotencyKey: body.IdempotencyKey,
		ToWalletID:     body.ToWalletID,
	})
	if err != nil {
		slog.Error("magic deposit failed", "layer", "handler", "error", err,
			"idempotencyKey", body.IdempotencyKey)
		writeServiceError(w, r, err)

		return
	}

	// Return 200 for idempotent replays, 201 for freshly executed deposits.
	statusCode := http.StatusCreated
	if resp.FromCache {
		statusCode = http.StatusOK
	}

	writeJSON(w, statusCode, TransferResponse{
		ID:     resp.TransferID,
		Status: resp.Status,
		Amount: resp.Amount,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, r, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		writeError(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrDuplicateIdempotencyKey):
		writeError(w, r, http.StatusConflict, err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response failed", "layer", "handler", "error", err)
	}
}

// writeError writes log entry and builds JSON error response including the request ID for traceability.
func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	requestID := middleware.GetReqID(r.Context())
	slog.Error("request failed", "layer", "handler", "error", msg, "requestID", requestID)

	writeJSON(w, status, errorResponse{
		RequestID: requestID,
		Error:     msg,
	})
}
