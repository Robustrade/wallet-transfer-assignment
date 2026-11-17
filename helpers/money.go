package helpers

// Money is an amount in minor units (implicit INR). Never use floats.
// Validation constructors live in model (NewAmount / NewBalance) so errors stay in one place.
type Money int64

func (m Money) Int64() int64 { return int64(m) }
