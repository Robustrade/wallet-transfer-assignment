package entrepo

import (
	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/internal/repository"
)

type TxRepositories struct {
	wallets   repository.WalletRepository
	transfers repository.TransferRepository
	ledger    repository.LedgerRepository
}

func NewTxRepositories(tx *ent.Tx) *TxRepositories {
	client := tx.Client()

	return &TxRepositories{
		wallets:   NewWalletRepository(client),
		transfers: NewTransferRepository(client),
		ledger:    NewLedgerRepository(client),
	}
}

func (r *TxRepositories) Wallets() repository.WalletRepository {
	return r.wallets
}

func (r *TxRepositories) Transfers() repository.TransferRepository {
	return r.transfers
}

func (r *TxRepositories) Ledger() repository.LedgerRepository {
	return r.ledger
}
