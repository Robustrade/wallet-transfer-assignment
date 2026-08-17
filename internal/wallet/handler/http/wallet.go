package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/domain"
	"github.com/raknay/wallet-transfer-assignment/internal/wallet/service"
)

type WalletHandler struct {
	service service.WalletService
}

func NewWalletHandler(router *chi.Mux, service service.WalletService) {
	h := WalletHandler{
		service: service,
	}
	router.Post("/wallet/transfer", h.CreateTransfer)
}

func (wl *WalletHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var input domain.Transfer

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	transfer, err := wl.service.CreateTransfer(r.Context(), input)
	if err != nil {
		errMap := map[string]string{"error": err.Error()}
		response(w, http.StatusBadRequest, errMap)
		return
	}

	response(w, http.StatusCreated, transfer)
}

func response(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
