package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"gorm.io/gorm"
)

type mockWalletRepo struct {
	wallets map[string]*model.Wallet
}

func (m *mockWalletRepo) GetWalletForUpdate(ctx context.Context, tx *gorm.DB, walletID string) (*model.Wallet, error) {
	wallet, ok := m.wallets[walletID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return wallet, nil
}

func (m *mockWalletRepo) UpdateBalance(ctx context.Context, tx *gorm.DB, walletID string, newBalance int64) error {
	wallet, ok := m.wallets[walletID]
	if !ok {
		return repository.ErrNotFound
	}
	wallet.Balance = newBalance
	wallet.UpdatedAt = time.Now().UTC()
	return nil
}

type mockTransferRepo struct {
	transfers map[string]*model.Transfer
}

func (m *mockTransferRepo) CreateTransfer(ctx context.Context, tx *gorm.DB, transfer model.Transfer) error {
	if _, exists := m.transfers[transfer.TransferID]; exists {
		return errors.New("duplicate transfer")
	}
	stored := transfer
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	m.transfers[transfer.TransferID] = &stored
	return nil
}

func (m *mockTransferRepo) GetByID(ctx context.Context, tx *gorm.DB, transferID string) (*model.Transfer, error) {
	transfer, ok := m.transfers[transferID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return transfer, nil
}

func (m *mockTransferRepo) UpdateTransferState(ctx context.Context, tx *gorm.DB, transferID string, state model.TransferState) error {
	transfer, ok := m.transfers[transferID]
	if !ok {
		return repository.ErrNotFound
	}
	transfer.State = state
	transfer.UpdatedAt = time.Now().UTC()
	return nil
}

type mockLedgerRepo struct {
	entries []model.LedgerEntry
}

func (m *mockLedgerRepo) CreateEntries(ctx context.Context, tx *gorm.DB, entries []model.LedgerEntry) error {
	m.entries = append(m.entries, entries...)
	return nil
}

type mockIdempotencyRepo struct {
	records map[string]*model.IdempotencyRecord
}

func (m *mockIdempotencyRepo) GetByKey(ctx context.Context, tx *gorm.DB, key string) (*model.IdempotencyRecord, error) {
	record, ok := m.records[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return record, nil
}

func (m *mockIdempotencyRepo) Create(ctx context.Context, tx *gorm.DB, record model.IdempotencyRecord) error {
	if _, exists := m.records[record.IdempotencyKey]; exists {
		return repository.ErrUniqueViolation
	}
	stored := record
	m.records[record.IdempotencyKey] = &stored
	return nil
}

// directTx runs the transactional body without a database, so the real
// CreateTransfer business logic is exercised by unit tests.
func directTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return fn(nil)
}

func newTestService(walletRepo repository.WalletRepository, idempotencyRepo repository.IdempotencyRepository) (*transferService, *mockTransferRepo, *mockLedgerRepo) {
	transferRepo := &mockTransferRepo{transfers: map[string]*model.Transfer{}}
	ledgerRepo := &mockLedgerRepo{}
	svc := &transferService{
		withTx:          directTx,
		walletRepo:      walletRepo,
		transferRepo:    transferRepo,
		ledgerRepo:      ledgerRepo,
		idempotencyRepo: idempotencyRepo,
	}
	return svc, transferRepo, ledgerRepo
}

func TestCreateTransferSucceeds(t *testing.T) {
	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 500},
		"wallet_2": {WalletID: "wallet_2", Balance: 100},
	}}
	idempotencyRepo := &mockIdempotencyRepo{records: map[string]*model.IdempotencyRecord{}}
	svc, _, ledgerRepo := newTestService(walletRepo, idempotencyRepo)

	response, err := svc.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if response.State != model.TransferStateProcessed {
		t.Fatalf("expected PROCESSED, got %s", response.State)
	}
	if response.FromWalletBalance != 300 || response.ToWalletBalance != 300 {
		t.Fatalf("expected updated balances, got %d and %d", response.FromWalletBalance, response.ToWalletBalance)
	}
	if len(ledgerRepo.entries) != 2 {
		t.Fatalf("expected two ledger entries, got %d", len(ledgerRepo.entries))
	}
	if ledgerRepo.entries[0].EntryType != model.EntryTypeDebit || ledgerRepo.entries[1].EntryType != model.EntryTypeCredit {
		t.Fatalf("expected debit then credit entries")
	}
}

func TestCreateTransferReturnsSameResultForDuplicateIdempotencyKey(t *testing.T) {
	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 500},
		"wallet_2": {WalletID: "wallet_2", Balance: 100},
	}}
	idempotencyRepo := &mockIdempotencyRepo{records: map[string]*model.IdempotencyRecord{}}
	svc, _, ledgerRepo := newTestService(walletRepo, idempotencyRepo)

	request := CreateTransferRequest{
		IdempotencyKey: "duplicate-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         150,
	}

	first, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}
	second, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("duplicate transfer failed: %v", err)
	}

	if first.TransferID != second.TransferID {
		t.Fatalf("expected same transfer id for duplicate request")
	}
	if len(ledgerRepo.entries) != 2 {
		t.Fatalf("expected only two ledger entries, got %d", len(ledgerRepo.entries))
	}
}

// TestDuplicateReturnsOriginalSnapshot proves exactly-once semantics: a replayed
// request returns the balances captured at execution time, even if the wallets
// have since changed.
func TestDuplicateReturnsOriginalSnapshot(t *testing.T) {
	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 500},
		"wallet_2": {WalletID: "wallet_2", Balance: 100},
	}}
	idempotencyRepo := &mockIdempotencyRepo{records: map[string]*model.IdempotencyRecord{}}
	svc, _, _ := newTestService(walletRepo, idempotencyRepo)

	request := CreateTransferRequest{
		IdempotencyKey: "snapshot-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	// Simulate later activity changing the wallet balances.
	walletRepo.wallets["wallet_1"].Balance = 9999
	walletRepo.wallets["wallet_2"].Balance = 8888

	replay, err := svc.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if replay.FromWalletBalance != first.FromWalletBalance || replay.ToWalletBalance != first.ToWalletBalance {
		t.Fatalf("replay should return original snapshot balances, got from=%d to=%d", replay.FromWalletBalance, replay.ToWalletBalance)
	}
}

func TestCreateTransferFailsForInsufficientFunds(t *testing.T) {
	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 50},
		"wallet_2": {WalletID: "wallet_2", Balance: 100},
	}}
	idempotencyRepo := &mockIdempotencyRepo{records: map[string]*model.IdempotencyRecord{}}
	svc, _, ledgerRepo := newTestService(walletRepo, idempotencyRepo)

	request := CreateTransferRequest{
		IdempotencyKey: "insufficient-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	}

	resp, err := svc.CreateTransfer(context.Background(), request)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds error, got %v", err)
	}
	if resp.State != model.TransferStateFailed {
		t.Fatalf("expected FAILED state, got %s", resp.State)
	}
	if len(ledgerRepo.entries) != 0 {
		t.Fatalf("expected no ledger entries for failed transfer, got %d", len(ledgerRepo.entries))
	}

	// Replaying the same key must return the same FAILED outcome.
	replay, err := svc.CreateTransfer(context.Background(), request)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds on replay, got %v", err)
	}
	if replay.TransferID != resp.TransferID {
		t.Fatalf("expected same transfer id on replay")
	}
}

func TestCreateTransferRejectsSelfTransfer(t *testing.T) {
	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 500},
	}}
	idempotencyRepo := &mockIdempotencyRepo{records: map[string]*model.IdempotencyRecord{}}
	svc, _, _ := newTestService(walletRepo, idempotencyRepo)

	_, err := svc.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "self-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         100,
	})
	if !errors.Is(err, ErrInvalidTransferRequest) {
		t.Fatalf("expected invalid request for self transfer, got %v", err)
	}
}

func TestCreateTransferUnknownWalletReturnsNotFound(t *testing.T) {
	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 500},
	}}
	idempotencyRepo := &mockIdempotencyRepo{records: map[string]*model.IdempotencyRecord{}}
	svc, _, _ := newTestService(walletRepo, idempotencyRepo)

	_, err := svc.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "missing-wallet",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_missing",
		Amount:         100,
	})
	if !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf("expected wallet not found, got %v", err)
	}
}

// conflictIdempotencyRepo simulates a concurrent duplicate that committed first:
// the initial pre-check misses, the insert conflicts, and the replay lookup finds
// the winner's stored result.
type conflictIdempotencyRepo struct {
	winner *model.IdempotencyRecord
	seen   bool
}

func (m *conflictIdempotencyRepo) GetByKey(ctx context.Context, tx *gorm.DB, key string) (*model.IdempotencyRecord, error) {
	if !m.seen {
		m.seen = true
		return nil, repository.ErrNotFound
	}
	return m.winner, nil
}

func (m *conflictIdempotencyRepo) Create(ctx context.Context, tx *gorm.DB, record model.IdempotencyRecord) error {
	return repository.ErrUniqueViolation
}

func TestConcurrentDuplicateReplaysWinnerResult(t *testing.T) {
	winnerResponse := TransferResponse{
		TransferID:        "winner-transfer",
		FromWalletID:      "wallet_1",
		ToWalletID:        "wallet_2",
		Amount:            100,
		State:             model.TransferStateProcessed,
		FromWalletBalance: 400,
		ToWalletBalance:   200,
	}
	payload, err := json.Marshal(winnerResponse)
	if err != nil {
		t.Fatalf("marshal winner response: %v", err)
	}

	walletRepo := &mockWalletRepo{wallets: map[string]*model.Wallet{
		"wallet_1": {WalletID: "wallet_1", Balance: 500},
		"wallet_2": {WalletID: "wallet_2", Balance: 100},
	}}
	idempotencyRepo := &conflictIdempotencyRepo{winner: &model.IdempotencyRecord{
		IdempotencyKey: "race-key",
		TransferID:     winnerResponse.TransferID,
		Response:       string(payload),
	}}
	svc, _, _ := newTestService(walletRepo, idempotencyRepo)

	resp, err := svc.CreateTransfer(context.Background(), CreateTransferRequest{
		IdempotencyKey: "race-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("expected conflict to resolve to winner result, got %v", err)
	}
	if resp.TransferID != winnerResponse.TransferID {
		t.Fatalf("expected winner transfer id %q, got %q", winnerResponse.TransferID, resp.TransferID)
	}
}
