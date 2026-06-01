package transfer

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const idPrefix = "txn_"

// NewID generates a domain-owned, time-ordered, prefixed transfer identifier.
//
// Format: "txn_<uuidv7-no-hyphens>"
// Example: "txn_01960f3bcd457e4ceab1234567890de"
//
// UUID v7 ensures time-ordering for sequential DB inserts and global uniqueness.
// The "txn_" prefix is structurally distinct from "wallet_" — no collision possible.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("transfer.NewID: failed to generate UUID v7: %v", err))
	}
	return idPrefix + strings.ReplaceAll(id.String(), "-", "")
}
