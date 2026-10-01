package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/RamG-Gupta/wallet-transfer-assignment/internal/domain"
	"github.com/RamG-Gupta/wallet-transfer-assignment/internal/service"
)

type Server struct {
	svc *service.Service
}

func New(svc *service.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /wallets", s.createWallet)
	mux.HandleFunc("GET /wallets/{id}", s.getWallet)
	mux.HandleFunc("POST /transfers", s.createTransfer)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createWalletBody struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

const maxJSONBody = 1 << 20

func (s *Server) createWallet(w http.ResponseWriter, r *http.Request) {
	var body createWalletBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	wallet, err := s.svc.CreateWallet(r.Context(), domain.CreateWalletRequest{
		ID:             body.ID,
		InitialBalance: body.InitialBalance,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, walletResponse(wallet))
}

func (s *Server) getWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wallet, err := s.svc.GetWallet(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, walletResponse(wallet))
}

type createTransferBody struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	tr, replay, err := s.svc.CreateTransfer(r.Context(), domain.CreateTransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	if tr.Status == domain.StatusFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, transferResponse(tr))
}

func walletResponse(w domain.Wallet) map[string]any {
	return map[string]any{
		"id":      w.ID,
		"balance": w.Balance,
	}
}

func transferResponse(tr domain.Transfer) map[string]any {
	ledger := make([]map[string]any, 0, len(tr.Ledger))
	for _, e := range tr.Ledger {
		ledger = append(ledger, map[string]any{
			"walletId": e.WalletID,
			"type":     e.Type,
			"amount":   e.Amount,
		})
	}
	var reason any
	if tr.FailureReason != "" {
		reason = tr.FailureReason
	}
	return map[string]any{
		"id":             tr.ID,
		"idempotencyKey": tr.IdempotencyKey,
		"fromWalletId":   tr.FromWalletID,
		"toWalletId":     tr.ToWalletID,
		"amount":         tr.Amount,
		"status":         tr.Status,
		"failureReason":  reason,
		"ledger":         ledger,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON value")
		}
		return fmt.Errorf("trailing data after JSON value: %w", err)
	}
	return nil
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrWalletExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
