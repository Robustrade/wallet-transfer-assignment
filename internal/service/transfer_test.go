package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/repository"
)

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

func TestExecuteIdempotentReplaysOriginalTransfer(t *testing.T) {
	from := domain.Wallet{ID:"wallet-a", Currency:"INR", Balance:1000}
	to := domain.Wallet{ID:"wallet-b", Currency:"INR", Balance:200}
	svc, _, stores := newTestService(t, from, to, 0)
	keys := newFakeIdempotencyStore()
	svc.newIdempotencyStore = func(repository.DBTX) idempotencyStore { return keys }
	request := TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300}

	first, err := svc.ExecuteIdempotent(context.Background(), "request-1", request)
	if err != nil { t.Fatal(err) }
	second, err := svc.ExecuteIdempotent(context.Background(), "request-1", request)
	if err != nil { t.Fatal(err) }
	if first != second { t.Fatalf("replay = %+v, original = %+v", second, first) }
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
	_, err := svc.ExecuteIdempotent(context.Background(), "request-1", TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:300})
	if err != nil { t.Fatal(err) }
	_, err = svc.ExecuteIdempotent(context.Background(), "request-1", TransferRequest{FromWalletID:from.ID, ToWalletID:to.ID, Amount:301})
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

	first, err := svc.ExecuteIdempotent(context.Background(), "request-failed", request)
	if !errors.Is(err, domain.ErrInsufficientFunds) { t.Fatalf("first error = %v", err) }
	second, err := svc.ExecuteIdempotent(context.Background(), "request-failed", request)
	if !errors.Is(err, domain.ErrInsufficientFunds) { t.Fatalf("replay error = %v", err) }
	if first != second || second.Status != domain.TransferFailed { t.Fatalf("replayed transfer = %+v, original = %+v", second, first) }
	if stores.transfers.creates != 1 { t.Fatalf("transfer created %d times", stores.transfers.creates) }
}

func TestExecuteIdempotentValidatesKey(t *testing.T) {
	svc := &TransferService{}
	_, err := svc.ExecuteIdempotent(context.Background(), "  ", TransferRequest{FromWalletID:"a", ToWalletID:"b", Amount:1})
	if !errors.Is(err, ErrInvalidIdempotencyKey) { t.Fatalf("error = %v", err) }
}

func TestStoredResponseRoundTrips(t *testing.T) {
	original := storedTransferResponse{Transfer:domain.Transfer{ID:"t1", FromWalletID:"a", ToWalletID:"b", Amount:2, Status:domain.TransferProcessed}}
	body, err := json.Marshal(original)
	if err != nil { t.Fatal(err) }
	var decoded storedTransferResponse
	if err := json.Unmarshal(body, &decoded); err != nil { t.Fatal(err) }
	if decoded != original { t.Fatalf("decoded = %+v, want %+v", decoded, original) }
}
