package unit

import (
	"encoding/json"
	"testing"

	"wallet-transfer-assignment/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMoney(t *testing.T) {
	t.Run("valid positive amounts", func(t *testing.T) {
		m, err := domain.ParseMoney("100.50")
		require.NoError(t, err)
		assert.Equal(t, "100.50", m.String())
		assert.True(t, m.IsPositive())
	})

	t.Run("exceeds max scale precision", func(t *testing.T) {
		_, err := domain.ParseMoney("10.12345")
		require.Error(t, err)
		var appErr *domain.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, domain.ErrCodeInvalidAmount, appErr.Code)
	})

	t.Run("invalid numeric string", func(t *testing.T) {
		_, err := domain.ParseMoney("abc")
		require.Error(t, err)
	})
}

func TestMoneyArithmetic(t *testing.T) {
	m1, _ := domain.ParseMoney("100.00")
	m2, _ := domain.ParseMoney("45.50")

	sub := m1.Sub(m2)
	assert.Equal(t, "54.50", sub.String())

	add := m2.Add(sub)
	assert.Equal(t, "100.00", add.String())

	assert.True(t, m1.GreaterThanOrEqual(m2))
	assert.False(t, m2.GreaterThanOrEqual(m1))
}

func TestMoneyJSON(t *testing.T) {
	m, _ := domain.ParseMoney("123.45")

	data, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, `"123.45"`, string(data))

	var unmarshaled domain.Money
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)
	assert.Equal(t, "123.45", unmarshaled.String())
}
