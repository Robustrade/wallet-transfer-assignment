package repository

type Repositories struct {
	Wallet      *WalletRepository
	Transfer    *TransferRepository
	Ledger      *LedgerRepository
	Idempotency *IdempotencyRepository
}

func NewRepositories() *Repositories {
	return &Repositories{
		Wallet:      NewWalletRepository(),
		Transfer:    NewTransferRepository(),
		Ledger:      NewLedgerRepository(),
		Idempotency: NewIdempotencyRepository(),
	}
}
