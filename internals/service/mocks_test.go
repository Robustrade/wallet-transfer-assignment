package service

import (
	"context"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeTxManager just runs fn directly against a nil pgx.Tx — none of the
// fake repositories below actually touch the tx, so no real connection or
// transaction is needed to unit test the service's orchestration logic.
type fakeTxManager struct{}

func (fakeTxManager) WithinTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return fn(nil)
}

type fakeWalletRepo struct {
	wallets   map[uuid.UUID]*models.Wallet
	getErr    error
	updateErr error
	updated   []models.Wallet
}

func (f *fakeWalletRepo) GetForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*models.Wallet, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	w, ok := f.wallets[id]
	if !ok {
		return nil, repository.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWalletRepo) UpdateBalance(ctx context.Context, tx pgx.Tx, w *models.Wallet) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = append(f.updated, *w)
	return nil
}

type fakeTransferRepo struct {
	createErr      error
	created        []models.Transfer
	updateStateErr error
	updatedStates  []models.Transfer
	byKey          map[string]*models.Transfer
	getByKeyErr    error
}

func (f *fakeTransferRepo) Create(ctx context.Context, tx pgx.Tx, t *models.Transfer) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, *t)
	return nil
}

func (f *fakeTransferRepo) UpdateState(ctx context.Context, tx pgx.Tx, t *models.Transfer) error {
	if f.updateStateErr != nil {
		return f.updateStateErr
	}
	f.updatedStates = append(f.updatedStates, *t)
	return nil
}

func (f *fakeTransferRepo) GetByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (*models.Transfer, error) {
	if f.getByKeyErr != nil {
		return nil, f.getByKeyErr
	}
	if f.byKey == nil {
		return nil, nil
	}
	return f.byKey[key], nil
}

type fakeLedgerRepo struct {
	insertErr error
	inserted  []models.LedgerEntry
}

func (f *fakeLedgerRepo) InsertPair(ctx context.Context, tx pgx.Tx, debit, credit models.LedgerEntry) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, debit, credit)
	return nil
}

type fakeIdempRepo struct {
	getRec  *repository.IdempotencyRecord
	getErr  error
	saveErr error
	saved   []repository.IdempotencyRecord
}

func (f *fakeIdempRepo) Get(ctx context.Context, tx pgx.Tx, key string) (*repository.IdempotencyRecord, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getRec, nil
}

func (f *fakeIdempRepo) Save(ctx context.Context, tx pgx.Tx, rec *repository.IdempotencyRecord) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, *rec)
	return nil
}
