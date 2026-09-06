package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// hashRequest fingerprints the mutable parts of a transfer request so a
// reused idempotency key can be checked against a differing payload.
func hashRequest(fromWalletID, toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amount)))
	return hex.EncodeToString(sum[:])
}
