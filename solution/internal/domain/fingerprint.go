package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// RequestFingerprint binds an idempotency key to the request it was first used
// with, so the same key cannot answer for a different transfer. The NUL
// separator stops two different requests from joining to the same string.
func RequestFingerprint(fromWalletID, toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		fromWalletID,
		toWalletID,
		strconv.FormatInt(amount, 10),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}
