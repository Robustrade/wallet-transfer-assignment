package fixtures

import (
	"wallet-transfer/internal/wallet/domain"

	"github.com/google/uuid"
)

// WalletFixture creates a test wallet
func WalletFixture() *domain.Wallet {
	return domain.NewWallet(
		uuid.New().String(),
		uuid.New().String(),
		"USD",
	)
}

// WalletWithBalanceFixture creates a test wallet with specified balance
func WalletWithBalanceFixture(balance int64) *domain.Wallet {
	wallet := WalletFixture()
	wallet.Balance = balance
	return wallet
}
