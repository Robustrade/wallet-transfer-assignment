package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

type IdempotencyRecord struct {
	Key          string     `json:"key"`
	RequestHash  string     `json:"requestHash"`
	TransferID   *uuid.UUID `json:"transferId,omitempty"`
	ResponseCode int        `json:"responseCode"`
	ResponseBody []byte     `json:"responseBody"`
}

func ComputeRequestHash(fromWalletID, toWalletID string, amount string) string {
	raw := fmt.Sprintf("%s:%s:%s", fromWalletID, toWalletID, amount)
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
