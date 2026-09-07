package memory

import (
	"context"

	"Wallet_Transfer_Service/internal/domain"
)

type ledgerRepo Store

func (r *ledgerRepo) InsertPair(ctx context.Context, debit, credit *domain.LedgerEntry) error {
	defer (*Store)(r).guard()()

	*r.nextEntryID++
	debit.ID = *r.nextEntryID
	*r.nextEntryID++
	credit.ID = *r.nextEntryID

	dCp, cCp := *debit, *credit
	r.ledger[debit.TransferID] = append(r.ledger[debit.TransferID], &dCp, &cCp)
	return nil
}

func (r *ledgerRepo) ListByTransfer(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error) {
	defer (*Store)(r).guard()()
	entries := r.ledger[transferID]
	out := make([]*domain.LedgerEntry, len(entries))
	for i, e := range entries {
		cp := *e
		out[i] = &cp
	}
	return out, nil
}
