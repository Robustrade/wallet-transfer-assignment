package handlers

import (
	"time"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	FromWalletID   string `json:"fromWalletId"   binding:"required"`
	ToWalletID     string `json:"toWalletId"     binding:"required"`
	Amount         *int64 `json:"amount"         binding:"required"`
}

func (r createTransferRequest) toDomain() domain.TransferRequest {
	var amount domain.Amount
	if r.Amount != nil {
		amount = domain.Amount(*r.Amount)
	}
	return domain.TransferRequest{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         amount,
	}
}

type transferResponse struct {
	TransferID   string    `json:"transferId"`
	Status       string    `json:"status"`
	FromWalletID string    `json:"fromWalletId"`
	ToWalletID   string    `json:"toWalletId"`
	Amount       int64     `json:"amount"`
	Currency     string    `json:"currency"`
	CreatedAt    time.Time `json:"createdAt"`
}

func transferResponseFromResult(r service.TransferResult) transferResponse {
	return transferResponse{
		TransferID:   r.TransferID.String(),
		Status:       string(r.Status),
		FromWalletID: r.FromWalletID,
		ToWalletID:   r.ToWalletID,
		Amount:       int64(r.Amount),
		Currency:     r.Currency,
		CreatedAt:    r.CreatedAt,
	}
}

func transferResponseFromTransfer(t domain.Transfer) transferResponse {
	return transferResponse{
		TransferID:   t.ID.String(),
		Status:       string(t.Status),
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       int64(t.Amount),
		Currency:     t.Currency,
		CreatedAt:    t.CreatedAt,
	}
}

type ledgerEntryResponse struct {
	EntryID   int64     `json:"entryId"`
	WalletID  string    `json:"walletId"`
	Type      string    `json:"type"`
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
}

type transferDetailResponse struct {
	transferResponse
	FailureReason *string               `json:"failureReason,omitempty"`
	LedgerEntries []ledgerEntryResponse `json:"ledgerEntries"`
}

func newTransferDetailResponse(d service.TransferDetail) transferDetailResponse {
	entries := make([]ledgerEntryResponse, 0, len(d.Entries))
	for _, e := range d.Entries {
		entries = append(entries, ledgerEntryResponse{
			EntryID:   e.ID,
			WalletID:  e.WalletID,
			Type:      string(e.Type),
			Amount:    int64(e.Amount),
			Currency:  e.Currency,
			CreatedAt: e.CreatedAt,
		})
	}

	return transferDetailResponse{
		transferResponse: transferResponseFromTransfer(d.Transfer),
		FailureReason:    d.Transfer.FailureReason,
		LedgerEntries:    entries,
	}
}

type walletResponse struct {
	WalletID  string    `json:"walletId"`
	Currency  string    `json:"currency"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func newWalletResponse(w domain.Wallet) walletResponse {
	return walletResponse{
		WalletID:  w.ID,
		Currency:  w.Currency,
		Balance:   int64(w.Balance),
		UpdatedAt: w.UpdatedAt,
	}
}

type historyResponse struct {
	WalletID  string             `json:"walletId"`
	Transfers []transferResponse `json:"transfers"`
}

func newHistoryResponse(walletID string, transfers []domain.Transfer) historyResponse {
	resp := make([]transferResponse, 0, len(transfers))
	for _, t := range transfers {
		resp = append(resp, transferResponseFromTransfer(t))
	}
	return historyResponse{WalletID: walletID, Transfers: resp}
}
