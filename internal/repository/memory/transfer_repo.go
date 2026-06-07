package memory

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// transferTxRepo is the transactional transfer repository.
type transferTxRepo struct {
	tx *tx
}

// InsertIfAbsent inserts a PENDING transfer unless its idempotency key already
// exists.
//
// To emulate a unique index it first acquires a per-key lock that is held for
// the remainder of the transaction. A concurrent transaction inserting the same
// key blocks on this lock until the first transaction commits (and the key
// becomes visible -> duplicate) or rolls back (and the key is released -> the
// waiter proceeds). This mirrors how PostgreSQL serialises INSERT ... ON
// CONFLICT on a unique constraint.
func (r *transferTxRepo) InsertIfAbsent(_ context.Context, t *domain.Transfer) (bool, *domain.Transfer, error) {
	// Acquire (once) the idempotency-key lock for this transaction.
	if _, held := r.tx.heldKeys[t.IdempotencyKey]; !held {
		lock := r.tx.store.keyLockFor(t.IdempotencyKey)
		lock.Lock()
		r.tx.heldKeys[t.IdempotencyKey] = lock
	}

	s := r.tx.store
	s.mu.Lock()
	defer s.mu.Unlock()

	// Already committed under this key?
	if existingID, ok := s.keyToID[t.IdempotencyKey]; ok {
		existing := s.transfers[existingID]
		clone := existing
		return false, &clone, nil
	}
	// Staged within this same transaction?
	if existingID, ok := r.tx.keyWrites[t.IdempotencyKey]; ok {
		existing := r.tx.transferWrites[existingID]
		clone := existing
		return false, &clone, nil
	}

	r.tx.transferWrites[t.ID] = *t
	r.tx.keyWrites[t.IdempotencyKey] = t.ID
	return true, nil, nil
}

// Get reads a transfer, preferring staged state.
func (r *transferTxRepo) Get(_ context.Context, id string) (*domain.Transfer, error) {
	if t, ok := r.tx.transferWrites[id]; ok {
		clone := t
		return &clone, nil
	}
	s := r.tx.store
	s.mu.Lock()
	t, ok := s.transfers[id]
	s.mu.Unlock()
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	clone := t
	return &clone, nil
}

// UpdateStatus stages a status change for a transfer.
func (r *transferTxRepo) UpdateStatus(_ context.Context, t *domain.Transfer) error {
	r.tx.transferWrites[t.ID] = *t
	return nil
}
