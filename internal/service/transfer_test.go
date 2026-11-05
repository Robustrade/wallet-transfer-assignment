package service

import (
	"context"
	"encoding/json"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/repository"
)

type fakeTx struct {
	committed bool
	rolledBack bool
	commitErr error
}
func (*fakeTx) QueryContext(context.Context, string, ...any) (*sql.Rows, error) { return nil, errors.New("unused") }
func (*fakeTx) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }
func (t *fakeTx) Commit() error { t.committed = true; return t.commitErr }
func (t *fakeTx) Rollback() error { t.rolledBack = true; return nil }

type fakeTxManager struct { tx *fakeTx; err error }
func (m fakeTxManager) Begin(context.Context) (Transaction, error) {
	if m.err != nil { return nil, m.err }
	return m.tx, nil
}

type fakeWalletStore struct {
	wallets map[string]domain.Wallet
	locks []string
	debits, credits int
}
func (s *fakeWalletStore) GetByIDForUpdate(_ context.Context, id string) (domain.Wallet, error) {
	s.locks = append(s.locks, id)
	wallet, ok := s.wallets[id]
	if !ok { return domain.Wallet{}, repository.ErrWalletNotFound }
	return wallet, nil
}
func (s *fakeWalletStore) Debit(_ context.Context, id string, amount domain.Amount) (domain.Wallet, error) {
	w := s.wallets[id]
	if err := w.Debit(amount); err != nil { return domain.Wallet{}, err }
	s.wallets[id] = w
	s.debits++
	return w, nil
}
func (s *fakeWalletStore) Credit(_ context.Context, id string, amount domain.Amount) (domain.Wallet, error) {
	w := s.wallets[id]
	if err := w.Credit(amount); err != nil { return domain.Wallet{}, err }
	s.wallets[id] = w
	s.credits++
	return w, nil
}

type fakeTransferStore struct { transfer domain.Transfer; creates, processed, failed int }
func (s *fakeTransferStore) Create(_ context.Context, transfer domain.Transfer) (domain.Transfer, error) {
	s.transfer = transfer; s.creates++; return transfer, nil
}
func (s *fakeTransferStore) MarkProcessed(_ context.Context, _ string) (domain.Transfer, error) {
	if err := s.transfer.MarkProcessed(); err != nil { return domain.Transfer{}, err }
	s.processed++; return s.transfer, nil
}
func (s *fakeTransferStore) MarkFailed(_ context.Context, _, code string) (domain.Transfer, error) {
	if err := s.transfer.MarkFailed(code); err != nil { return domain.Transfer{}, err }
	s.failed++; return s.transfer, nil
}

type fakeLedgerStore struct { entries []domain.LedgerEntry; failAt int }
func (s *fakeLedgerStore) Create(_ context.Context, entry domain.LedgerEntry) (domain.LedgerEntry, error) {
	if s.failAt > 0 && len(s.entries)+1 == s.failAt { return domain.LedgerEntry{}, errors.New("injected ledger failure") }
	s.entries = append(s.entries, entry); return entry, nil
}

type fakeStores struct {
	wallets *fakeWalletStore
	transfers *fakeTransferStore
	ledger *fakeLedgerStore
}
func newTestService(t *testing.T, from, to domain.Wallet, ledgerFailAt int) (*TransferService, *fakeTx, *fakeStores) {
	t.Helper()
	tx := &fakeTx{}
	stores := &fakeStores{
		wallets: &fakeWalletStore{wallets: map[string]domain.Wallet{from.ID: from, to.ID: to}},
		transfers: &fakeTransferStore{},
		ledger: &fakeLedgerStore{failAt: ledgerFailAt},
	}
	id := 0
	service := &TransferService{
		txManager: fakeTxManager{tx: tx},
		newStores: func(repository.DBTX) (walletStore, transferStore, ledgerStore) {
			return stores.wallets, stores.transfers, stores.ledger
		},
		newID: func() (string, error) { id++; return fmt.Sprintf("id-%d", id), nil },
	}
	return service, tx, stores
}

func TestExecuteSuccessfulTransfer(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:1000}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:200}
	svc, tx, stores := newTestService(t, from, to, 0)
	transfer, err := svc.Execute(context.Background(), TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300})
	if err != nil { t.Fatal(err) }
	if transfer.Status != domain.TransferProcessed { t.Fatalf("status = %s", transfer.Status) }
	if stores.wallets.wallets[from.ID].Balance != 700 || stores.wallets.wallets[to.ID].Balance != 500 {
		t.Fatalf("balances = %d and %d", stores.wallets.wallets[from.ID].Balance, stores.wallets.wallets[to.ID].Balance)
	}
	if len(stores.ledger.entries) != 2 || stores.ledger.entries[0].Type != domain.EntryDebit || stores.ledger.entries[1].Type != domain.EntryCredit {
		t.Fatalf("ledger entries = %+v", stores.ledger.entries)
	}
	if !tx.committed || stores.transfers.processed != 1 { t.Fatal("expected transfer to commit as processed") }
	if !reflect.DeepEqual(stores.wallets.locks, []string{"wallet-a", "wallet-b"}) { t.Fatalf("lock order = %v", stores.wallets.locks) }
}

func TestExecuteInsufficientFundsPersistsFailure(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:10}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:200}
	svc, tx, stores := newTestService(t, from, to, 0)
	transfer, err := svc.Execute(context.Background(), TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300})
	if !errors.Is(err, domain.ErrInsufficientFunds) { t.Fatalf("error = %v", err) }
	if transfer.Status != domain.TransferFailed || transfer.FailureCode != failureInsufficientFunds { t.Fatalf("transfer = %+v", transfer) }
	if stores.wallets.debits != 0 || stores.wallets.credits != 0 || len(stores.ledger.entries) != 0 { t.Fatal("failed transfer changed balances or ledger") }
	if !tx.committed { t.Fatal("expected deterministic failure to be committed") }
}

func TestExecuteRejectsCurrencyMismatch(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:100}
	to := domain.Wallet{ID:"wallet-b", Currency:"USD", Balance:200}
	svc, tx, stores := newTestService(t, from, to, 0)
	transfer, err := svc.Execute(context.Background(), TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:30})
	if !errors.Is(err, domain.ErrCurrencyMismatch) { t.Fatalf("error = %v", err) }
	if transfer.Status != domain.TransferFailed || transfer.FailureCode != failureCurrencyMismatch { t.Fatalf("transfer = %+v", transfer) }
	if !tx.committed || stores.wallets.debits != 0 || len(stores.ledger.entries) != 0 { t.Fatal("currency mismatch should commit only failed transfer") }
}

func TestExecuteRollsBackOnLedgerFailure(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:100}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:20}
	svc, tx, stores := newTestService(t, from, to, 2)
	_, err := svc.Execute(context.Background(), TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:30})
	if err == nil { t.Fatal("expected injected ledger failure") }
	if tx.committed { t.Fatal("transaction must not commit after ledger failure") }
	if !tx.rolledBack { t.Fatal("transaction should roll back") }
	if stores.transfers.processed != 0 { t.Fatal("transfer must not be marked processed") }
}

func TestExecuteRejectsInvalidRequestBeforeBeginningTransaction(t *testing.T) {
	tx := &fakeTx{}
	svc := &TransferService{txManager: fakeTxManager{tx:tx}}
	_, err := svc.Execute(context.Background(), TransferRequest{FromWalletID:"w1", ToWalletID:"w1", Amount:1})
	if !errors.Is(err, domain.ErrSameWallet) { t.Fatalf("error = %v", err) }
	if tx.committed || tx.rolledBack { t.Fatal("invalid request should not begin a transaction") }
}

type fakeIdempotencyStore struct { records map[string]repository.IdempotencyRecord }

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{records: make(map[string]repository.IdempotencyRecord)}
}
func (s *fakeIdempotencyStore) Claim(_ context.Context, key, hash string) (bool, error) {
	if _, exists := s.records[key]; exists { return false, nil }
	s.records[key] = repository.IdempotencyRecord{Key:key, RequestHash:hash}
	return true, nil
}
func (s *fakeIdempotencyStore) Get(_ context.Context, key string) (repository.IdempotencyRecord, error) {
	record, ok := s.records[key]
	if !ok { return repository.IdempotencyRecord{}, repository.ErrIdempotencyRecordNotFound }
	return record, nil
}
func (s *fakeIdempotencyStore) Complete(_ context.Context, key, hash, transferID string, status int, body []byte) error {
	record, ok := s.records[key]
	if !ok || record.RequestHash != hash || record.ResponseStatus != 0 { return errors.New("record is not pending") }
	record.TransferID, record.ResponseStatus, record.ResponseBody = transferID, status, append([]byte(nil), body...)
	s.records[key] = record
	return nil
}

func TestHashRequestIsDeterministic(t *testing.T) {
	request := TransferRequest{FromWalletID:"wallet-a", ToWalletID:"wallet-b", Amount:42}
	first, err := hashRequest(request)
	if err != nil { t.Fatal(err) }
	second, err := hashRequest(request)
	if err != nil { t.Fatal(err) }
	if first != second { t.Fatalf("hash changed: %s != %s", first, second) }
	changed, err := hashRequest(TransferRequest{FromWalletID:"wallet-a", ToWalletID:"wallet-b", Amount:43})
	if err != nil { t.Fatal(err) }
	if first == changed { t.Fatal("different amount produced the same request hash") }
}

func testResponseEncoder(transfer domain.Transfer, transferErr error) (int, []byte, error) {
	status := 201
	if transferErr != nil {
		status = 422
	}
	body, err := json.Marshal(map[string]any{
		"transfer": map[string]any{
			"id": transfer.ID, "status": transfer.Status,
			"failureCode": transfer.FailureCode,
		},
		"error": errorString(transferErr),
	})
	return status, body, err
}

func errorString(err error) string {
	if err == nil { return "" }
	return err.Error()
}

func TestExecuteIdempotentReplaysOriginalTransfer(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:1000}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:200}
	svc, _, stores := newTestService(t, from, to, 0)
	keys := newFakeIdempotencyStore()
	svc.newIdempotencyStore = func(repository.DBTX) idempotencyStore { return keys }
	request := TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300}

	first, err := svc.ExecuteIdempotent(context.Background(), "request-1", request, testResponseEncoder)
	if err != nil { t.Fatal(err) }
	second, err := svc.ExecuteIdempotent(context.Background(), "request-1", request, func(domain.Transfer, error) (int, []byte, error) {
		t.Fatal("replay must not rebuild the stored response")
		return 0, nil, nil
	})
	if err != nil { t.Fatal(err) }
	if first.StatusCode != 201 || second.StatusCode != first.StatusCode || string(second.Body) != string(first.Body) {
		t.Fatalf("replay response = status %d body %s; original = status %d body %s", second.StatusCode, second.Body, first.StatusCode, first.Body)
	}
	if first.Transfer != second.Transfer { t.Fatalf("replay transfer = %+v, original = %+v", second.Transfer, first.Transfer) }
	if stores.transfers.creates != 1 || len(stores.ledger.entries) != 2 {
		t.Fatalf("duplicate request executed again: transfers=%d ledger=%d", stores.transfers.creates, len(stores.ledger.entries))
	}
	if stores.wallets.wallets[from.ID].Balance != 700 || stores.wallets.wallets[to.ID].Balance != 500 {
		t.Fatal("replay changed wallet balances")
	}
}

func TestExecuteIdempotentRejectsDifferentPayload(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:1000}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:200}
	svc, _, stores := newTestService(t, from, to, 0)
	keys := newFakeIdempotencyStore()
	svc.newIdempotencyStore = func(repository.DBTX) idempotencyStore { return keys }
	_, err := svc.ExecuteIdempotent(context.Background(), "request-1", TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300}, testResponseEncoder)
	if err != nil { t.Fatal(err) }
	_, err = svc.ExecuteIdempotent(context.Background(), "request-1", TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:301}, testResponseEncoder)
	if !errors.Is(err, ErrIdempotencyConflict) { t.Fatalf("error = %v", err) }
	if stores.transfers.creates != 1 { t.Fatalf("transfer created %d times", stores.transfers.creates) }
}

func TestExecuteIdempotentReplaysBusinessFailure(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:10}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:200}
	svc, _, stores := newTestService(t, from, to, 0)
	keys := newFakeIdempotencyStore()
	svc.newIdempotencyStore = func(repository.DBTX) idempotencyStore { return keys }
	request := TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300}

	first, err := svc.ExecuteIdempotent(context.Background(), "request-failed", request, testResponseEncoder)
	if !errors.Is(err, domain.ErrInsufficientFunds) { t.Fatalf("first error = %v", err) }
	second, err := svc.ExecuteIdempotent(context.Background(), "request-failed", request, func(domain.Transfer, error) (int, []byte, error) {
		t.Fatal("replay must return the persisted business-failure response")
		return 0, nil, nil
	})
	if !errors.Is(err, domain.ErrInsufficientFunds) { t.Fatalf("replay error = %v", err) }
	if first.Transfer != second.Transfer || second.Transfer.Status != domain.TransferFailed ||
		first.StatusCode != 422 || second.StatusCode != first.StatusCode || string(second.Body) != string(first.Body) {
		t.Fatalf("replayed response = %+v, original = %+v", second, first)
	}
	if stores.transfers.creates != 1 { t.Fatalf("transfer created %d times", stores.transfers.creates) }
}

func TestExecuteIdempotentValidatesKey(t *testing.T) {
	svc := &TransferService{}
	_, err := svc.ExecuteIdempotent(context.Background(), "  ", TransferRequest{FromWalletID:"a", ToWalletID:"b", Amount:1}, testResponseEncoder)
	if !errors.Is(err, ErrInvalidIdempotencyKey) { t.Fatalf("error = %v", err) }
}
