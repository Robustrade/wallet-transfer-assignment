package httpapi

import (
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

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

func (s *Server) createWallet(w http.ResponseWriter, r *http.Request) {
	var body createWalletRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: errorDetail{Code: "INVALID_REQUEST", Message: err.Error()}})
		return
	}

	wallet, err := s.service.CreateWallet(r.Context(), body.ID, body.Balance)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newWalletResponse(wallet))
}

func (s *Server) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := s.service.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newWalletResponse(wallet))
}

func newWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{ID: w.ID, Balance: w.Balance, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}
