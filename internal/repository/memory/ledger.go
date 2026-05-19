package memory

import (
	"context"
	"time"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

type ledgerRepo struct {
	s *Store
}

func (r *ledgerRepo) Create(_ context.Context, entry *domain.LedgerEntry) error {
	r.s.mustBeInTransaction()

	r.s.nextEntryID++
	copy := *entry
	copy.ID = r.s.nextEntryID
	copy.CreatedAt = time.Now().UTC()
	r.s.ledger = append(r.s.ledger, copy)
	entry.ID = copy.ID
	entry.CreatedAt = copy.CreatedAt
	return nil
}

func (r *ledgerRepo) ListByTransferID(_ context.Context, transferID string) ([]domain.LedgerEntry, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	var entries []domain.LedgerEntry
	for _, e := range r.s.ledger {
		if e.TransferID == transferID {
			entries = append(entries, e)
		}
	}
	return entries, nil
}
