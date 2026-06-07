package memory

import "github.com/Robustrade/wallet-transfer-assignment/internal/repository"

// Compile-time guarantees that the in-memory types satisfy the repository
// ports, keeping the test double honest with the production contract.
var (
	_ repository.Store              = (*Store)(nil)
	_ repository.WalletRepository   = (*walletTxRepo)(nil)
	_ repository.TransferRepository = (*transferTxRepo)(nil)
	_ repository.LedgerRepository   = (*ledgerTxRepo)(nil)
	_ repository.WalletRepository   = (*walletReadRepo)(nil)
	_ repository.TransferRepository = (*transferReadRepo)(nil)
	_ repository.LedgerRepository   = (*ledgerReadRepo)(nil)
)
