package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferHandler serves /transfers.
type TransferHandler struct {
	svc *service.TransferService
	log *slog.Logger
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// statusFor maps the transfer outcome to HTTP. A replay maps the stored result the same
// way, so it gets the same status code as the first call.
func statusFor(t domain.Transfer) int {
	if t.Status == domain.StatusProcessed {
		return http.StatusCreated
	}
	return http.StatusUnprocessableEntity // FAILED, e.g. insufficient funds
}

// Create handles POST /transfers.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, h.log, err)
		return
	}

	res, err := h.svc.CreateTransfer(r.Context(), domain.TransferCommand{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}

	w.Header().Set("Idempotent-Replayed", strconv.FormatBool(res.Replayed))
	writeJSON(w, statusFor(res.Transfer), res.Transfer)
}

// Get handles GET /transfers/{id}.
func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.GetTransfer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
