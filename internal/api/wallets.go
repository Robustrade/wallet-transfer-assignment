package api

import (
	"net/http"
	"strconv"

	"wallet/internal/domain"
	"wallet/internal/dto"
)

// getWallet handles canonical GET /v1/wallets/{id} and the legacy
// GET /wallets/{id} alias.
func (h *handler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.svc.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.WalletResponse{
		WalletID: wallet.ID,
		Balance:  wallet.Balance,
		Currency: wallet.Currency,
	})
}

// listLedger handles canonical GET /v1/wallets/{id}/ledger?limit=N and the
// legacy GET /wallets/{id}/ledger alias. An absent limit selects the default
// page size; an explicit limit must be a positive integer, and an oversized one
// is clamped by the service.
func (h *handler) listLedger(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, r, h.logger, domain.NewValidationError("limit must be an integer"))
			return
		}
		if n <= 0 {
			writeError(w, r, h.logger, domain.NewValidationError("limit must be a positive integer"))
			return
		}
		limit = n
	}

	entries, err := h.svc.ListLedger(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	resp := make([]dto.LedgerEntryResponse, len(entries))
	for i, e := range entries {
		resp[i] = dto.LedgerEntryResponse{
			EntryID:    e.ID,
			TransferID: e.TransferID,
			WalletID:   e.WalletID,
			Type:       string(e.EntryType),
			Amount:     e.Amount,
			CreatedAt:  dto.FormatTime(e.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": resp})
}
