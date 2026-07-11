package main

import (
	"encoding/json"
	"net/http"
)

type httpHandler struct {
	svc *Service
}

func NewHandler(svc *Service) *httpHandler { return &httpHandler{svc: svc} }

func (h *httpHandler) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", h.createTransfer)
	return mux
}

type createTransferReq struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (h *httpHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req createTransferReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	res, err := h.svc.Transfer(r.Context(), TransferRequest{IdempotencyKey: req.IdempotencyKey, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount})
	if err != nil {
		if err == ErrInsufficientFunds {
			http.Error(w, "insufficient funds", http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}
