package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type canonicalTransferRequest struct {
	FromWalletID string `json:"fromWalletId"`
	ToWalletID string `json:"toWalletId"`
	Amount int64 `json:"amount"`
}

func hashRequest(request TransferRequest) (string, error) {
	canonical, err := json.Marshal(canonicalTransferRequest{
		FromWalletID: request.FromWalletID,
		ToWalletID: request.ToWalletID,
		Amount: int64(request.Amount),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
