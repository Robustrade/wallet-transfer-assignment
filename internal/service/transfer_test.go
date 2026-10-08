package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

var injected = errors.New("injected database failure")

type memory struct {
	repository.Tx
	wallets   map[string]domain.Wallet
	transfers map[string]domain.Transfer
	records   map[string]repository.IdempotencyRecord
	ledger    []domain.LedgerEntry
	fail      string
	begun     int
}

func fixture() *memory {
	return &memory{wallets: map[string]domain.Wallet{"a": {ID: "a", Balance: 100}, "b": {ID: "b", Balance: 20}}, transfers: map[string]domain.Transfer{}, records: map[string]repository.IdempotencyRecord{}}
}
func cloneMap[K comparable, V any](src map[K]V) map[K]V {
	dst := make(map[K]V)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// This fake tests orchestration, not PostgreSQL atomicity or locking.
func (m *memory) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	m.begun++
	staged := *m
	staged.wallets = cloneMap(m.wallets)
	staged.transfers = cloneMap(m.transfers)
	staged.records = cloneMap(m.records)
	staged.ledger = append([]domain.LedgerEntry(nil), m.ledger...)
	if err := fn(ctx, &staged); err != nil {
		return err
	}
	if m.fail == "commit" {
		return injected
	}
	m.wallets, m.transfers, m.records, m.ledger = staged.wallets, staged.transfers, staged.records, staged.ledger
	return nil
}
func (m *memory) LockWallets(_ context.Context, a, b string) (domain.Wallet, domain.Wallet, error) {
	x, ok := m.wallets[a]
	y, ok2 := m.wallets[b]
	if !ok || !ok2 {
		return x, y, repository.ErrNotFound
	}
	return x, y, nil
}
func (m *memory) ReserveIdempotencyKey(_ context.Context, k, f, id string, at time.Time) (bool, error) {
	if _, ok := m.records[k]; ok {
		return false, nil
	}
	m.records[k] = repository.IdempotencyRecord{Key: k, Fingerprint: f, TransferID: id, CreatedAt: at}
	return true, nil
}
func (m *memory) GetIdempotencyRecord(_ context.Context, k string) (repository.IdempotencyRecord, error) {
	return m.records[k], nil
}
func (m *memory) CreateTransfer(_ context.Context, v domain.Transfer) error {
	m.transfers[v.ID] = v
	return nil
}
func (m *memory) UpdateWalletBalance(_ context.Context, id string, b int64, at time.Time) error {
	if m.fail == "credit" && id == "b" {
		return injected
	}
	w := m.wallets[id]
	w.Balance = b
	w.UpdatedAt = at
	m.wallets[id] = w
	return nil
}
func (m *memory) CreateLedgerEntry(_ context.Context, v domain.LedgerEntry) error {
	if (m.fail == "ledger" && v.Type == domain.LedgerEntryCredit) || (m.fail == "debit_ledger" && v.Type == domain.LedgerEntryDebit) {
		return injected
	}
	m.ledger = append(m.ledger, v)
	return nil
}
func (m *memory) MarkTransferProcessed(_ context.Context, id string, at time.Time) error {
	if len(m.ledger) != 2 {
		return errors.New("processed before ledger complete")
	}
	v := m.transfers[id]
	err := v.MarkProcessed(at)
	m.transfers[id] = v
	return err
}
func (m *memory) MarkTransferFailed(_ context.Context, id string, at time.Time) error {
	v := m.transfers[id]
	err := v.MarkFailed(at)
	m.transfers[id] = v
	return err
}
func (m *memory) CompleteIdempotencyKey(_ context.Context, k, id string, r repository.Result, at time.Time) error {
	if m.fail == "result" {
		return injected
	}
	v := m.records[k]
	v.Result = &r
	v.UpdatedAt = at
	m.records[k] = v
	return nil
}
func testService(m *memory) *Service { return New(m, slog.New(slog.NewTextHandler(io.Discard, nil))) }
func request() Request {
	return Request{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: 30}
}

func TestSuccessfulTransferAndReplay(t *testing.T) {
	m := fixture()
	s := testService(m)
	r, err := s.Transfer(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 201 || m.wallets["a"].Balance != 70 || m.wallets["b"].Balance != 50 || len(m.transfers) != 1 || len(m.ledger) != 2 {
		t.Fatalf("incorrect result: %+v %+v", r, m)
	}
	for _, v := range m.transfers {
		assertBalancedTransferLedger(t, v, m.ledger)
		if v.Status() != domain.TransferStatusProcessed {
			t.Fatal("not processed")
		}
	}
	var body struct {
		TransferID string `json:"transferId"`
		State      string `json:"state"`
		Amount     int64  `json:"amount"`
	}
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil || body.State != "PROCESSED" || body.Amount != 30 {
		t.Fatalf("unexpected success response: %s", r.Body)
	}
	if _, ok := m.transfers[body.TransferID]; !ok {
		t.Fatal("result references an unknown transfer")
	}
	beforeWallets := cloneMap(m.wallets)
	beforeLedger := append([]domain.LedgerEntry(nil), m.ledger...)
	again, err := s.Transfer(context.Background(), request())
	if err != nil || again != r || len(m.ledger) != 2 || m.wallets["a"].Balance != 70 {
		t.Fatal("replay changed result or balances")
	}
	if !reflect.DeepEqual(beforeWallets, m.wallets) || !reflect.DeepEqual(beforeLedger, m.ledger) || len(m.transfers) != 1 {
		t.Fatal("retry changed persisted financial state")
	}

}
func TestBusinessFailureAndReplay(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "insufficient", true: "overflow"}[overflow], func(t *testing.T) {
			m := fixture()
			req := request()
			if overflow {
				w := m.wallets["b"]
				w.Balance = math.MaxInt64
				m.wallets["b"] = w
			} else {
				req.Amount = 101
			}
			before := cloneMap(m.wallets)
			s := testService(m)
			r, err := s.Transfer(context.Background(), req)
			if err != nil || r.StatusCode != 422 || len(m.transfers) != 1 || len(m.ledger) != 0 || !reflect.DeepEqual(before, m.wallets) {
				t.Fatalf("failure not committed correctly: %+v %v", r, err)
			}
			var body struct {
				State      string `json:"state"`
				TransferID string `json:"transferId"`
				Error      struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(r.Body), &body); err != nil || body.State != "FAILED" {
				t.Fatalf("failure response: %s", r.Body)
			}
			wantCode := "INSUFFICIENT_BALANCE"
			if overflow {
				wantCode = "BALANCE_OVERFLOW"
			}
			if body.Error.Code != wantCode {
				t.Fatalf("got failure code %q want %q", body.Error.Code, wantCode)
			}
			if _, ok := m.transfers[body.TransferID]; !ok {
				t.Fatal("failed result references unknown transfer")
			}
			for _, v := range m.transfers {
				if v.Status() != domain.TransferStatusFailed {
					t.Fatal("not failed")
				}
			}
			w := m.wallets["a"]
			w.Balance = 1000
			m.wallets["a"] = w
			again, err := s.Transfer(context.Background(), req)
			if err != nil || again != r || len(m.transfers) != 1 || len(m.ledger) != 0 {
				t.Fatal("failed replay changed result")
			}
		})
	}
}
func TestMissingWallet(t *testing.T) {
	for _, missing := range []string{"a", "b"} {
		t.Run(map[string]string{"a": "source", "b": "destination"}[missing], func(t *testing.T) {
			m := fixture()
			delete(m.wallets, missing)
			before := cloneMap(m.wallets)
			result, err := testService(m).Transfer(context.Background(), request())
			if !errors.Is(err, ErrWalletNotFound) || result != (repository.Result{}) {
				t.Fatalf("missing wallet: %+v %v", result, err)
			}
			if len(m.transfers) != 0 || len(m.records) != 0 || len(m.ledger) != 0 || !reflect.DeepEqual(before, m.wallets) {
				t.Fatal("missing wallet changed financial state")
			}
		})
	}
}
func TestRollbackOnFailure(t *testing.T) {
	for _, point := range []string{"credit", "debit_ledger", "ledger", "result", "commit"} {
		t.Run(point, func(t *testing.T) {
			m := fixture()
			m.fail = point
			r, err := testService(m).Transfer(context.Background(), request())
			if !errors.Is(err, injected) || r != (repository.Result{}) || len(m.transfers) != 0 || len(m.records) != 0 || len(m.ledger) != 0 || m.wallets["a"].Balance != 100 || m.wallets["b"].Balance != 20 {
				t.Fatalf("partial result escaped: %+v %v", r, err)
			}
		})
	}
}
func TestInvalidTransferHasNoSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Request
	}{
		{"zero amount", Request{"key", "a", "b", 0}},
		{"negative amount", Request{"key", "a", "b", -1}},
		{"same wallet", Request{"key", "a", "a", 1}},
		{"blank source", Request{"key", "", "b", 1}},
		{"blank destination", Request{"key", "a", "", 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixture()
			before := cloneMap(m.wallets)
			result, err := testService(m).Transfer(context.Background(), tc.req)
			if !errors.Is(err, ErrInvalidRequest) || result != (repository.Result{}) {
				t.Fatalf("invalid request: %+v %v", result, err)
			}
			if !reflect.DeepEqual(before, m.wallets) || len(m.transfers) != 0 || len(m.ledger) != 0 || len(m.records) != 0 {
				t.Fatal("invalid request changed financial state")
			}
		})
	}
}

func TestConflictingRetryHasNoSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Request
	}{
		{"amount", Request{"key", "a", "b", 31}},
		{"source", Request{"key", "c", "b", 30}},
		{"destination", Request{"key", "a", "c", 30}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixture()
			m.wallets["c"] = domain.Wallet{ID: "c", Balance: 100}
			s := testService(m)
			original, err := s.Transfer(context.Background(), request())
			if err != nil {
				t.Fatal(err)
			}
			wallets := cloneMap(m.wallets)
			transfers := cloneMap(m.transfers)
			ledger := append([]domain.LedgerEntry(nil), m.ledger...)
			result, err := s.Transfer(context.Background(), tc.req)
			if !errors.Is(err, ErrIdempotencyConflict) || result != (repository.Result{}) {
				t.Fatalf("conflicting retry: %+v %v", result, err)
			}
			if !reflect.DeepEqual(wallets, m.wallets) || !reflect.DeepEqual(transfers, m.transfers) || !reflect.DeepEqual(ledger, m.ledger) || len(m.records) != 1 {
				t.Fatal("conflict changed financial state")
			}
			replay, err := s.Transfer(context.Background(), request())
			if err != nil || replay != original {
				t.Fatal("conflict replaced original result")
			}
		})
	}
}
