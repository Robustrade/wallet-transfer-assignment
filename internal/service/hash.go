package service

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// computeRequestHash fingerprints the financial content of a transfer request,
// so the idempotency layer can tell a genuine retry (same hash) from a client
// reusing a key for a different transfer (different hash).
//
// Only the three fields that define "the same transfer" are hashed. Anything
// else the client sends (timestamps, descriptions) is deliberately excluded,
// since a retry that differs only in those is still the same transfer.
//
// The input is built from parsed values in a fixed order, never from the raw
// JSON body, so key order and whitespace in the request cannot change the hash.
//
//   - uuid.UUID.String() always yields the canonical lowercase form, so the
//     same wallet sent as upper- or lower-case hashes identically.
//   - The '|' delimiter prevents boundary collisions between adjacent fields.
//   - StringFixed(2) forces two decimal places (100.5 and 100.50 both become
//     "100.50"), matching the NUMERIC(16,2) scale in the schema.
//
// Callers must reject amounts with more than two decimal places BEFORE hashing:
// StringFixed rounds, so 100.505 and 100.51 would otherwise hash the same.
func computeRequestHash(fromWalletID, toWalletID uuid.UUID, amount decimal.Decimal) string {
	input := fromWalletID.String() + "|" + toWalletID.String() + "|" + amount.StringFixed(2)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
