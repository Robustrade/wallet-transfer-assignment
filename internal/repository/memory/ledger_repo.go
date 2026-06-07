package memory

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// ledgerTxRepo is the transactional ledger repository.
type ledgerTxRepo struct {
	tx *tx
}

// InsertPair stages the balanced debit/credit pair. The two entries are always
// added together so the buffered ledger can never hold a half-written pair.
func (r *ledgerTxRepo) InsertPair(_ context.Context, debit, credit *domain.LedgerEntry) error {
	r.tx.ledgerWrites = append(r.tx.ledgerWrites, *debit, *credit)
	return nil
}

// ListByWallet is not supported on the transactional repo; history reads go
// through the Reader repositories.
func (r *ledgerTxRepo) ListByWallet(_ context.Context, _ string) ([]domain.LedgerEntry, error) {
	return nil, nil
}
