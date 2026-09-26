package postgres

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func newTransferID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fmt.Sprintf("tr_%d", time.Now().UTC().UnixNano())
	}
	return "tr_" + hex.EncodeToString(id[:])
}

func newClaimToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate idempotency owner token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}
