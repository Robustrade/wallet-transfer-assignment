package domain

// Money is represented as an integer count of the smallest indivisible unit of
// the currency (for example, cents). Integer arithmetic avoids the rounding and
// representation errors inherent in floating-point money handling, which is a
// hard requirement for a ledger that must always balance.
//
// A single, implicit currency is assumed for this assignment; multi-currency
// support would add a currency dimension to wallets, amounts, and ledger
// entries (see docs/DESIGN.md, "Assumptions").
type Money = int64

// ValidateAmount enforces the invariant that transferable amounts are strictly
// positive. Zero and negative transfers are rejected at the domain boundary.
func ValidateAmount(amount Money) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
