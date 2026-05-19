package transfer

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hariharandr/wallet-transfer-assignment/internal/apperr"
	"github.com/hariharandr/wallet-transfer-assignment/internal/httpapi"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/transfers", h.create)
}

type createRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in createRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	res, err := h.svc.Transfer(r.Context(), Request{
		IdempotencyKey: in.IdempotencyKey,
		FromWalletID:   in.FromWalletID,
		ToWalletID:     in.ToWalletID,
		Amount:         in.Amount,
	})
	if err != nil {
		// insufficient funds carries a real FAILED transfer, so
		// send the id back with the error.
		if errors.Is(err, apperr.ErrInsufficientFunds) {
			httpapi.WriteJSON(w, httpapi.StatusFor(err), httpapi.Response{
				Error:      "insufficient funds",
				TransferID: res.TransferID,
				Status:     res.State.String(),
			})
			return
		}
		httpapi.WriteError(w, httpapi.StatusFor(err), err.Error())
		return
	}

	httpapi.WriteJSON(w, http.StatusCreated, httpapi.Response{
		TransferID: res.TransferID,
		Status:     res.State.String(),
		FromWallet: res.From,
		ToWallet:   res.To,
		Amount:     res.Amount,
	})
}
