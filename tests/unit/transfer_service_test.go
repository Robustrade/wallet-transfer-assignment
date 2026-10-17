package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// inMemoryRepo implements repository.WalletRepository for thorough unit testing of service logic.
type inMemoryRepo struct {
	mu           sync.Mutex
	wallets      map[string]*domain.Wallet
	transfers    map[string]*domain.Transfer
	ledgers      []*domain.LedgerEntry
	idempotency  map[string]*domain.IdempotencyRecord
	simulateFail bool
}

func newInMemoryRepo() *inMemoryRepo {
	return &inMemoryRepo{
		wallets: map[string]*domain.Wallet{
			"wallet_1": {ID: "wallet_1", UserID: "user_1", Balance: 10000, Currency: "INR", CreatedAt: time.Now(), UpdatedAt: time.Now()},
			"wallet_2": {ID: "wallet_2", UserID: "user_2", Balance: 5000, Currency: "INR", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
		transfers:   make(map[string]*domain.Transfer),
		ledgers:     make([]*domain.LedgerEntry, 0),
		idempotency: make(map[string]*domain.IdempotencyRecord),
	}
}

func (m *inMemoryRepo) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, exists := m.wallets[id]
	if !exists {
		return nil, domain.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (m *inMemoryRepo) GetWalletsForUpdate(ctx context.Context, ids ...string) ([]*domain.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*domain.Wallet
	for _, id := range ids {
		if w, ok := m.wallets[id]; ok {
			cp := *w
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *inMemoryRepo) DebitWalletConditional(ctx context.Context, walletID string, amount int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[walletID]
	if !ok {
		return 0, nil
	}
	if w.Balance < amount {
		return 0, nil // Insufficient funds
	}
	w.Balance -= amount
	w.UpdatedAt = time.Now()
	return 1, nil
}

func (m *inMemoryRepo) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[walletID]
	if !ok {
		return domain.ErrWalletNotFound
	}
	w.Balance += amount
	w.UpdatedAt = time.Now()
	return nil
}

func (m *inMemoryRepo) CreateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *transfer
	m.transfers[transfer.ID] = &cp
	return nil
}

func (m *inMemoryRepo) UpdateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *transfer
	m.transfers[transfer.ID] = &cp
	return nil
}

func (m *inMemoryRepo) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *inMemoryRepo) CreateLedgerEntries(ctx context.Context, entries ...*domain.LedgerEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.simulateFail {
		return fmt.Errorf("simulated failure after debit")
	}
	for _, e := range entries {
		cp := *e
		cp.ID = int64(len(m.ledgers) + 1)
		m.ledgers = append(m.ledgers, &cp)
	}
	return nil
}

func (m *inMemoryRepo) GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []*domain.LedgerEntry
	for _, e := range m.ledgers {
		if e.TransferID == transferID {
			cp := *e
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *inMemoryRepo) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.idempotency[key]
	if !ok {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

func (m *inMemoryRepo) CreateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.idempotency[record.IdempotencyKey]; ok {
		return fmt.Errorf("duplicate key value violates unique constraint 23505")
	}
	cp := *record
	m.idempotency[record.IdempotencyKey] = &cp
	return nil
}

func (m *inMemoryRepo) UpdateIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *record
	m.idempotency[record.IdempotencyKey] = &cp
	return nil
}

func (m *inMemoryRepo) WithTransaction(ctx context.Context, fn func(txRepo repository.WalletRepository) error) error {
	m.mu.Lock()
	// Snapshot state for rollback simulation
	walletsSnap := make(map[string]domain.Wallet)
	for k, v := range m.wallets {
		walletsSnap[k] = *v
	}
	transfersSnap := make(map[string]domain.Transfer)
	for k, v := range m.transfers {
		transfersSnap[k] = *v
	}
	ledgersSnap := append([]*domain.LedgerEntry(nil), m.ledgers...)
	idempotencySnap := make(map[string]domain.IdempotencyRecord)
	for k, v := range m.idempotency {
		idempotencySnap[k] = *v
	}
	m.mu.Unlock()

	err := fn(m)
	if err != nil {
		// Roll back to snapshot on error
		m.mu.Lock()
		m.wallets = make(map[string]*domain.Wallet)
		for k, v := range walletsSnap {
			cp := v
			m.wallets[k] = &cp
		}
		m.transfers = make(map[string]*domain.Transfer)
		for k, v := range transfersSnap {
			cp := v
			m.transfers[k] = &cp
		}
		m.ledgers = ledgersSnap
		m.idempotency = make(map[string]*domain.IdempotencyRecord)
		for k, v := range idempotencySnap {
			cp := v
			m.idempotency[k] = &cp
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *inMemoryRepo) Ping(ctx context.Context) error {
	return nil
}

func TestTransferService_SuccessfulTransfer(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-success-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         3000, // ₹30.00
	}

	result, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, result.StatusCode)
	}
	if result.Replayed {
		t.Errorf("expected replayed to be false for new transfer")
	}

	// Verify wallet balances: wallet_1 was 10000 - 3000 = 7000; wallet_2 was 5000 + 3000 = 8000
	w1, _ := repo.GetWallet(context.Background(), "wallet_1")
	w2, _ := repo.GetWallet(context.Background(), "wallet_2")
	if w1.Balance != 7000 {
		t.Errorf("expected wallet_1 balance 7000, got %d", w1.Balance)
	}
	if w2.Balance != 8000 {
		t.Errorf("expected wallet_2 balance 8000, got %d", w2.Balance)
	}

	// Verify double-entry ledger: exactly 2 entries, 1 debit, 1 credit, equal amounts
	if len(repo.ledgers) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(repo.ledgers))
	}
	e1 := repo.ledgers[0]
	e2 := repo.ledgers[1]
	if e1.EntryType != domain.LedgerEntryDebit || e1.WalletID != "wallet_1" || e1.Amount != 3000 {
		t.Errorf("invalid debit entry: %+v", e1)
	}
	if e2.EntryType != domain.LedgerEntryCredit || e2.WalletID != "wallet_2" || e2.Amount != 3000 {
		t.Errorf("invalid credit entry: %+v", e2)
	}
}

func TestTransferService_InsufficientFunds(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-insufficient-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         20000, // More than wallet_1's 10000 balance
	}

	result, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("expected graceful 409 conflict result, got error: %v", err)
	}

	if result.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", result.StatusCode)
	}

	// Balances must remain unchanged
	w1, _ := repo.GetWallet(context.Background(), "wallet_1")
	w2, _ := repo.GetWallet(context.Background(), "wallet_2")
	if w1.Balance != 10000 {
		t.Errorf("expected wallet_1 balance 10000, got %d", w1.Balance)
	}
	if w2.Balance != 5000 {
		t.Errorf("expected wallet_2 balance 5000, got %d", w2.Balance)
	}

	// No ledger entries created
	if len(repo.ledgers) != 0 {
		t.Errorf("expected 0 ledger entries on failed transfer, got %d", len(repo.ledgers))
	}

	// Transfer record exists in FAILED state
	if len(repo.transfers) != 1 {
		t.Fatalf("expected 1 transfer record, got %d", len(repo.transfers))
	}
	for _, tr := range repo.transfers {
		if tr.Status != domain.TransferStatusFailed {
			t.Errorf("expected transfer status FAILED, got %s", tr.Status)
		}
		if tr.FailureCode == nil || *tr.FailureCode != domain.FailureCodeInsufficientFunds {
			t.Errorf("expected failure code INSUFFICIENT_FUNDS, got %v", tr.FailureCode)
		}
	}
}

func TestTransferService_IdempotencyReplay(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-repeat-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         2000,
	}

	// First execution
	res1, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error on first transfer: %v", err)
	}
	if res1.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res1.StatusCode)
	}

	// Second execution with same key and same payload
	res2, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error on repeated transfer: %v", err)
	}
	if res2.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 on replay, got %d", res2.StatusCode)
	}
	if !res2.Replayed {
		t.Errorf("expected replayed to be true")
	}

	// Balances should be deducted only once!
	w1, _ := repo.GetWallet(context.Background(), "wallet_1")
	if w1.Balance != 8000 {
		t.Errorf("expected wallet_1 balance 8000 after single debit, got %d", w1.Balance)
	}

	// Exactly 2 ledger entries
	if len(repo.ledgers) != 2 {
		t.Errorf("expected exactly 2 ledger entries, got %d", len(repo.ledgers))
	}
}

func TestTransferService_IdempotencyReplay_FailedTransfer(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-repeat-fail",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50000, // Insufficient
	}

	res1, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res1.StatusCode)
	}

	// Replay request with same key
	res2, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 on replay, got %d", res2.StatusCode)
	}
	if !res2.Replayed {
		t.Errorf("expected replayed to be true")
	}
}

func TestTransferService_IdempotencyPayloadMismatch(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req1 := &service.CreateTransferRequest{
		IdempotencyKey: "shared-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1000,
	}

	_, err := svc.CreateTransfer(context.Background(), req1)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	// Reuse same key with different amount
	req2 := &service.CreateTransferRequest{
		IdempotencyKey: "shared-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         2000, // Changed!
	}

	_, err = svc.CreateTransfer(context.Background(), req2)
	if err == nil {
		t.Fatalf("expected conflict error on payload mismatch, got nil")
	}

	var de *domain.DomainError
	if !domain.IsDomainError(err) {
		t.Fatalf("expected domain error, got %v", err)
	}
	_ = json.Unmarshal([]byte{}, &de)
	if err.Error() != domain.ErrIdempotencyKeyReuseMismatch.Error() {
		t.Errorf("expected %v, got %v", domain.ErrIdempotencyKeyReuseMismatch, err)
	}
}

func TestTransferService_SameWalletRejected(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-same",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         1000,
	}

	_, err := svc.CreateTransfer(context.Background(), req)
	if err != domain.ErrSameWalletTransfer {
		t.Errorf("expected ErrSameWalletTransfer, got %v", err)
	}
}

func TestTransferService_MissingWallet(t *testing.T) {
	repo := newInMemoryRepo()
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-missing",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_nonexistent",
		Amount:         1000,
	}

	_, err := svc.CreateTransfer(context.Background(), req)
	if err != domain.ErrWalletNotFound {
		t.Errorf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestTransferService_RollbackOnFailure(t *testing.T) {
	repo := newInMemoryRepo()
	repo.simulateFail = true // will fail inside transaction during CreateLedgerEntries
	svc := service.NewTransferService(repo)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "key-rollback",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1000,
	}

	_, err := svc.CreateTransfer(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error from simulated failure, got nil")
	}

	// Verify complete rollback: wallet_1 balance must not be debited!
	w1, _ := repo.GetWallet(context.Background(), "wallet_1")
	if w1.Balance != 10000 {
		t.Errorf("expected balance to remain 10000 after rollback, got %d", w1.Balance)
	}

	// No orphaned transfers or ledger entries
	if len(repo.ledgers) != 0 {
		t.Errorf("expected 0 ledger entries after rollback, got %d", len(repo.ledgers))
	}
	if len(repo.transfers) != 0 {
		t.Errorf("expected 0 transfers after rollback, got %d", len(repo.transfers))
	}
}

func TestTransferService_NilRequestRejected(t *testing.T) {
	svc := service.NewTransferService(nil)
	if _, err := svc.CreateTransfer(context.Background(), nil); err == nil {
		t.Fatal("expected nil request to be rejected")
	}
}

func TestTransferService_RejectsOversizedIdentifiers(t *testing.T) {
	svc := service.NewTransferService(nil)
	tests := []struct {
		name string
		req  *service.CreateTransferRequest
	}{
		{"idempotency key", &service.CreateTransferRequest{IdempotencyKey: strings.Repeat("k", 256), FromWalletID: "a", ToWalletID: "b", Amount: 1}},
		{"source wallet ID", &service.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: strings.Repeat("a", 65), ToWalletID: "b", Amount: 1}},
		{"destination wallet ID", &service.CreateTransferRequest{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: strings.Repeat("b", 65), Amount: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.CreateTransfer(context.Background(), tt.req); err == nil {
				t.Fatal("expected oversized identifier to be rejected")
			}
		})
	}
}
