package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type walletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type queryHandler struct {
	transfers TransferReader
	wallets   WalletReader
	logger    *slog.Logger
}

// getTransfer handles GET /transfers/{id}.
func (h *queryHandler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, r, h.logger, badRequest("id: must be a UUID"))
		return
	}
	t, err := h.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransferResponse(t))
}

// getWallet handles GET /wallets/{id}.
func (h *queryHandler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, walletResponse{ID: wallet.ID, Balance: wallet.Balance, UpdatedAt: wallet.UpdatedAt.UTC()})
}
