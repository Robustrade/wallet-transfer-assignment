package handler

import (
	"encoding/json"
	"net/http"

	"github.com/wallet-transfer-assignment/internal/model"
	"github.com/wallet-transfer-assignment/internal/service"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req model.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request format"}`, http.StatusBadRequest)
		return
	}

	if req.IdempotencyKey == "" || req.FromWalletID == "" || req.ToWalletID == "" || req.Amount <= 0 {
		http.Error(w, `{"error":"missing or invalid fields"}`, http.StatusBadRequest)
		return
	}

	if req.FromWalletID == req.ToWalletID {
		http.Error(w, `{"error":"cannot transfer to the same wallet"}`, http.StatusBadRequest)
		return
	}

	result := h.svc.ProcessTransfer(r.Context(), &req)

	if result.Error != nil && result.Response == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.HTTPStatus)
		msg := map[string]string{"error": result.Error.Error()}
		json.NewEncoder(w).Encode(msg)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.HTTPStatus)
	w.Write(result.Response)
}
