package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"github.com/manish8561/wallet-transfer-assignment/internal/services"
)

// TransferService defines the interface for the transfer service
type TransferService interface {
	ExecuteTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (*services.TransferResponse, error)
	GetBalance(ctx context.Context, walletID string) (int64, error)
	GetTransferHistory(ctx context.Context, walletID string, limit, offset int) ([]*models.Transfer, error)
	CreateWallet(ctx context.Context, initialBalance int64) (*models.Wallet, error)
}

// TransferHandler handles transfer HTTP requests
type TransferHandler struct {
	service TransferService
}

// NewTransferHandler creates a new transfer handler
func NewTransferHandler(service TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

// CreateTransferRequest represents the request to create a transfer
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// CreateWalletRequest represents the request to create a wallet
type CreateWalletRequest struct {
	InitialBalance int64 `json:"initialBalance"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid request body"})
		return
	}

	// Validate required fields
	if req.IdempotencyKey == "" || req.FromWalletID == "" || req.ToWalletID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "missing required fields"})
		return
	}

	// Execute transfer
	response, err := h.service.ExecuteTransfer(r.Context(), req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")

		// Determine HTTP status based on error type
		statusCode := http.StatusInternalServerError
		errMsg := err.Error()
		if strings.Contains(errMsg, "insufficient balance") ||
			strings.Contains(errMsg, "from_wallet not found") ||
			strings.Contains(errMsg, "to_wallet not found") {
			statusCode = http.StatusUnprocessableEntity
		}
		if strings.Contains(errMsg, "validation error") {
			statusCode = http.StatusBadRequest
		}

		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	// Return successful response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetBalance handles GET /wallets/{wallet_id}/balance
func (h *TransferHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("wallet_id")
	if walletID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "wallet_id is required"})
		return
	}

	balance, err := h.service.GetBalance(r.Context(), walletID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"wallet_id": walletID,
		"balance":   balance,
	})
}

// CreateWallet handles POST /wallets
func (h *TransferHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid request body"})
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), req.InitialBalance)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	// Return successful response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(wallet)
}
