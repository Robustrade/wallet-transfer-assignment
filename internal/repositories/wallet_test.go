package repositories

import (
	"testing"

	"time"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"github.com/stretchr/testify/assert"
)

// Test wallet model structure and table name
func TestWalletModel(t *testing.T) {
	t.Run("valid wallet", func(t *testing.T) {
		now := time.Now()
		wallet := models.Wallet{
			WalletID:  "test-wallet-1",
			Balance:   100,
			CreatedAt: now,
			UpdatedAt: now,
		}
		assert.Equal(t, "test-wallet-1", wallet.WalletID)
		assert.Equal(t, int64(100), wallet.Balance)
		assert.Equal(t, "wallets", wallet.TableName())
	})

	t.Run("wallet with zero balance", func(t *testing.T) {
		wallet := models.Wallet{
			WalletID:  "test-wallet-2",
			Balance:   0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		assert.Equal(t, int64(0), wallet.Balance)
	})

	t.Run("wallet with large balance", func(t *testing.T) {
		wallet := models.Wallet{
			WalletID:  "test-wallet-3",
			Balance:   999999999999,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		assert.Equal(t, int64(999999999999), wallet.Balance)
	})
}
