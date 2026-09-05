package entrepo

import (
	"context"

	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/ent/ledgerentry"
	"wallet-transfer-assignment/internal/domain"
)

type LedgerRepository struct {
	client *ent.Client
}

func NewLedgerRepository(client *ent.Client) *LedgerRepository {
	return &LedgerRepository{
		client: client,
	}
}

func (r *LedgerRepository) CreateMany(
	ctx context.Context,
	entries ...*domain.LedgerEntry,
) error {
	builders := make([]*ent.LedgerEntryCreate, 0, len(entries))

	for _, entry := range entries {
		builder := r.client.LedgerEntry.
			Create().
			SetID(entry.ID).
			SetTransferID(entry.TransferID).
			SetWalletID(entry.WalletID).
			SetEntryType(
				ledgerentry.EntryType(entry.Type),
			).
			SetAmount(entry.Amount)

		builders = append(builders, builder)
	}

	return r.client.LedgerEntry.
		CreateBulk(builders...).
		Exec(ctx)
}
