package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// HashTransferRequest fingerprints the request body (without the key). It detects a key
// that is reused with a different request. The fields are encoded in a fixed order.
func HashTransferRequest(cmd domain.TransferCommand) string {
	canonical, _ := json.Marshal([]any{cmd.FromWalletID, cmd.ToWalletID, cmd.Amount})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// newUUID returns a random version 4 UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
