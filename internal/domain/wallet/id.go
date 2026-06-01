package wallet

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const idPrefix = "wallet_"

// NewID generates a domain-owned, time-ordered, prefixed wallet identifier.
//
// Format: "wallet_<uuidv7-no-hyphens>"
// Example: "wallet_01960f3aab127e4ceab1234567890ab"
//
// UUID v7 is used for:
//   - Time-ordering: sequential inserts are B-tree friendly (no index page splits)
//   - Global uniqueness: no coordination needed across services
//   - Human readability: the "wallet_" prefix makes the entity type obvious in logs and URLs
//
// Conflicts with transfer IDs ("txn_...") are structurally impossible.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails if the system clock is broken; panic is appropriate.
		panic(fmt.Sprintf("wallet.NewID: failed to generate UUID v7: %v", err))
	}
	return idPrefix + strings.ReplaceAll(id.String(), "-", "")
}
