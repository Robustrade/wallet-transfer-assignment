package memory

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// walletTxRepo is the transactional wallet repository. Reads see staged writes
// first, then committed state, so a transaction observes its own mutations.
type walletTxRepo struct {
	tx *tx
}

// Create stages a new wallet, rejecting duplicates.
func (r *walletTxRepo) Create(_ context.Context, w *domain.Wallet) error {
	s := r.tx.store
	s.mu.Lock()
	_, committed := s.wallets[w.ID]
	s.mu.Unlock()
	if committed {
		return domain.ErrWalletAlreadyExists
	}
	if _, staged := r.tx.walletWrites[w.ID]; staged {
		return domain.ErrWalletAlreadyExists
	}
	r.tx.walletWrites[w.ID] = *w
	return nil
}

// Get reads a wallet without locking.
func (r *walletTxRepo) Get(_ context.Context, id string) (*domain.Wallet, error) {
	if w, ok := r.tx.walletWrites[id]; ok {
		clone := w
		return &clone, nil
	}
	r.tx.store.mu.Lock()
	w, ok := r.tx.store.wallets[id]
	r.tx.store.mu.Unlock()
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	clone := w
	return &clone, nil
}

// GetForUpdate acquires the wallet's row lock for the duration of the
// transaction, then returns the current row. Acquiring the lock here, while the
// store mutex is not held, lets concurrent transactions queue on the wallet
// exactly like SELECT ... FOR UPDATE.
func (r *walletTxRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	if _, held := r.tx.heldLocks[id]; !held {
		lock := r.tx.store.lockFor(id)
		lock.Lock()
		r.tx.heldLocks[id] = lock
	}
	return r.Get(ctx, id)
}

// UpdateBalance stages a balance change for an existing wallet.
func (r *walletTxRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	w, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	w.Balance = newBalance
	r.tx.walletWrites[id] = *w
	return nil
}
