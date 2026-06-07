package service_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/memory"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// newTestEnv builds a service backed by the in-memory store plus deterministic
// id generators so assertions can reference predictable transfer and wallet ids.
func newTestEnv(t *testing.T) (*service.TransferService, *service.WalletService, repository.Store) {
	t.Helper()
	store := memory.NewStore()
	var counter int64
	idGen := func() string {
		return fmt.Sprintf("transfer-%d", atomic.AddInt64(&counter, 1))
	}
	transferSvc := service.NewTransferService(store, idGen, discardLogger())

	// Wallet ids are now server-generated. For tests we seed the generator so
	// existing cases can keep referring to wallets by friendly names ("w1",
	// "source", ...). mustCreateWallet pushes the desired id before each create.
	gen := &seededIDGen{}
	walletSvc := service.NewWalletService(store, gen.next, discardLogger())
	walletGenerators.Store(walletSvc, gen)
	return transferSvc, walletSvc, store
}

// walletGenerators maps a test wallet service to its seeded id generator so
// mustCreateWallet can choose the id a create will produce.
var walletGenerators sync.Map // *service.WalletService -> *seededIDGen

// seededIDGen returns queued ids in order, falling back to a counter. It is safe
// for the serial use that test setup makes of it.
type seededIDGen struct {
	mu      sync.Mutex
	queue   []string
	counter int64
}

func (g *seededIDGen) push(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = append(g.queue, id)
}

func (g *seededIDGen) next() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.queue) > 0 {
		id := g.queue[0]
		g.queue = g.queue[1:]
		return id
	}
	g.counter++
	return fmt.Sprintf("wallet-auto-%d", g.counter)
}

// mustCreateWallet creates a wallet with a chosen id (for readable assertions)
// and an opening balance, failing the test on error.
func mustCreateWallet(t *testing.T, svc *service.WalletService, id string, balance int64) {
	t.Helper()
	if g, ok := walletGenerators.Load(svc); ok {
		g.(*seededIDGen).push(id)
	}
	w, err := svc.CreateWallet(context.Background(), balance, "USD")
	if err != nil {
		t.Fatalf("create wallet %s: %v", id, err)
	}
	if w.ID != id {
		t.Fatalf("expected wallet id %s, got %s", id, w.ID)
	}
}

func TestCreateTransfer_HappyPath(t *testing.T) {
	transferSvc, walletSvc, store := newTestEnv(t)
	mustCreateWallet(t, walletSvc, "w1", 1000)
	mustCreateWallet(t, walletSvc, "w2", 0)

	res, err := transferSvc.CreateTransfer(context.Background(), domain.TransferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w1", ToWalletID: "w2", Amount: 300,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Replayed {
		t.Fatalf("first call must not be a replay")
	}
	if res.Transfer.Status != domain.StatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", res.Transfer.Status)
	}

	// Balances moved correctly.
	assertBalance(t, walletSvc, "w1", 700)
	assertBalance(t, walletSvc, "w2", 300)

	// Ledger has exactly one balanced debit/credit pair.
	assertLedgerBalanced(t, store, res.Transfer.ID, "w1", "w2", 300)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	transferSvc, walletSvc, store := newTestEnv(t)
	mustCreateWallet(t, walletSvc, "w1", 100)
	mustCreateWallet(t, walletSvc, "w2", 0)

	res, err := transferSvc.CreateTransfer(context.Background(), domain.TransferRequest{
		IdempotencyKey: "key-1", FromWalletID: "w1", ToWalletID: "w2", Amount: 500,
	})
	if err != nil {
		t.Fatalf("insufficient funds is a business outcome, not an error: %v", err)
	}
	if res.Transfer.Status != domain.StatusFailed {
		t.Fatalf("expected FAILED, got %s", res.Transfer.Status)
	}
	if res.Transfer.FailureReason != domain.ReasonInsufficientFunds {
		t.Fatalf("expected reason %s, got %s", domain.ReasonInsufficientFunds, res.Transfer.FailureReason)
	}

	// No money moved.
	assertBalance(t, walletSvc, "w1", 100)
	assertBalance(t, walletSvc, "w2", 0)

	// A failed transfer writes no ledger entries, so the ledger stays balanced.
	entries := listLedger(t, store, "w1")
	if len(entries) != 0 {
		t.Fatalf("failed transfer must not create ledger entries, got %d", len(entries))
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	transferSvc, walletSvc, _ := newTestEnv(t)
	mustCreateWallet(t, walletSvc, "w1", 1000)
	mustCreateWallet(t, walletSvc, "w2", 0)

	req := domain.TransferRequest{IdempotencyKey: "key-dup", FromWalletID: "w1", ToWalletID: "w2", Amount: 250}

	first, err := transferSvc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := transferSvc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if !second.Replayed {
		t.Fatalf("second identical request must be a replay")
	}
	if first.Transfer.ID != second.Transfer.ID {
		t.Fatalf("replay must return the same transfer id: %s != %s", first.Transfer.ID, second.Transfer.ID)
	}

	// The duplicate must NOT move money a second time.
	assertBalance(t, walletSvc, "w1", 750)
	assertBalance(t, walletSvc, "w2", 250)
}

func TestCreateTransfer_KeyReusedWithDifferentParams(t *testing.T) {
	transferSvc, walletSvc, _ := newTestEnv(t)
	mustCreateWallet(t, walletSvc, "w1", 1000)
	mustCreateWallet(t, walletSvc, "w2", 0)

	_, err := transferSvc.CreateTransfer(context.Background(), domain.TransferRequest{
		IdempotencyKey: "key-x", FromWalletID: "w1", ToWalletID: "w2", Amount: 100,
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Same key, different amount -> conflict.
	_, err = transferSvc.CreateTransfer(context.Background(), domain.TransferRequest{
		IdempotencyKey: "key-x", FromWalletID: "w1", ToWalletID: "w2", Amount: 999,
	})
	if err == nil {
		t.Fatalf("expected idempotency conflict error")
	}
}

// TestCreateTransfer_ConcurrentSameKey verifies exactly-once semantics: many
// concurrent requests sharing one idempotency key must result in exactly one
// money movement.
func TestCreateTransfer_ConcurrentSameKey(t *testing.T) {
	transferSvc, walletSvc, _ := newTestEnv(t)
	mustCreateWallet(t, walletSvc, "w1", 1000)
	mustCreateWallet(t, walletSvc, "w2", 0)

	const goroutines = 20
	req := domain.TransferRequest{IdempotencyKey: "key-concurrent", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}

	var wg sync.WaitGroup
	ids := make(chan string, goroutines)
	errs := make(chan error, goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release all goroutines at once to maximise contention
			res, err := transferSvc.CreateTransfer(context.Background(), req)
			if err != nil {
				errs <- err
				return
			}
			ids <- res.Transfer.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatalf("no concurrent request should error: %v", err)
	}

	// All requests must observe the same single transfer id.
	seen := map[string]struct{}{}
	for id := range ids {
		seen[id] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("expected exactly one transfer id across concurrent duplicates, got %d", len(seen))
	}

	// Exactly one debit of 100 must have been applied.
	assertBalance(t, walletSvc, "w1", 900)
	assertBalance(t, walletSvc, "w2", 100)
}

// TestCreateTransfer_ConcurrentDebitsNoOverdraw verifies that concurrent
// distinct transfers debiting the same wallet never overdraw it (no double
// spend) and that the ledger reflects only the successful transfers.
func TestCreateTransfer_ConcurrentDebitsNoOverdraw(t *testing.T) {
	transferSvc, walletSvc, store := newTestEnv(t)
	// Source can fund exactly 5 transfers of 100.
	mustCreateWallet(t, walletSvc, "source", 500)
	mustCreateWallet(t, walletSvc, "sink", 0)

	const attempts = 10 // twice as many as can succeed
	var wg sync.WaitGroup
	var processed, failed int64

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := transferSvc.CreateTransfer(context.Background(), domain.TransferRequest{
				IdempotencyKey: fmt.Sprintf("key-%d", i),
				FromWalletID:   "source",
				ToWalletID:     "sink",
				Amount:         100,
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			switch res.Transfer.Status {
			case domain.StatusProcessed:
				atomic.AddInt64(&processed, 1)
			case domain.StatusFailed:
				atomic.AddInt64(&failed, 1)
			}
		}(i)
	}
	wg.Wait()

	if processed != 5 {
		t.Fatalf("expected exactly 5 processed transfers, got %d", processed)
	}
	if failed != 5 {
		t.Fatalf("expected exactly 5 failed transfers, got %d", failed)
	}

	// Source fully drained, never negative; sink received exactly 500.
	assertBalance(t, walletSvc, "source", 0)
	assertBalance(t, walletSvc, "sink", 500)

	// Ledger: 5 successful transfers -> 5 debit entries on source.
	debits := listLedger(t, store, "source")
	if len(debits) != 5 {
		t.Fatalf("expected 5 debit entries on source, got %d", len(debits))
	}
	var total int64
	for _, e := range debits {
		if e.Type != domain.EntryDebit {
			t.Fatalf("expected DEBIT entries on source, got %s", e.Type)
		}
		total += e.Amount
	}
	if total != 500 {
		t.Fatalf("expected total debited 500, got %d", total)
	}
}

// --- helpers ---------------------------------------------------------------

func assertBalance(t *testing.T, svc *service.WalletService, id string, want int64) {
	t.Helper()
	w, err := svc.GetWallet(context.Background(), id)
	if err != nil {
		t.Fatalf("get wallet %s: %v", id, err)
	}
	if w.Balance != want {
		t.Fatalf("wallet %s balance = %d, want %d", id, w.Balance, want)
	}
}

func listLedger(t *testing.T, store repository.Store, walletID string) []domain.LedgerEntry {
	t.Helper()
	entries, err := store.Reader().Ledger.ListByWallet(context.Background(), walletID)
	if err != nil {
		t.Fatalf("list ledger for %s: %v", walletID, err)
	}
	return entries
}

func assertLedgerBalanced(t *testing.T, store repository.Store, transferID, from, to string, amount int64) {
	t.Helper()
	debits := listLedger(t, store, from)
	credits := listLedger(t, store, to)
	if len(debits) != 1 || debits[0].Type != domain.EntryDebit || debits[0].Amount != amount || debits[0].TransferID != transferID {
		t.Fatalf("expected single matching DEBIT on %s, got %+v", from, debits)
	}
	if len(credits) != 1 || credits[0].Type != domain.EntryCredit || credits[0].Amount != amount || credits[0].TransferID != transferID {
		t.Fatalf("expected single matching CREDIT on %s, got %+v", to, credits)
	}
}
