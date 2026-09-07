package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/memory"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

func TestStore_WithinTxRollsBackOnError(t *testing.T) {
	store := memory.New()
	store.SeedWallet("w1", 100, "USD")
	ctx := context.Background()

	wantErr := errors.New("boom")
	err := store.WithinTx(ctx, func(ctx context.Context) error {
		w, _ := store.Get(ctx, "w1")
		w.Balance = domain.Money{Amount: 0, Currency: "USD"}
		if uErr := store.UpdateBalance(ctx, w); uErr != nil {
			return uErr
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want boom", err)
	}

	w, _ := store.Get(ctx, "w1")
	if w.Balance.Amount != 100 {
		t.Fatalf("balance = %d, want 100 (rolled back)", w.Balance.Amount)
	}
}

func TestStore_DuplicateTransferKeyRejected(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	first, _ := domain.NewTransfer("t1", "key", "a", "b", domain.Money{Amount: 1, Currency: "USD"})
	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, _ := domain.NewTransfer("t2", "key", "a", "b", domain.Money{Amount: 1, Currency: "USD"})
	if err := store.Create(ctx, second); !errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		t.Fatalf("got %v, want ErrDuplicateIdempotencyKey", err)
	}
}

func TestStore_InsertPairAndSum(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	debit, credit, _ := domain.NewLedgerPair("t1", "a", "b", domain.Money{Amount: 40, Currency: "USD"})
	if err := store.InsertPair(ctx, debit, credit); err != nil {
		t.Fatalf("InsertPair: %v", err)
	}

	aSum, _ := store.SumByWallet(ctx, "a")
	bSum, _ := store.SumByWallet(ctx, "b")
	if aSum.Amount != -40 || bSum.Amount != 40 {
		t.Fatalf("sums = %d / %d, want -40 / 40", aSum.Amount, bSum.Amount)
	}
}

func TestStore_IdempotencyInsertAndGet(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	rec := service.IdempotencyRecord{Key: "k", TransferID: "t1", RequestHash: "h"}
	if err := store.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Insert(ctx, rec); !errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		t.Fatalf("duplicate Insert got %v", err)
	}
	got, err := store.GetRecord(ctx, "k")
	if err != nil || got.RequestHash != "h" {
		t.Fatalf("GetRecord = %+v, %v", got, err)
	}
}
