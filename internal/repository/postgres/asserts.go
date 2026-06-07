package postgres

import "github.com/Robustrade/wallet-transfer-assignment/internal/repository"

// Compile-time guarantees that the PostgreSQL types satisfy the repository
// ports. If a method signature drifts, the build fails here rather than at a
// distant call site.
var (
	_ repository.Store              = (*Store)(nil)
	_ repository.WalletRepository   = (*walletRepo)(nil)
	_ repository.TransferRepository = (*transferRepo)(nil)
	_ repository.LedgerRepository   = (*ledgerRepo)(nil)
)
