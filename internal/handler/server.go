package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/service"
)

type Server struct {
	svc *service.Service
}

func New(svc *service.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", s.createTransfer)
	return mux
}

type createTransferBody struct {
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         float64 `json:"amount"`
}

type transferResponse struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	FailureReason  string `json:"failureReason,omitempty"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if body.Amount != float64(int64(body.Amount)) {
		writeError(w, http.StatusBadRequest, "invalid_request", domain.ErrInvalidAmount.Error())
		return
	}
	out, err := s.svc.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         int64(body.Amount),
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	status := http.StatusCreated
	if out.Replay {
		status = http.StatusOK
	}
	if out.Transfer.Status == domain.StatusFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, transferResponse{
		ID:             out.Transfer.ID,
		IdempotencyKey: out.Transfer.IdempotencyKey,
		FromWalletID:   out.Transfer.FromWalletID,
		ToWalletID:     out.Transfer.ToWalletID,
		Amount:         out.Transfer.Amount,
		Status:         string(out.Transfer.Status),
		FailureReason:  out.Transfer.FailureReason,
		CreatedAt:      out.Transfer.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      out.Transfer.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrMissingIdempotencyKey),
		errors.Is(err, domain.ErrMissingWalletID):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
