package model

import "time"

type WalletStatus string

const (
	WalletStatusActive       WalletStatus = "ACTIVE"
	WalletStatusDebitFrozen  WalletStatus = "DEBIT_FROZEN"
	WalletStatusCreditFrozen WalletStatus = "CREDIT_FROZEN"
	WalletStatusFrozen       WalletStatus = "FROZEN"
	WalletStatusClosed       WalletStatus = "CLOSED"
)

type Wallet struct {
	ID        string       `json:"id"`
	Balance   int64        `json:"balance"`
	Status    WalletStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type WalletResponse struct {
	ID      string       `json:"id"`
	Balance string       `json:"balance"`
	Status  WalletStatus `json:"status"`
}

type WalletURIParam struct {
	ID string `uri:"id" binding:"required,min=3,max=64,wallet_id"`
}

type CreateWalletRequest struct {
	ID             string       `json:"id"             binding:"required,min=3,max=64,wallet_id"`
	InitialBalance string       `json:"initialBalance" binding:"required,amount_non_negative"`
	Status         WalletStatus `json:"status"         binding:"omitempty,oneof=ACTIVE DEBIT_FROZEN CREDIT_FROZEN FROZEN CLOSED"`
}

type UpdateWalletStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=ACTIVE DEBIT_FROZEN CREDIT_FROZEN FROZEN CLOSED"`
}

func (s WalletStatus) CanDebit() bool {
	return s == WalletStatusActive || s == WalletStatusCreditFrozen
}

func (s WalletStatus) CanCredit() bool {
	return s == WalletStatusActive || s == WalletStatusDebitFrozen
}
