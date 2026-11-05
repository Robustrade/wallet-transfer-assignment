package handler

import (
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type WalletHandler struct {
	walletService service.WalletService
}

func NewWalletHandler(walletService service.WalletService) *WalletHandler {
	return &WalletHandler{walletService: walletService}
}

func (h *WalletHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /wallets", h.CreateWallet)
	mux.HandleFunc("GET /wallets/{walletId}", h.GetWallet)
	mux.HandleFunc("POST /wallets/{walletId}/funds", h.AddFunds)
	mux.HandleFunc("GET /wallets/{walletId}/ledger", h.GetLedger)
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var request CreateWalletRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	wallet, err := h.walletService.CreateWallet(r.Context(), service.CreateWalletInput{
		ID: request.WalletID, Balance: request.InitialBalance, Status: domain.WalletStatus(request.Status),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.walletService.GetWallet(r.Context(), r.PathValue("walletId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

func (h *WalletHandler) AddFunds(w http.ResponseWriter, r *http.Request) {
	var request AddFundsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	wallet, err := h.walletService.AddFunds(r.Context(), r.PathValue("walletId"), request.Amount)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

func (h *WalletHandler) GetLedger(w http.ResponseWriter, r *http.Request) {
	entries, err := h.walletService.GetLedger(r.Context(), r.PathValue("walletId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	response := make([]LedgerEntryResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, LedgerEntryResponse{
			ID: entry.ID, WalletID: entry.WalletID, TransferID: entry.TransferID,
			Type: string(entry.Type), Amount: entry.Amount,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func toWalletResponse(wallet *domain.Wallet) WalletResponse {
	return WalletResponse{
		WalletID: wallet.ID, Balance: wallet.Balance, Status: string(wallet.Status),
		CreatedAt: wallet.CreatedAt, UpdatedAt: wallet.UpdatedAt,
	}
}
