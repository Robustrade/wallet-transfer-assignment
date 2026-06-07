package memory

import (
	"context"
	"sort"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// Read repositories serve committed state for non-transactional queries such as
// status polling, balance reads and ledger history.

type walletReadRepo struct{ store *Store }

func (r *walletReadRepo) Create(context.Context, *domain.Wallet) error {
	panic("Create is not supported on the read repository")
}

func (r *walletReadRepo) Get(_ context.Context, id string) (*domain.Wallet, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	w, ok := r.store.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	clone := w
	return &clone, nil
}

func (r *walletReadRepo) GetForUpdate(context.Context, string) (*domain.Wallet, error) {
	panic("GetForUpdate is not supported on the read repository")
}

func (r *walletReadRepo) UpdateBalance(context.Context, string, int64) error {
	panic("UpdateBalance is not supported on the read repository")
}

type transferReadRepo struct{ store *Store }

func (r *transferReadRepo) InsertIfAbsent(context.Context, *domain.Transfer) (bool, *domain.Transfer, error) {
	panic("InsertIfAbsent is not supported on the read repository")
}

func (r *transferReadRepo) Get(_ context.Context, id string) (*domain.Transfer, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	t, ok := r.store.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	clone := t
	return &clone, nil
}

func (r *transferReadRepo) UpdateStatus(context.Context, *domain.Transfer) error {
	panic("UpdateStatus is not supported on the read repository")
}

type ledgerReadRepo struct{ store *Store }

func (r *ledgerReadRepo) InsertPair(context.Context, *domain.LedgerEntry, *domain.LedgerEntry) error {
	panic("InsertPair is not supported on the read repository")
}

// ListByWallet returns a wallet's ledger entries, newest first.
func (r *ledgerReadRepo) ListByWallet(_ context.Context, walletID string) ([]domain.LedgerEntry, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	var out []domain.LedgerEntry
	for _, e := range r.store.ledger {
		if e.WalletID == walletID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}
