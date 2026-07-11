package handler

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
)

// amountPattern matches a positive decimal with at most 2 decimal places.
// Zero matches too — this only checks shape; whether zero is a valid
// amount is a business-meaning question the service answers, not this
// handler-level check.
var amountPattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

// validateAmount checks amount's shape and converts it into both its
// decimal form and integer minor units (e.g. "100.10" -> 10010), the
// same minor-units value request_hash canonicalizes against.
func validateAmount(amount json.Number) (decimal.Decimal, int64, error) {
	raw := amount.String()
	if !amountPattern.MatchString(raw) {
		return decimal.Decimal{}, 0, fmt.Errorf("amount must be a positive number with at most 2 decimal places")
	}

	parsed, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Decimal{}, 0, fmt.Errorf("amount is not a valid decimal: %w", err)
	}

	// IntPart silently truncates (wraps) if the value doesn't fit in an
	// int64. Converting the truncated value back to a decimal and
	// comparing against the pre-truncation value catches that silently —
	// shifted is already an exact integer decimal at this point (the
	// regex above guarantees at most 2 decimal places), so any mismatch
	// here can only mean overflow, never a real fractional truncation.
	shifted := parsed.Shift(2)
	minorUnits := shifted.IntPart()
	if !shifted.Equal(decimal.NewFromInt(minorUnits)) {
		return decimal.Decimal{}, 0, domain.ErrAmountUnrepresentable
	}
	return parsed, minorUnits, nil
}

// validateWalletID checks that raw is syntactically a UUID, deliberately
// not that it's specifically v7 — the UUID version is an internal detail
// of how wallet IDs happen to be generated, not a client-facing
// contract, so pinning to v7 here would make any future generation
// change a breaking API change for no correctness benefit.
func validateWalletID(raw string) error {
	if _, err := uuid.Parse(raw); err != nil {
		return fmt.Errorf("invalid wallet id: %w", err)
	}
	return nil
}

// validateIdempotencyKey checks that raw is a valid int64-shaped string.
// Snowflake IDs are 63-bit, but JavaScript's Number can only safely
// represent integers up to 2^53-1 — a bare JSON number would get
// silently corrupted by any browser client, which is why this is a
// string on the wire in both directions, not a number.
func validateIdempotencyKey(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("idempotencyKey must be a valid int64 string: %w", err)
	}
	return id, nil
}
