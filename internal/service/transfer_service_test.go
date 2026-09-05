package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type fakeTxManager struct {
	repos *fakeTxRepositories
}

func (f *fakeTxManager) WithTx(
	ctx context.Context,
	fn func(repository.TxRepositories) error,
) error {
	walletSnapshot := cloneWallets(f.repos.wallets.(*fakeWalletRepository).wallets)
	transferSnapshot := cloneTransfers(f.repos.transfers.(*fakeTransferRepository).transfers)
	ledgerSnapshot := cloneLedgerEntries(f.repos.ledger.(*fakeLedgerRepository).entries)

	err := fn(f.repos)
	if err == nil {
		return nil
	}

	f.repos.wallets.(*fakeWalletRepository).wallets = walletSnapshot
	f.repos.transfers.(*fakeTransferRepository).transfers = transferSnapshot
	f.repos.ledger.(*fakeLedgerRepository).entries = ledgerSnapshot

	return err
}

type fakeTxRepositories struct {
	wallets   repository.WalletRepository
	transfers repository.TransferRepository
	ledger    repository.LedgerRepository
}

func (f *fakeTxRepositories) Wallets() repository.WalletRepository {
	return f.wallets
}

func (f *fakeTxRepositories) Transfers() repository.TransferRepository {
	return f.transfers
}

func (f *fakeTxRepositories) Ledger() repository.LedgerRepository {
	return f.ledger
}

type fakeWalletRepository struct {
	wallets map[uuid.UUID]*domain.Wallet
}

func (f *fakeWalletRepository) FindByID(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Wallet, error) {
	w, ok := f.wallets[id]
	if !ok {
		return nil, repository.ErrNotFound
	}

	return w, nil
}

func (f *fakeWalletRepository) FindByIDsForUpdate(
	ctx context.Context,
	ids ...uuid.UUID,
) ([]*domain.Wallet, error) {
	result := make([]*domain.Wallet, 0, len(ids))

	for _, id := range ids {
		if w, ok := f.wallets[id]; ok {
			result = append(result, w)
		}
	}

	return result, nil
}

func (f *fakeWalletRepository) Debit(
	ctx context.Context,
	id uuid.UUID,
	amount int64,
) error {
	w, ok := f.wallets[id]
	if !ok {
		return repository.ErrNotFound
	}

	if w.Balance < amount {
		return domain.ErrInsufficientFunds
	}

	w.Balance -= amount

	return nil
}

func (f *fakeWalletRepository) Credit(
	ctx context.Context,
	id uuid.UUID,
	amount int64,
) error {
	w, ok := f.wallets[id]
	if !ok {
		return repository.ErrNotFound
	}

	w.Balance += amount

	return nil
}

// -----------------------------------------------------------------------------
// Fake TransferRepository
// -----------------------------------------------------------------------------

type fakeTransferRepository struct {
	transfers map[string]*domain.Transfer
}

func newFakeTransferRepository() *fakeTransferRepository {
	return &fakeTransferRepository{
		transfers: make(map[string]*domain.Transfer),
	}
}

func (f *fakeTransferRepository) FindByIdempotencyKey(
	ctx context.Context,
	key string,
) (*domain.Transfer, error) {
	t, ok := f.transfers[key]

	if !ok {
		return nil, repository.ErrNotFound
	}

	return t, nil
}

func (f *fakeTransferRepository) Create(
	ctx context.Context,
	t *domain.Transfer,
) error {
	if _, exists := f.transfers[t.IdempotencyKey]; exists {
		return repository.ErrAlreadyExists
	}

	f.transfers[t.IdempotencyKey] = t

	return nil
}

func (f *fakeTransferRepository) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	status domain.TransferStatus,
	failureReason domain.TransferFailureReason,
) error {
	for _, t := range f.transfers {
		if t.ID == id {
			t.Status = status
			t.FailureReason = failureReason

			return nil
		}
	}

	return repository.ErrNotFound
}

// -----------------------------------------------------------------------------
// Fake LedgerRepository
// -----------------------------------------------------------------------------

type fakeLedgerRepository struct {
	entries   []*domain.LedgerEntry
	createErr error
}

func (f *fakeLedgerRepository) CreateMany(
	ctx context.Context,
	entries ...*domain.LedgerEntry,
) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.entries = append(f.entries, entries...)

	return nil
}

func TestCreateTransfer_Success(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 1000,
			},
			toWalletID: {
				ID:      toWalletID,
				Balance: 500,
			},
		},
	}

	transferRepo := newFakeTransferRepository()

	ledgerRepo := &fakeLedgerRepository{}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	req := domain.CreateTransfer{
		IdempotencyKey: "transfer-success-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	result, err := svc.CreateTransfer(ctx, req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	if result.Transfer == nil {
		t.Fatal("expected transfer, got nil")
	}

	if result.Replayed {
		t.Fatal("expected new transfer, got replayed transfer")
	}

	if result.Transfer.Status != domain.TransferStatusProcessed {
		t.Fatalf(
			"expected transfer status %s, got %s",
			domain.TransferStatusProcessed,
			result.Transfer.Status,
		)
	}

	// Source: 1000 - 200 = 800
	if walletRepo.wallets[fromWalletID].Balance != 800 {
		t.Fatalf(
			"expected source balance 800, got %d",
			walletRepo.wallets[fromWalletID].Balance,
		)
	}

	// Destination: 500 + 200 = 700
	if walletRepo.wallets[toWalletID].Balance != 700 {
		t.Fatalf(
			"expected destination balance 700, got %d",
			walletRepo.wallets[toWalletID].Balance,
		)
	}

	// Exactly two ledger entries are required.
	if len(ledgerRepo.entries) != 2 {
		t.Fatalf(
			"expected 2 ledger entries, got %d",
			len(ledgerRepo.entries),
		)
	}

	assertLedgerEntry(
		t,
		ledgerRepo.entries,
		fromWalletID,
		domain.LedgerEntryTypeDebit,
		200,
	)

	assertLedgerEntry(
		t,
		ledgerRepo.entries,
		toWalletID,
		domain.LedgerEntryTypeCredit,
		200,
	)
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 1000,
			},
			toWalletID: {
				ID:      toWalletID,
				Balance: 500,
			},
		},
	}

	transferRepo := newFakeTransferRepository()
	ledgerRepo := &fakeLedgerRepository{}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	req := domain.CreateTransfer{
		IdempotencyKey: "transfer-idempotent-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	firstResult, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	secondResult, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("replayed transfer failed: %v", err)
	}

	if firstResult.Transfer.ID != secondResult.Transfer.ID {
		t.Fatalf(
			"expected same transfer ID, got %s and %s",
			firstResult.Transfer.ID,
			secondResult.Transfer.ID,
		)
	}

	if !secondResult.Replayed {
		t.Fatal("expected second request to be replayed")
	}

	if walletRepo.wallets[fromWalletID].Balance != 800 {
		t.Fatalf(
			"expected source balance 800, got %d",
			walletRepo.wallets[fromWalletID].Balance,
		)
	}

	if walletRepo.wallets[toWalletID].Balance != 700 {
		t.Fatalf(
			"expected destination balance 700, got %d",
			walletRepo.wallets[toWalletID].Balance,
		)
	}

	if len(ledgerRepo.entries) != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries after retry, got %d",
			len(ledgerRepo.entries),
		)
	}

	if len(transferRepo.transfers) != 1 {
		t.Fatalf(
			"expected exactly 1 transfer, got %d",
			len(transferRepo.transfers),
		)
	}
}

func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 1000,
			},
			toWalletID: {
				ID:      toWalletID,
				Balance: 500,
			},
		},
	}

	transferRepo := newFakeTransferRepository()
	ledgerRepo := &fakeLedgerRepository{}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	firstReq := domain.CreateTransfer{
		IdempotencyKey: "transfer-conflict-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	_, err := svc.CreateTransfer(ctx, firstReq)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	secondReq := domain.CreateTransfer{
		IdempotencyKey: "transfer-conflict-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         300, // same key, different payload
	}

	_, err = svc.CreateTransfer(ctx, secondReq)

	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf(
			"expected ErrIdempotencyConflict, got %v",
			err,
		)
	}

	if walletRepo.wallets[fromWalletID].Balance != 800 {
		t.Fatalf(
			"expected source balance to remain 800, got %d",
			walletRepo.wallets[fromWalletID].Balance,
		)
	}

	if walletRepo.wallets[toWalletID].Balance != 700 {
		t.Fatalf(
			"expected destination balance to remain 700, got %d",
			walletRepo.wallets[toWalletID].Balance,
		)
	}

	if len(ledgerRepo.entries) != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			len(ledgerRepo.entries),
		)
	}

	if len(transferRepo.transfers) != 1 {
		t.Fatalf(
			"expected exactly 1 transfer, got %d",
			len(transferRepo.transfers),
		)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 100,
			},
			toWalletID: {
				ID:      toWalletID,
				Balance: 500,
			},
		},
	}

	transferRepo := newFakeTransferRepository()
	ledgerRepo := &fakeLedgerRepository{}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	req := domain.CreateTransfer{
		IdempotencyKey: "transfer-insufficient-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	result, err := svc.CreateTransfer(ctx, req)

	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf(
			"expected ErrInsufficientFunds, got %v",
			err,
		)
	}

	if result == nil || result.Transfer == nil {
		t.Fatal("expected failed transfer result")
	}

	if result.Transfer.Status != domain.TransferStatusFailed {
		t.Fatalf(
			"expected FAILED status, got %s",
			result.Transfer.Status,
		)
	}

	if result.Transfer.FailureReason !=
		domain.TransferFailureInsufficientFunds {

		t.Fatalf(
			"expected failure reason %s, got %s",
			domain.TransferFailureInsufficientFunds,
			result.Transfer.FailureReason,
		)
	}

	// No money should move.
	if walletRepo.wallets[fromWalletID].Balance != 100 {
		t.Fatalf(
			"expected source balance 100, got %d",
			walletRepo.wallets[fromWalletID].Balance,
		)
	}

	if walletRepo.wallets[toWalletID].Balance != 500 {
		t.Fatalf(
			"expected destination balance 500, got %d",
			walletRepo.wallets[toWalletID].Balance,
		)
	}

	// Failed transfer must not create ledger entries.
	if len(ledgerRepo.entries) != 0 {
		t.Fatalf(
			"expected 0 ledger entries, got %d",
			len(ledgerRepo.entries),
		)
	}

	// The FAILED transfer should still be persisted.
	if len(transferRepo.transfers) != 1 {
		t.Fatalf(
			"expected 1 persisted transfer, got %d",
			len(transferRepo.transfers),
		)
	}
}

func TestCreateTransfer_FailedTransferReplay(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 100,
			},
			toWalletID: {
				ID:      toWalletID,
				Balance: 500,
			},
		},
	}

	transferRepo := newFakeTransferRepository()
	ledgerRepo := &fakeLedgerRepository{}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	req := domain.CreateTransfer{
		IdempotencyKey: "transfer-failed-replay-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	firstResult, err := svc.CreateTransfer(ctx, req)

	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf(
			"expected ErrInsufficientFunds, got %v",
			err,
		)
	}

	if firstResult == nil || firstResult.Transfer == nil {
		t.Fatal("expected failed transfer result")
	}

	originalTransferID := firstResult.Transfer.ID

	// Simulate money being added later.
	walletRepo.wallets[fromWalletID].Balance = 1000

	secondResult, err := svc.CreateTransfer(ctx, req)

	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf(
			"expected original ErrInsufficientFunds on replay, got %v",
			err,
		)
	}

	if secondResult == nil || secondResult.Transfer == nil {
		t.Fatal("expected replayed failed transfer result")
	}

	if !secondResult.Replayed {
		t.Fatal("expected second request to be replayed")
	}

	if secondResult.Transfer.ID != originalTransferID {
		t.Fatalf(
			"expected same transfer ID %s, got %s",
			originalTransferID,
			secondResult.Transfer.ID,
		)
	}

	if secondResult.Transfer.Status != domain.TransferStatusFailed {
		t.Fatalf(
			"expected FAILED status, got %s",
			secondResult.Transfer.Status,
		)
	}

	if secondResult.Transfer.FailureReason !=
		domain.TransferFailureInsufficientFunds {

		t.Fatalf(
			"expected failure reason %s, got %s",
			domain.TransferFailureInsufficientFunds,
			secondResult.Transfer.FailureReason,
		)
	}

	// Even though the source now has enough money,
	// replay must not execute the transfer again.
	if walletRepo.wallets[fromWalletID].Balance != 1000 {
		t.Fatalf(
			"expected source balance to remain 1000, got %d",
			walletRepo.wallets[fromWalletID].Balance,
		)
	}

	if walletRepo.wallets[toWalletID].Balance != 500 {
		t.Fatalf(
			"expected destination balance to remain 500, got %d",
			walletRepo.wallets[toWalletID].Balance,
		)
	}

	if len(ledgerRepo.entries) != 0 {
		t.Fatalf(
			"expected 0 ledger entries, got %d",
			len(ledgerRepo.entries),
		)
	}

	if len(transferRepo.transfers) != 1 {
		t.Fatalf(
			"expected exactly 1 transfer, got %d",
			len(transferRepo.transfers),
		)
	}
}

func TestCreateTransfer_MissingWallet(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 1000,
			},
			// destination wallet intentionally missing
		},
	}

	transferRepo := newFakeTransferRepository()
	ledgerRepo := &fakeLedgerRepository{}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	req := domain.CreateTransfer{
		IdempotencyKey: "transfer-missing-wallet-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	result, err := svc.CreateTransfer(ctx, req)

	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}

	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}

	if len(transferRepo.transfers) != 0 {
		t.Fatalf(
			"expected no transfer to be persisted, got %d",
			len(transferRepo.transfers),
		)
	}

	if len(ledgerRepo.entries) != 0 {
		t.Fatalf(
			"expected no ledger entries, got %d",
			len(ledgerRepo.entries),
		)
	}
}

func assertLedgerEntry(
	t *testing.T,
	entries []*domain.LedgerEntry,
	walletID uuid.UUID,
	entryType domain.LedgerEntryType,
	amount int64,
) {
	t.Helper()

	for _, entry := range entries {
		if entry.WalletID == walletID &&
			entry.Type == entryType &&
			entry.Amount == amount {

			return
		}
	}

	t.Fatalf(
		"expected ledger entry wallet=%s type=%s amount=%d",
		walletID,
		entryType,
		amount,
	)
}

func cloneWallets(
	src map[uuid.UUID]*domain.Wallet,
) map[uuid.UUID]*domain.Wallet {
	dst := make(map[uuid.UUID]*domain.Wallet, len(src))

	for id, w := range src {
		copyWallet := *w
		dst[id] = &copyWallet
	}

	return dst
}

func cloneTransfers(
	src map[string]*domain.Transfer,
) map[string]*domain.Transfer {
	dst := make(map[string]*domain.Transfer, len(src))

	for key, tr := range src {
		copyTransfer := *tr
		dst[key] = &copyTransfer
	}

	return dst
}

func cloneLedgerEntries(
	src []*domain.LedgerEntry,
) []*domain.LedgerEntry {
	dst := make([]*domain.LedgerEntry, 0, len(src))

	for _, entry := range src {
		copyEntry := *entry
		dst = append(dst, &copyEntry)
	}

	return dst
}

func TestCreateTransfer_RollsBackOnLedgerFailure(t *testing.T) {
	ctx := context.Background()

	fromWalletID := uuid.New()
	toWalletID := uuid.New()

	walletRepo := &fakeWalletRepository{
		wallets: map[uuid.UUID]*domain.Wallet{
			fromWalletID: {
				ID:      fromWalletID,
				Balance: 1000,
			},
			toWalletID: {
				ID:      toWalletID,
				Balance: 500,
			},
		},
	}

	transferRepo := newFakeTransferRepository()

	expectedErr := errors.New("ledger unavailable")

	ledgerRepo := &fakeLedgerRepository{
		createErr: expectedErr,
	}

	txRepos := &fakeTxRepositories{
		wallets:   walletRepo,
		transfers: transferRepo,
		ledger:    ledgerRepo,
	}

	txManager := &fakeTxManager{
		repos: txRepos,
	}

	svc := NewTransferService(
		txManager,
		transferRepo,
	)

	req := domain.CreateTransfer{
		IdempotencyKey: "transfer-ledger-failure-001",
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         200,
	}

	result, err := svc.CreateTransfer(ctx, req)

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"expected ledger error %v, got %v",
			expectedErr,
			err,
		)
	}

	if result != nil {
		t.Fatalf(
			"expected nil result after transaction failure, got %+v",
			result,
		)
	}

	// Debit must be rolled back.
	if walletRepo.wallets[fromWalletID].Balance != 1000 {
		t.Fatalf(
			"expected source balance 1000 after rollback, got %d",
			walletRepo.wallets[fromWalletID].Balance,
		)
	}

	// Credit must be rolled back.
	if walletRepo.wallets[toWalletID].Balance != 500 {
		t.Fatalf(
			"expected destination balance 500 after rollback, got %d",
			walletRepo.wallets[toWalletID].Balance,
		)
	}

	if len(ledgerRepo.entries) != 0 {
		t.Fatalf(
			"expected 0 ledger entries after rollback, got %d",
			len(ledgerRepo.entries),
		)
	}

	// PENDING transfer insert was part of the same transaction,
	// so it must also disappear.
	if len(transferRepo.transfers) != 0 {
		t.Fatalf(
			"expected 0 transfers after rollback, got %d",
			len(transferRepo.transfers),
		)
	}
}
