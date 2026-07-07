package domain

// Wallet stores its balance in the currency's smallest unit.
type Wallet struct {
	ID      string
	Balance int64
}
