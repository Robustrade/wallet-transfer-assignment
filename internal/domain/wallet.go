package domain

// Wallet holds a stored balance expressed in the smallest currency unit
// (e.g. cents). Balances are integers; floating point is never used for money.
type Wallet struct {
	ID       string
	Balance  int64
	Currency string
}

// CanDebit reports whether the wallet holds enough funds to be debited by
// amount. The authoritative guard is the database CHECK (balance >= 0); this
// method lets the service fail fast with a meaningful error before writing.
func (w *Wallet) CanDebit(amount int64) bool {
	return w.Balance >= amount
}
