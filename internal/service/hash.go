package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// requestHash fingerprints the parts of a transfer request that must not
// change between retries sharing the same idempotency key. If a client sends
// a different payload under a key it already used, that's a client bug, not
// a legitimate retry — the hash mismatch is what catches it.
//
// The preimage is JSON-encoded rather than delimiter-joined: wallet IDs are
// unconstrained strings and could legally contain a plain separator like
// "|", which would let two different (from, to) pairs collide onto the same
// preimage (e.g. ("a|b","c") and ("a","b|c")). JSON's string escaping makes
// field boundaries unambiguous regardless of what characters a wallet ID
// contains.
func requestHash(in CreateTransferInput) string {
	preimage, _ := json.Marshal(struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}{From: in.FromWalletID, To: in.ToWalletID, Amount: in.Amount})
	// json.Marshal cannot fail here: every field is a plain string or int64.

	sum := sha256.Sum256(preimage)
	return hex.EncodeToString(sum[:])
}
