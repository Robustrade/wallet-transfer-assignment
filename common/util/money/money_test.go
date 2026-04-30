package money_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
)

func TestMoney_FromMinor_Minor(t *testing.T) {
	m := money.FromMinor(12345)
	require.Equal(t, int64(12345), m.Minor())
}

func TestMoney_Add(t *testing.T) {
	a := money.FromMinor(100)
	b := money.FromMinor(50)
	assert.Equal(t, int64(150), a.Add(b).Minor())
}

func TestMoney_Sub(t *testing.T) {
	a := money.FromMinor(100)
	b := money.FromMinor(30)
	assert.Equal(t, int64(70), a.Sub(b).Minor())
}

func TestMoney_Sub_NegativeAllowed(t *testing.T) {
	// Sub does not protect against negative results — callers must check.
	a := money.FromMinor(10)
	b := money.FromMinor(50)
	assert.True(t, a.Sub(b).IsNegative())
}

func TestMoney_Neg(t *testing.T) {
	assert.Equal(t, int64(-100), money.FromMinor(100).Neg().Minor())
	assert.Equal(t, int64(0), money.FromMinor(0).Neg().Minor())
}

func TestMoney_Predicates(t *testing.T) {
	tests := []struct {
		name       string
		m          money.Money
		isPositive bool
		isZero     bool
		isNegative bool
	}{
		{"positive", money.FromMinor(1), true, false, false},
		{"zero", money.FromMinor(0), false, true, false},
		{"negative", money.FromMinor(-1), false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isPositive, tt.m.IsPositive())
			assert.Equal(t, tt.isZero, tt.m.IsZero())
			assert.Equal(t, tt.isNegative, tt.m.IsNegative())
		})
	}
}

func TestMoney_Compare(t *testing.T) {
	a := money.FromMinor(100)
	b := money.FromMinor(200)
	assert.True(t, a.Lt(b))
	assert.False(t, b.Lt(a))
	assert.True(t, a.Gte(a))
	assert.True(t, b.Gte(a))
	assert.False(t, a.Gte(b))
}
