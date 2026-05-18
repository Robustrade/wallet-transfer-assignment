package tests

import (
	"context"
	"errors"
	"sync"
	"testing"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"
)

func TestCreateTransfer_ProcessedCreatesLedgerAndUpdatesBalances(t *testing.T) {
	store := newFakeStore(map[string]int64{
		"wallet_1": 1000,
		"wallet_2": 500,
	})

	svc := service.NewTransferService(store)

	result, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "key_1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.StatusCode != service.StatusCreated {
		t.Fatalf("expected status code %d, got %d", service.StatusCreated, result.StatusCode)
	}

	if result.Response.Status != string(domain.TransferStatusProcessed) {
		t.Fatalf("expected transfer status PROCESSED, got %s", result.Response.Status)
	}

	if result.Replayed {
		t.Fatalf("expected first request not to be replayed")
	}

	if store.wallets["wallet_1"].Balance != 900 {
		t.Fatalf("expected wallet_1 balance 900, got %d", store.wallets["wallet_1"].Balance)
	}

	if store.wallets["wallet_2"].Balance != 600 {
		t.Fatalf("expected wallet_2 balance 600, got %d", store.wallets["wallet_2"].Balance)
	}

	if len(store.transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(store.transfers))
	}

	if len(store.ledgerEntries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(store.ledgerEntries))
	}

	assertLedgerBalanced(t, store.ledgerEntries, result.Response.TransferID, 100)
}

func TestCreateTransfer_IdempotencyReplayReturnsOriginalResponseWithoutDuplicateSideEffects(t *testing.T) {
	store := newFakeStore(map[string]int64{
		"wallet_1": 1000,
		"wallet_2": 500,
	})

	svc := service.NewTransferService(store)

	first, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "same_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	second, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "same_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	if !second.Replayed {
		t.Fatalf("expected second request to be replayed")
	}

	if first.Response.TransferID != second.Response.TransferID {
		t.Fatalf(
			"expected same transfer id for replay, first=%s second=%s",
			first.Response.TransferID,
			second.Response.TransferID,
		)
	}

	if len(store.transfers) != 1 {
		t.Fatalf("expected only 1 transfer after replay, got %d", len(store.transfers))
	}

	if len(store.ledgerEntries) != 2 {
		t.Fatalf("expected only 2 ledger entries after replay, got %d", len(store.ledgerEntries))
	}

	if store.wallets["wallet_1"].Balance != 900 {
		t.Fatalf("expected wallet_1 to be debited once, got %d", store.wallets["wallet_1"].Balance)
	}

	if store.wallets["wallet_2"].Balance != 600 {
		t.Fatalf("expected wallet_2 to be credited once, got %d", store.wallets["wallet_2"].Balance)
	}
}

func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	store := newFakeStore(map[string]int64{
		"wallet_1": 1000,
		"wallet_2": 500,
		"wallet_3": 200,
	})

	svc := service.NewTransferService(store)

	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "conflict_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	_, err = svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "conflict_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_3",
		Amount:         100,
	})

	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	if len(store.transfers) != 1 {
		t.Fatalf("expected no second transfer, got %d transfers", len(store.transfers))
	}

	if len(store.ledgerEntries) != 2 {
		t.Fatalf("expected no extra ledger entries, got %d", len(store.ledgerEntries))
	}
}

func TestCreateTransfer_InsufficientFundsCreatesFailedTransferWithoutLedger(t *testing.T) {
	store := newFakeStore(map[string]int64{
		"wallet_1": 50,
		"wallet_2": 500,
	})

	svc := service.NewTransferService(store)

	result, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
		IdempotencyKey: "insufficient_key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("expected no transport-level error, got %v", err)
	}

	if result.StatusCode != service.StatusUnprocessableEntity {
		t.Fatalf("expected status code %d, got %d", service.StatusUnprocessableEntity, result.StatusCode)
	}

	if result.Response.Status != string(domain.TransferStatusFailed) {
		t.Fatalf("expected FAILED transfer, got %s", result.Response.Status)
	}

	if result.Response.Error != domain.ErrInsufficientFunds.Error() {
		t.Fatalf("expected insufficient funds error, got %s", result.Response.Error)
	}

	if store.wallets["wallet_1"].Balance != 50 {
		t.Fatalf("expected wallet_1 balance unchanged, got %d", store.wallets["wallet_1"].Balance)
	}

	if store.wallets["wallet_2"].Balance != 500 {
		t.Fatalf("expected wallet_2 balance unchanged, got %d", store.wallets["wallet_2"].Balance)
	}

	if len(store.ledgerEntries) != 0 {
		t.Fatalf("expected no ledger entries for failed transfer, got %d", len(store.ledgerEntries))
	}

	if len(store.transfers) != 1 {
		t.Fatalf("expected failed transfer to be recorded, got %d transfers", len(store.transfers))
	}

	for _, transfer := range store.transfers {
		if transfer.Status != domain.TransferStatusFailed {
			t.Fatalf("expected stored transfer status FAILED, got %s", transfer.Status)
		}
	}
}

func TestCreateTransfer_ConcurrentTransfersDoNotOverspend_InMemoryStore(t *testing.T) {
	store := newFakeStore(map[string]int64{
		"wallet_1": 100,
		"wallet_2": 0,
		"wallet_3": 0,
	})

	svc := service.NewTransferService(store)

	commands := []service.CreateTransferCommand{
		{
			IdempotencyKey: "concurrent_1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         80,
		},
		{
			IdempotencyKey: "concurrent_2",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_3",
			Amount:         80,
		},
	}

	var wg sync.WaitGroup
	results := make(chan *service.CreateTransferResult, len(commands))
	errs := make(chan error, len(commands))

	for _, cmd := range commands {
		wg.Add(1)

		go func(cmd service.CreateTransferCommand) {
			defer wg.Done()

			result, err := svc.CreateTransfer(context.Background(), cmd)
			if err != nil {
				errs <- err
				return
			}

			results <- result
		}(cmd)
	}

	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error from concurrent transfer: %v", err)
		}
	}

	var processed int
	var failed int

	for result := range results {
		switch result.Response.Status {
		case string(domain.TransferStatusProcessed):
			processed++

		case string(domain.TransferStatusFailed):
			failed++

		default:
			t.Fatalf("unexpected transfer status %s", result.Response.Status)
		}
	}

	if processed != 1 {
		t.Fatalf("expected exactly 1 processed transfer, got %d", processed)
	}

	if failed != 1 {
		t.Fatalf("expected exactly 1 failed transfer, got %d", failed)
	}

	if store.wallets["wallet_1"].Balance != 20 {
		t.Fatalf("expected wallet_1 balance 20, got %d", store.wallets["wallet_1"].Balance)
	}

	if len(store.ledgerEntries) != 2 {
		t.Fatalf("expected ledger entries only for successful transfer, got %d", len(store.ledgerEntries))
	}
}

func assertLedgerBalanced(
	t *testing.T,
	entries []*domain.LedgerEntry,
	transferID string,
	amount int64,
) {
	t.Helper()

	var debitCount int
	var creditCount int
	var debitTotal int64
	var creditTotal int64

	for _, entry := range entries {
		if entry.TransferID != transferID {
			continue
		}

		switch entry.Type {
		case domain.LedgerEntryTypeDebit:
			debitCount++
			debitTotal += entry.Amount

		case domain.LedgerEntryTypeCredit:
			creditCount++
			creditTotal += entry.Amount

		default:
			t.Fatalf("unexpected ledger entry type %s", entry.Type)
		}
	}

	if debitCount != 1 {
		t.Fatalf("expected 1 debit entry, got %d", debitCount)
	}

	if creditCount != 1 {
		t.Fatalf("expected 1 credit entry, got %d", creditCount)
	}

	if debitTotal != amount {
		t.Fatalf("expected debit total %d, got %d", amount, debitTotal)
	}

	if creditTotal != amount {
		t.Fatalf("expected credit total %d, got %d", amount, creditTotal)
	}

	if debitTotal != creditTotal {
		t.Fatalf("ledger is not balanced, debit=%d credit=%d", debitTotal, creditTotal)
	}
}

type fakeStore struct {
	mu sync.Mutex

	wallets       map[string]*domain.Wallet
	transfers     map[string]*domain.Transfer
	ledgerEntries []*domain.LedgerEntry
	idempotency   map[string]*repository.IdempotencyRecord
}

func newFakeStore(walletBalances map[string]int64) *fakeStore {
	wallets := make(map[string]*domain.Wallet, len(walletBalances))

	for walletID, balance := range walletBalances {
		wallets[walletID] = &domain.Wallet{
			ID:      walletID,
			Balance: balance,
		}
	}

	return &fakeStore{
		wallets:       wallets,
		transfers:     make(map[string]*domain.Transfer),
		ledgerEntries: make([]*domain.LedgerEntry, 0),
		idempotency:   make(map[string]*repository.IdempotencyRecord),
	}
}

func (s *fakeStore) WithTx(
	ctx context.Context,
	fn func(ctx context.Context, repo repository.Repository) error,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	txRepo := &fakeRepository{
		wallets:       cloneWallets(s.wallets),
		transfers:     cloneTransfers(s.transfers),
		ledgerEntries: cloneLedgerEntries(s.ledgerEntries),
		idempotency:   cloneIdempotencyRecords(s.idempotency),
	}

	if err := fn(ctx, txRepo); err != nil {
		return err
	}

	s.wallets = txRepo.wallets
	s.transfers = txRepo.transfers
	s.ledgerEntries = txRepo.ledgerEntries
	s.idempotency = txRepo.idempotency

	return nil
}

type fakeRepository struct {
	wallets       map[string]*domain.Wallet
	transfers     map[string]*domain.Transfer
	ledgerEntries []*domain.LedgerEntry
	idempotency   map[string]*repository.IdempotencyRecord
}

func (r *fakeRepository) GetIdempotencyRecordForUpdate(
	ctx context.Context,
	idempotencyKey string,
) (*repository.IdempotencyRecord, error) {
	record, ok := r.idempotency[idempotencyKey]
	if !ok {
		return nil, repository.ErrNotFound
	}

	return cloneIdempotencyRecord(record), nil
}

func (r *fakeRepository) TryCreateIdempotencyRecord(
	ctx context.Context,
	idempotencyKey string,
	requestHash string,
) (bool, error) {
	if _, exists := r.idempotency[idempotencyKey]; exists {
		return false, nil
	}

	r.idempotency[idempotencyKey] = &repository.IdempotencyRecord{
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		Status:         repository.IdempotencyStatusStarted,
	}

	return true, nil
}

func (r *fakeRepository) CompleteIdempotencyRecord(
	ctx context.Context,
	idempotencyKey string,
	transferID string,
	responseCode int,
	responseBody []byte,
) error {
	record, ok := r.idempotency[idempotencyKey]
	if !ok {
		return repository.ErrNotFound
	}

	record.TransferID = stringPtr(transferID)
	record.ResponseCode = intPtr(responseCode)
	record.ResponseBody = append([]byte(nil), responseBody...)
	record.Status = repository.IdempotencyStatusCompleted

	return nil
}

func (r *fakeRepository) FailIdempotencyRecord(
	ctx context.Context,
	idempotencyKey string,
	transferID string,
	responseCode int,
	responseBody []byte,
) error {
	record, ok := r.idempotency[idempotencyKey]
	if !ok {
		return repository.ErrNotFound
	}

	record.TransferID = stringPtr(transferID)
	record.ResponseCode = intPtr(responseCode)
	record.ResponseBody = append([]byte(nil), responseBody...)
	record.Status = repository.IdempotencyStatusFailed

	return nil
}

func (r *fakeRepository) CreateTransfer(
	ctx context.Context,
	transfer *domain.Transfer,
) error {
	if _, exists := r.transfers[transfer.ID]; exists {
		return errors.New("transfer already exists")
	}

	r.transfers[transfer.ID] = cloneTransfer(transfer)

	return nil
}

func (r *fakeRepository) UpdateTransferStatus(
	ctx context.Context,
	transferID string,
	status domain.TransferStatus,
	failureReason *string,
) error {
	transfer, ok := r.transfers[transferID]
	if !ok {
		return repository.ErrNotFound
	}

	if transfer.Status != domain.TransferStatusPending {
		return domain.ErrInvalidTransferState
	}

	transfer.Status = status
	transfer.FailureReason = cloneStringPtr(failureReason)

	return nil
}

func (r *fakeRepository) LockWalletsForUpdate(
	ctx context.Context,
	walletIDs []string,
) (map[string]*domain.Wallet, error) {
	wallets := make(map[string]*domain.Wallet, len(walletIDs))

	for _, walletID := range walletIDs {
		wallet, ok := r.wallets[walletID]
		if !ok {
			return nil, repository.ErrNotFound
		}

		wallets[walletID] = cloneWallet(wallet)
	}

	return wallets, nil
}

func (r *fakeRepository) UpdateWalletBalance(
	ctx context.Context,
	walletID string,
	balance int64,
) error {
	wallet, ok := r.wallets[walletID]
	if !ok {
		return repository.ErrNotFound
	}

	wallet.Balance = balance

	return nil
}

func (r *fakeRepository) InsertLedgerEntries(
	ctx context.Context,
	entries ...*domain.LedgerEntry,
) error {
	for _, entry := range entries {
		for _, existing := range r.ledgerEntries {
			if existing.TransferID == entry.TransferID && existing.Type == entry.Type {
				return errors.New("duplicate ledger entry type for transfer")
			}
		}

		r.ledgerEntries = append(r.ledgerEntries, cloneLedgerEntry(entry))
	}

	return nil
}

func cloneWallets(input map[string]*domain.Wallet) map[string]*domain.Wallet {
	output := make(map[string]*domain.Wallet, len(input))

	for key, wallet := range input {
		output[key] = cloneWallet(wallet)
	}

	return output
}

func cloneWallet(wallet *domain.Wallet) *domain.Wallet {
	if wallet == nil {
		return nil
	}

	copied := *wallet
	return &copied
}

func cloneTransfers(input map[string]*domain.Transfer) map[string]*domain.Transfer {
	output := make(map[string]*domain.Transfer, len(input))

	for key, transfer := range input {
		output[key] = cloneTransfer(transfer)
	}

	return output
}

func cloneTransfer(transfer *domain.Transfer) *domain.Transfer {
	if transfer == nil {
		return nil
	}

	copied := *transfer
	copied.FailureReason = cloneStringPtr(transfer.FailureReason)

	return &copied
}

func cloneLedgerEntries(input []*domain.LedgerEntry) []*domain.LedgerEntry {
	output := make([]*domain.LedgerEntry, 0, len(input))

	for _, entry := range input {
		output = append(output, cloneLedgerEntry(entry))
	}

	return output
}

func cloneLedgerEntry(entry *domain.LedgerEntry) *domain.LedgerEntry {
	if entry == nil {
		return nil
	}

	copied := *entry
	return &copied
}

func cloneIdempotencyRecords(
	input map[string]*repository.IdempotencyRecord,
) map[string]*repository.IdempotencyRecord {
	output := make(map[string]*repository.IdempotencyRecord, len(input))

	for key, record := range input {
		output[key] = cloneIdempotencyRecord(record)
	}

	return output
}

func cloneIdempotencyRecord(
	record *repository.IdempotencyRecord,
) *repository.IdempotencyRecord {
	if record == nil {
		return nil
	}

	copied := *record
	copied.TransferID = cloneStringPtr(record.TransferID)
	copied.ResponseCode = cloneIntPtr(record.ResponseCode)
	copied.ResponseBody = append([]byte(nil), record.ResponseBody...)

	return &copied
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}

	copied := *value
	return &copied
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}

	copied := *value
	return &copied
}
