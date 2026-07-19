package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

const (
	testFromWalletID = "00000000-0000-0000-0000-000000000001"
	testToWalletID   = "00000000-0000-0000-0000-000000000002"
)

func TestCreateTransferProcessesHappyPath(t *testing.T) {
	store := newFakeStore()
	service := NewTransferService(store)

	result, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}

	if result.Transfer.Status != string(domain.TransferStatusProcessed) {
		t.Fatalf("status = %q, want %q", result.Transfer.Status, domain.TransferStatusProcessed)
	}
	if store.wallets[testFromWalletID].Balance != 900 {
		t.Fatalf("from wallet balance = %d, want 900", store.wallets[testFromWalletID].Balance)
	}
	if store.wallets[testToWalletID].Balance != 100 {
		t.Fatalf("to wallet balance = %d, want 100", store.wallets[testToWalletID].Balance)
	}
	if len(store.ledgerEntries) != 2 {
		t.Fatalf("ledger entry count = %d, want 2", len(store.ledgerEntries))
	}
}

func TestCreateTransferReplaysCompletedIdempotencyRecord(t *testing.T) {
	store := newFakeStore()
	service := NewTransferService(store)

	first, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}
	second, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("second CreateTransfer returned error: %v", err)
	}

	if !second.Replayed {
		t.Fatal("second request was not marked as replayed")
	}
	if second.Transfer != first.Transfer {
		t.Fatalf("replayed response differs: got %+v want %+v", second.Transfer, first.Transfer)
	}
	if len(store.ledgerEntries) != 2 {
		t.Fatalf("ledger entry count = %d, want 2", len(store.ledgerEntries))
	}
}

func TestCreateTransferRejectsIdempotencyConflict(t *testing.T) {
	store := newFakeStore()
	service := NewTransferService(store)

	_, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}
	_, err = service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         200,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want %v", err, domain.ErrIdempotencyConflict)
	}
}

func TestCreateTransferFailsTerminallyForInsufficientFunds(t *testing.T) {
	store := newFakeStore()
	service := NewTransferService(store)

	result, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         2000,
	})
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}

	if result.Transfer.Status != string(domain.TransferStatusFailed) {
		t.Fatalf("status = %q, want %q", result.Transfer.Status, domain.TransferStatusFailed)
	}
	if result.Transfer.FailureReason == nil || *result.Transfer.FailureReason != domain.FailureInsufficientFunds {
		t.Fatalf("failure reason = %v, want %q", result.Transfer.FailureReason, domain.FailureInsufficientFunds)
	}
	if len(store.ledgerEntries) != 0 {
		t.Fatalf("ledger entry count = %d, want 0", len(store.ledgerEntries))
	}
	if store.wallets[testFromWalletID].Balance != 1000 {
		t.Fatalf("from wallet balance = %d, want 1000", store.wallets[testFromWalletID].Balance)
	}
}

func TestCreateTransferRollsBackIdempotencyOnWalletNotFound(t *testing.T) {
	store := newFakeStore()
	service := NewTransferService(store)

	_, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "00000000-0000-0000-0000-000000000999",
		ToWalletID:     testToWalletID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("err = %v, want %v", err, domain.ErrWalletNotFound)
	}
	if _, ok := store.idempotencyRecords["key-1"]; ok {
		t.Fatal("idempotency record was kept after wallet validation failure")
	}
}

type fakeStore struct {
	mu                 sync.Mutex
	nextTransferID     int
	nextLedgerEntryID  int64
	wallets            map[string]domain.Wallet
	transfers          map[string]domain.Transfer
	idempotencyRecords map[string]domain.IdempotencyRecord
	ledgerEntries      []domain.LedgerEntry
}

func newFakeStore() *fakeStore {
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	return &fakeStore{
		nextTransferID:    1,
		nextLedgerEntryID: 1,
		wallets: map[string]domain.Wallet{
			testFromWalletID: {
				ID:        testFromWalletID,
				OwnerID:   "owner-1",
				Currency:  domain.DefaultCurrency,
				Balance:   1000,
				Status:    domain.WalletStatusActive,
				CreatedAt: now,
				UpdatedAt: now,
			},
			testToWalletID: {
				ID:        testToWalletID,
				OwnerID:   "owner-2",
				Currency:  domain.DefaultCurrency,
				Balance:   0,
				Status:    domain.WalletStatusActive,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		transfers:          make(map[string]domain.Transfer),
		idempotencyRecords: make(map[string]domain.IdempotencyRecord),
		ledgerEntries:      make([]domain.LedgerEntry, 0),
	}
}

func (store *fakeStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx repository.Tx) error) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	snapshot := store.clone()
	tx := &fakeTx{store: store}
	if err := fn(ctx, tx); err != nil {
		store.restore(snapshot)
		return err
	}
	return nil
}

func (store *fakeStore) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	transfer, ok := store.transfers[id]
	if !ok {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return transfer, nil
}

func (store *fakeStore) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	wallet, ok := store.wallets[id]
	if !ok {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return wallet, nil
}

func (store *fakeStore) ListWalletLedger(ctx context.Context, walletID string, limit int, cursor int64) ([]domain.LedgerEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entries := make([]domain.LedgerEntry, 0)
	for _, entry := range store.ledgerEntries {
		if entry.WalletID != walletID {
			continue
		}
		if cursor > 0 && entry.ID >= cursor {
			continue
		}
		entries = append(entries, entry)
		if len(entries) == limit {
			break
		}
	}
	return entries, nil
}

func (store *fakeStore) clone() *fakeStore {
	snapshot := &fakeStore{
		nextTransferID:     store.nextTransferID,
		nextLedgerEntryID:  store.nextLedgerEntryID,
		wallets:            make(map[string]domain.Wallet, len(store.wallets)),
		transfers:          make(map[string]domain.Transfer, len(store.transfers)),
		idempotencyRecords: make(map[string]domain.IdempotencyRecord, len(store.idempotencyRecords)),
		ledgerEntries:      append([]domain.LedgerEntry(nil), store.ledgerEntries...),
	}
	for key, value := range store.wallets {
		snapshot.wallets[key] = value
	}
	for key, value := range store.transfers {
		snapshot.transfers[key] = value
	}
	for key, value := range store.idempotencyRecords {
		snapshot.idempotencyRecords[key] = cloneIdempotencyRecord(value)
	}
	return snapshot
}

func (store *fakeStore) restore(snapshot *fakeStore) {
	store.nextTransferID = snapshot.nextTransferID
	store.nextLedgerEntryID = snapshot.nextLedgerEntryID
	store.wallets = snapshot.wallets
	store.transfers = snapshot.transfers
	store.idempotencyRecords = snapshot.idempotencyRecords
	store.ledgerEntries = snapshot.ledgerEntries
}

type fakeTx struct {
	store *fakeStore
}

func (tx *fakeTx) InsertIdempotencyRecord(ctx context.Context, key string, fingerprint string) (bool, error) {
	if _, ok := tx.store.idempotencyRecords[key]; ok {
		return false, nil
	}
	now := time.Now().UTC()
	tx.store.idempotencyRecords[key] = domain.IdempotencyRecord{
		Key:                key,
		RequestFingerprint: fingerprint,
		Status:             domain.IdempotencyStatusInProgress,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	return true, nil
}

func (tx *fakeTx) GetIdempotencyRecord(ctx context.Context, key string) (domain.IdempotencyRecord, error) {
	record, ok := tx.store.idempotencyRecords[key]
	if !ok {
		return domain.IdempotencyRecord{}, domain.ErrRequestInProgress
	}
	return cloneIdempotencyRecord(record), nil
}

func (tx *fakeTx) SetIdempotencyTransfer(ctx context.Context, key string, transferID string) error {
	record, ok := tx.store.idempotencyRecords[key]
	if !ok {
		return domain.ErrRequestInProgress
	}
	record.TransferID = &transferID
	record.UpdatedAt = time.Now().UTC()
	tx.store.idempotencyRecords[key] = record
	return nil
}

func (tx *fakeTx) CompleteIdempotencyRecord(ctx context.Context, key string, transferID string, responseSnapshot []byte) error {
	record, ok := tx.store.idempotencyRecords[key]
	if !ok {
		return domain.ErrRequestInProgress
	}
	record.TransferID = &transferID
	record.Status = domain.IdempotencyStatusCompleted
	record.ResponseSnapshot = append([]byte(nil), responseSnapshot...)
	record.UpdatedAt = time.Now().UTC()
	tx.store.idempotencyRecords[key] = record
	return nil
}

func (tx *fakeTx) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	wallet, ok := tx.store.wallets[id]
	if !ok {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return wallet, nil
}

func (tx *fakeTx) InsertPendingTransfer(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error) {
	now := time.Now().UTC()
	transfer.ID = fmt.Sprintf("transfer-%d", tx.store.nextTransferID)
	tx.store.nextTransferID++
	transfer.Status = domain.TransferStatusPending
	transfer.CreatedAt = now
	transfer.UpdatedAt = now
	tx.store.transfers[transfer.ID] = transfer
	return transfer, nil
}

func (tx *fakeTx) LockTransferForProcessing(ctx context.Context, id string) (domain.Transfer, error) {
	transfer, ok := tx.store.transfers[id]
	if !ok {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return transfer, nil
}

func (tx *fakeTx) CompleteTransfer(ctx context.Context, transfer domain.Transfer) error {
	existing, ok := tx.store.transfers[transfer.ID]
	if !ok {
		return domain.ErrTransferNotFound
	}
	if existing.Status != domain.TransferStatusPending {
		return domain.ErrInvalidState
	}
	transfer.UpdatedAt = time.Now().UTC()
	tx.store.transfers[transfer.ID] = transfer
	return nil
}

func (tx *fakeTx) LockWalletsForTransfer(ctx context.Context, fromWalletID string, toWalletID string) (domain.Wallet, domain.Wallet, error) {
	fromWallet, ok := tx.store.wallets[fromWalletID]
	if !ok {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	toWallet, ok := tx.store.wallets[toWalletID]
	if !ok {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	return fromWallet, toWallet, nil
}

func (tx *fakeTx) UpdateWalletBalances(ctx context.Context, fromWallet domain.Wallet, toWallet domain.Wallet) error {
	if _, ok := tx.store.wallets[fromWallet.ID]; !ok {
		return domain.ErrWalletNotFound
	}
	if _, ok := tx.store.wallets[toWallet.ID]; !ok {
		return domain.ErrWalletNotFound
	}
	tx.store.wallets[fromWallet.ID] = fromWallet
	tx.store.wallets[toWallet.ID] = toWallet
	return nil
}

func (tx *fakeTx) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	seen := make(map[string]struct{})
	for _, existing := range tx.store.ledgerEntries {
		seen[ledgerUniqKey(existing)] = struct{}{}
	}
	for _, entry := range entries {
		key := ledgerUniqKey(entry)
		if _, ok := seen[key]; ok {
			return errors.New("duplicate ledger entry")
		}
		seen[key] = struct{}{}
		entry.ID = tx.store.nextLedgerEntryID
		tx.store.nextLedgerEntryID++
		entry.CreatedAt = time.Now().UTC()
		tx.store.ledgerEntries = append(tx.store.ledgerEntries, entry)
	}
	return nil
}

func ledgerUniqKey(entry domain.LedgerEntry) string {
	return entry.TransferID + "|" + entry.WalletID + "|" + string(entry.Type)
}

func cloneIdempotencyRecord(record domain.IdempotencyRecord) domain.IdempotencyRecord {
	clone := record
	if record.TransferID != nil {
		transferID := *record.TransferID
		clone.TransferID = &transferID
	}
	clone.ResponseSnapshot = append([]byte(nil), record.ResponseSnapshot...)
	return clone
}

func mustSnapshot(t *testing.T, result TransferResult) []byte {
	t.Helper()

	snapshot, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return snapshot
}
