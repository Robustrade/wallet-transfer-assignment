package unit

import (
	"testing"

	"wallet-transfer/internal/wallet/domain"

	"github.com/stretchr/testify/assert"
)

func TestNewWallet(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")

	assert.Equal(t, "wallet_1", wallet.ID)
	assert.Equal(t, "user_1", wallet.UserID)
	assert.Equal(t, int64(0), wallet.Balance)
	assert.Equal(t, "USD", wallet.Currency)
}

func TestWalletCanDebit(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")
	wallet.Balance = 1000

	assert.True(t, wallet.CanDebit(500))
	assert.True(t, wallet.CanDebit(1000))
	assert.False(t, wallet.CanDebit(1001))
}

func TestWalletDebit(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")
	wallet.Balance = 1000

	err := wallet.Debit(500)
	assert.NoError(t, err)
	assert.Equal(t, int64(500), wallet.Balance)
}

func TestWalletDebitInsufficientFunds(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")
	wallet.Balance = 100

	err := wallet.Debit(500)
	assert.Error(t, err)
	assert.Equal(t, domain.ErrInsufficientFunds, err)
	assert.Equal(t, int64(100), wallet.Balance)
}

func TestWalletDebitInvalidAmount(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")
	wallet.Balance = 1000

	err := wallet.Debit(-100)
	assert.Error(t, err)
	assert.Equal(t, domain.ErrInvalidAmount, err)
	assert.Equal(t, int64(1000), wallet.Balance)
}

func TestWalletCredit(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")
	wallet.Balance = 500

	err := wallet.Credit(500)
	assert.NoError(t, err)
	assert.Equal(t, int64(1000), wallet.Balance)
}

func TestWalletCreditInvalidAmount(t *testing.T) {
	wallet := domain.NewWallet("wallet_1", "user_1", "USD")
	wallet.Balance = 500

	err := wallet.Credit(-100)
	assert.Error(t, err)
	assert.Equal(t, domain.ErrInvalidAmount, err)
	assert.Equal(t, int64(500), wallet.Balance)
}
