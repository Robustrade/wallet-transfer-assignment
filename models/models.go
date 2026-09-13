package models

import "wallet-transfer-assigment/utils"

// wallets
// transfers
// ledger_entries
// idempotency_records

type Wallet struct {
	WalletID string `json:"wallet_id"`
	Balance  int64  `json:"balance"` // in lowest denomination (e.g., cents for USD)
}

type Transfers struct {
	TransactionID string                `json:"transaction_id"`
	FromWalletID  string                `json:"from_wallet_id"`
	ToWalletID    string                `json:"to_wallet_id"`
	Amount        int64                 `json:"amount"` // in lowest denomination (e.g., cents for USD)
	Status        utils.TRANSFER_STATUS `json:"status"` // e.g., "PENDING", "COMPLETED", "FAILED"
	Comments      string                `json:"comments"`
}

type LedgerEntries struct {
	WalletID      string                 `json:"wallet_id"`
	Amount        int64                  `json:"amount"` // in lowest denomination (e.g., cents for USD)
	Type          utils.TRANSACTION_TYPE `json:"type"`
	TransactionID string                 `json:"transaction_id"`
}

type IdempotencyRecords struct {
	IdempotencyKey string `json:"idempotency_key"`
	TransactionID  string `json:"transaction_id"`
}
