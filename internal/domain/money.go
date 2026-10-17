package domain

import (
	"fmt"
	"math"
)

// ValidateAmount validates that the amount in paise is positive and safe against overflow.
func ValidateAmount(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	// Check against absurdly large amounts that could cause overflow
	if amount > math.MaxInt64/2 {
		return NewDomainError(CodeInvalidAmount, "amount exceeds maximum allowable value")
	}
	return nil
}

// FormatPaise formats an integer paise amount into an INR string representation (e.g. ₹10.50).
// Note: This is for display/logging purposes only. All calculations remain strictly integer paise.
func FormatPaise(paise int64) string {
	sign := ""
	var magnitude uint64
	if paise < 0 {
		sign = "-"
		// Avoid negating math.MinInt64 directly; its positive counterpart does not
		// fit in int64. This expression safely computes its unsigned magnitude.
		magnitude = uint64(-(paise + 1)) + 1
	} else {
		magnitude = uint64(paise)
	}
	rupees := magnitude / 100
	remainder := magnitude % 100
	return fmt.Sprintf("%s₹%d.%02d", sign, rupees, remainder)
}
