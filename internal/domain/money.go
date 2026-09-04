package domain

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
)

// Money wraps shopspring/decimal to provide safe fixed-point monetary operations.
type Money struct {
	dec decimal.Decimal
}

// MaxScale defines sub-cent precision (4 decimal places).
const MaxScale int32 = 4

func NewMoney(dec decimal.Decimal) Money {
	return Money{dec: dec.Truncate(MaxScale)}
}

func ParseMoney(val string) (Money, error) {
	d, err := decimal.NewFromString(val)
	if err != nil {
		return Money{}, ErrInvalidAmount(fmt.Sprintf("invalid numeric amount string: '%s'", val))
	}
	if d.Exponent() < -MaxScale {
		return Money{}, ErrInvalidAmount(fmt.Sprintf("amount exceeds maximum allowed precision of %d decimal places", MaxScale))
	}
	return NewMoney(d), nil
}

func (m Money) Decimal() decimal.Decimal {
	return m.dec
}

func (m Money) String() string {
	return m.dec.StringFixed(2)
}

func (m Money) StringExact() string {
	return m.dec.String()
}

func (m Money) IsPositive() bool {
	return m.dec.GreaterThan(decimal.Zero)
}

func (m Money) IsZero() bool {
	return m.dec.IsZero()
}

func (m Money) IsNegative() bool {
	return m.dec.IsNegative()
}

func (m Money) GreaterThanOrEqual(other Money) bool {
	return m.dec.GreaterThanOrEqual(other.dec)
}

func (m Money) LessThan(other Money) bool {
	return m.dec.LessThan(other.dec)
}

func (m Money) Add(other Money) Money {
	return NewMoney(m.dec.Add(other.dec))
}

func (m Money) Sub(other Money) Money {
	return NewMoney(m.dec.Sub(other.dec))
}

func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.dec.StringFixed(2))
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var val string
	if err := json.Unmarshal(data, &val); err == nil {
		parsed, err := ParseMoney(val)
		if err != nil {
			return err
		}
		*m = parsed
		return nil
	}

	// Support unmarshaling numbers directly if sent as numeric JSON
	var num float64
	if err := json.Unmarshal(data, &num); err != nil {
		return ErrInvalidAmount("amount must be a valid number or numeric string")
	}
	d := decimal.NewFromFloat(num)
	*m = NewMoney(d)
	return nil
}
