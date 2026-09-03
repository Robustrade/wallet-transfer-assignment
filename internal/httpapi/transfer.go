package httpapi

import (
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	ID            string                `json:"id"`
	FromWalletID  string                `json:"fromWalletId"`
	ToWalletID    string                `json:"toWalletId"`
	Amount        int64                 `json:"amount"`
	Status        domain.TransferStatus `json:"status"`
	FailureReason string                `json:"failureReason,omitempty"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	Entries       []ledgerEntryResponse `json:"entries"`
}

type ledgerEntryResponse struct {
	ID        string           `json:"id"`
	WalletID  string           `json:"walletId"`
	Type      domain.EntryType `json:"type"`
	Amount    int64            `json:"amount"`
	CreatedAt time.Time        `json:"createdAt"`
}

func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: errorDetail{Code: "INVALID_REQUEST", Message: err.Error()}})
		return
	}

	result, err := s.service.CreateTransfer(r.Context(), domain.TransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	if result.Replayed {
		w.Header().Set("Idempotent-Replay", "true")
	}

	if result.Transfer.Status == domain.StatusFailed {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody{Error: errorDetail{
			Code:       result.Transfer.FailureReason,
			Message:    "transfer could not be settled",
			TransferID: result.Transfer.ID,
		}})
		return
	}
	writeJSON(w, http.StatusCreated, newTransferResponse(result))
}

func (s *Server) getTransfer(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.GetTransfer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransferResponse(result))
}

func newTransferResponse(result *service.TransferResult) transferResponse {
	entries := make([]ledgerEntryResponse, 0, len(result.Entries))
	for _, e := range result.Entries {
		entries = append(entries, ledgerEntryResponse{
			ID: e.ID, WalletID: e.WalletID, Type: e.EntryType, Amount: e.Amount, CreatedAt: e.CreatedAt,
		})
	}

	t := result.Transfer
	return transferResponse{
		ID:            t.ID,
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		Status:        t.Status,
		FailureReason: t.FailureReason,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
		Entries:       entries,
	}
}
