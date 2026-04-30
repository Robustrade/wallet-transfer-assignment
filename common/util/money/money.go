// Package money provides a typed representation of monetary amounts.
//
// All values are stored as int64 minor units (e.g. paisa for INR, cents for USD).
// Floating-point arithmetic is never used internally; callers should convert to
// minor units at the system boundary.
package money

// Money is an amount expressed in minor units of some currency.
//
// Add / Sub / Neg wrap on int64 overflow (the Go spec for signed-integer
// arithmetic). int64 minor units saturate at ~9.2e18, which in INR paisa
// is ~9.2e16 rupees — practically unreachable for a single wallet — but the
// type does not check for it. Callers operating on attacker-controllable or
// unbounded inputs must validate the magnitude themselves.
type Money int64

// FromMinor constructs a Money from an int64 expressed in minor units.
func FromMinor(n int64) Money { return Money(n) }

// Minor returns the underlying int64 value in minor units.
func (m Money) Minor() int64 { return int64(m) }

// Add returns m + o.
func (m Money) Add(o Money) Money { return m + o }

// Sub returns m - o. Callers must check the result for negativity if relevant.
func (m Money) Sub(o Money) Money { return m - o }

// Neg returns -m.
func (m Money) Neg() Money { return -m }

// IsPositive reports whether m > 0.
func (m Money) IsPositive() bool { return m > 0 }

// IsZero reports whether m == 0.
func (m Money) IsZero() bool { return m == 0 }

// IsNegative reports whether m < 0.
func (m Money) IsNegative() bool { return m < 0 }

// Lt reports whether m < o.
func (m Money) Lt(o Money) bool { return m < o }

// Gte reports whether m >= o.
func (m Money) Gte(o Money) bool { return m >= o }
