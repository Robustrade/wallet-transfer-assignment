package domain

type WalletID string
type Wallet struct {
	ID      WalletID
	Balance int64
}

func (w *Wallet) Debit(amount int64) error {

	err := w.ValidateDebit(amount)
	if err != nil {
		return err
	}
	w.Balance -= amount
	return nil
}

func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	w.Balance += amount
	return nil

}

func (w *Wallet) ValidateDebit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if w.Balance-amount < 0 {
		return ErrInsufficientFunds
	}

	return nil

}
