package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"wallet-assignment/internal/service"
)

type Handler struct {
	svc *service.TransferService
}

func NewHandler(s *service.TransferService) *Handler {
	return &Handler{s}
}

func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {

	// Ensure correct method
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IdempotencyKey string `json:"idempotencyKey"`
		FromWalletId   string `json:"fromWalletId"`
		ToWalletId     string `json:"toWalletId"`
		Amount         int64  `json:"amount"`
	}

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid request",
		}); err != nil {
			log.Println("encode error:", err)
		}
		return
	}

	// Call service
	id, err := h.svc.CreateTransfer(
		r.Context(),
		req.IdempotencyKey,
		req.FromWalletId,
		req.ToWalletId,
		req.Amount,
	)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		}); err != nil {
			log.Println("encode error:", err)
		}
		return
	}

	// Success response
	if err := json.NewEncoder(w).Encode(map[string]string{
		"transferId": id,
	}); err != nil {
		log.Println("encode error:", err)
	}
}
