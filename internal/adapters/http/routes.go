package httpadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"wallet-transfer-assignment/internal/platform/httputil"
	"wallet-transfer-assignment/internal/transfers"
	"wallet-transfer-assignment/internal/wallets"
)

type Deps struct {
	Transfers transfers.Service
	Wallets   *wallets.Service
}

func RegisterRoutes(r chi.Router, deps Deps) {
	h := &handler{
		transfers: deps.Transfers,
		wallets:   deps.Wallets,
	}

	r.Route("/wallets", func(r chi.Router) {
		r.Post("/", h.createWallet)
		r.Get("/{walletId}", h.getWallet)
	})

	r.Route("/transfers", func(r chi.Router) {
		r.Post("/", h.createTransfer)
	})
}

type handler struct {
	transfers transfers.Service
	wallets   *wallets.Service
}

type createWalletRequest struct {
	InitialBalance *int64 `json:"initialBalance"`
}

type walletResponse struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
}

func (h *handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	initial := int64(0)
	if req.InitialBalance != nil {
		initial = *req.InitialBalance
	}
	if initial < 0 {
		httputil.WriteError(w, http.StatusBadRequest, "initialBalance must be >= 0")
		return
	}

	wal, err := h.wallets.Create(r.Context(), initial)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusCreated, walletResponse{WalletID: wal.ID, Balance: wal.Balance})
}

func (h *handler) getWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "walletId")
	wal, err := h.wallets.Get(r.Context(), id)
	if errors.Is(err, wallets.ErrNotFound) {
		httputil.WriteError(w, http.StatusNotFound, "wallet not found")
		return
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, walletResponse{WalletID: wal.ID, Balance: wal.Balance})
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (h *handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	status, body, err := h.transfers.Create(r.Context(), transfers.CreateRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})

	if body != nil {
		_ = httputil.WriteJSONBytes(w, status, body)
		return
	}

	httputil.WriteError(w, status, transferErrMessage(r, err))
}

func transferErrMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, transfers.ErrInvalidRequest):
		return "invalid request"
	case errors.Is(err, transfers.ErrWalletNotFound):
		return "wallet not found"
	case errors.Is(err, transfers.ErrInsufficientFunds):
		return "insufficient funds"
	case errors.Is(err, transfers.ErrIdempotencyConflict):
		return "idempotency key reused with different payload"
	default:
		// Log the full error server-side; never surface it externally.
		fmt.Printf("unexpected transfer error (requestId=%s): %v",
			r.Header.Get("X-Request-Id"), err)
		return "internal error"
	}
}

func decodeJSON(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
