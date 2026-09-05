package repository

import (
	"context"
)

type TxRepositories interface {
	Wallets() WalletRepository
	Transfers() TransferRepository
	Ledger() LedgerRepository
}

type TransactionManager interface {
	WithTx(
		ctx context.Context,
		fn func(repos TxRepositories) error,
	) error
}
