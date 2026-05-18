package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type Server struct {
	service *Service
	mux     *http.ServeMux
}

func NewServer(service *Service) *Server {
	mux := http.NewServeMux()
	server := &Server{service: service, mux: mux}
	mux.HandleFunc("/transfers", server.handleTransfers)
	mux.HandleFunc("/wallets", server.handleWallets)
	mux.HandleFunc("/wallets/", server.handleWalletByID)
	return server
}

func (s *Server) Router() http.Handler {
	return s.mux
}

func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := s.service.CreateTransfer(r.Context(), &req)
	if err != nil {
		s.writeTransferError(w, err, result)
		return
	}

	response := transferResponse(result.Transfer)
	if result.Existing {
		if result.Transfer.State == StateFailed {
			s.writeJSON(w, http.StatusBadRequest, response)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}

	if result.Transfer.State == StateProcessed {
		s.writeJSON(w, http.StatusCreated, response)
		return
	}
	s.writeJSON(w, http.StatusBadRequest, response)
}

func (s *Server) writeTransferError(w http.ResponseWriter, err error, result *TransferResult) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrWalletNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrInsufficientFunds):
		if result != nil && result.Transfer != nil {
			s.writeJSON(w, http.StatusBadRequest, transferResponse(result.Transfer))
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) handleWallets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	wallet, err := s.service.CreateWallet(r.Context(), &req)
	if err != nil {
		if errors.Is(err, ErrInvalidRequest) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, http.StatusCreated, WalletResponse{ID: wallet.ID, Balance: wallet.Balance})
}

func (s *Server) handleWalletByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	walletID := strings.TrimPrefix(r.URL.Path, "/wallets/")
	if walletID == "" {
		http.Error(w, "wallet id required", http.StatusBadRequest)
		return
	}
	wallet, err := s.service.GetWallet(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, ErrWalletNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, http.StatusOK, WalletResponse{ID: wallet.ID, Balance: wallet.Balance})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func transferResponse(t *Transfer) TransferResponse {
	return TransferResponse{
		TransferID:     t.ID,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		State:          string(t.State),
		IdempotencyKey: t.IdempotencyKey,
		CreatedAt:      t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      t.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
