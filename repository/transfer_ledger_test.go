package repository_test

import (
	"context"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
)

func seedWallets(t *testing.T, ctx context.Context, fromBal, toBal int64) {
	t.Helper()
	wallets := repository.NewWalletRepository(requirePool(t))
	from, _ := model.NewWallet("from_w", fromBal)
	to, _ := model.NewWallet("to_w", toBal)
	if err := wallets.Create(ctx, from); err != nil {
		t.Fatal(err)
	}
	if err := wallets.Create(ctx, to); err != nil {
		t.Fatal(err)
	}
}

func claimKey(t *testing.T, ctx context.Context, key string, from, to string, amount int64) {
	t.Helper()
	hash, body, err := model.HashTransferRequest(from, to, amount)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := model.NewIdempotencyRecord(key, hash, body)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewIdempotencyRepository(requirePool(t))
	claimed, err := repo.Claim(ctx, rec)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
}

func TestTransferProcessed_WithLedgerPair(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	ctx := context.Background()
	seedWallets(t, ctx, 100, 0)
	claimKey(t, ctx, "key-ok", "from_w", "to_w", 30)

	wallets := repository.NewWalletRepository(pool)
	transfers := repository.NewTransferRepository(pool)
	ledger := repository.NewLedgerRepository(pool)
	idem := repository.NewIdempotencyRepository(pool)

	txCtx, tx, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tr, err := model.NewTransfer("key-ok", "from_w", "to_w", 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.Insert(txCtx, tr); err != nil {
		t.Fatal(err)
	}

	locked, err := wallets.LockByIDs(txCtx, tr.FromWalletID, tr.ToWalletID)
	if err != nil || len(locked) != 2 {
		t.Fatalf("lock: %v len=%d", err, len(locked))
	}
	ok, err := wallets.Debit(txCtx, tr.FromWalletID, tr.Amount)
	if err != nil || !ok {
		t.Fatalf("debit: %v %v", ok, err)
	}
	if err := wallets.Credit(txCtx, tr.ToWalletID, tr.Amount); err != nil {
		t.Fatal(err)
	}
	if err := tr.MarkProcessed(); err != nil {
		t.Fatal(err)
	}
	debit, credit, err := model.NewLedgerPair(tr)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.InsertPair(txCtx, debit, credit); err != nil {
		t.Fatal(err)
	}
	if err := transfers.UpdateStatus(txCtx, tr); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"transferId":"` + tr.ID.String() + `","status":"PROCESSED"}`)
	if err := idem.StoreResponse(txCtx, tr.IdempotencyKey, 201, body); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := transfers.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusProcessed {
		t.Fatalf("status=%s", got.Status)
	}
	entries, err := ledger.ListByTransfer(ctx, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger len=%d", len(entries))
	}

	from, _ := wallets.GetByID(ctx, "from_w")
	to, _ := wallets.GetByID(ctx, "to_w")
	if from.Balance != 70 || to.Balance != 30 {
		t.Fatalf("balances from=%d to=%d", from.Balance, to.Balance)
	}

	sumFrom, _ := ledger.SumSigned(ctx, "from_w")
	sumTo, _ := ledger.SumSigned(ctx, "to_w")
	if from.OpeningBalance.Int64()+sumFrom != from.Balance.Int64() {
		t.Fatalf("from reconcile: opening=%d sum=%d bal=%d", from.OpeningBalance, sumFrom, from.Balance)
	}
	if to.OpeningBalance.Int64()+sumTo != to.Balance.Int64() {
		t.Fatalf("to reconcile: opening=%d sum=%d bal=%d", to.OpeningBalance, sumTo, to.Balance)
	}

	// second ledger pair for same transfer must fail unique constraint
	txCtx2, tx2, _ := repository.BeginTx(ctx, pool)
	defer func() { _ = tx2.Rollback(ctx) }()
	if err := ledger.InsertPair(txCtx2, debit, credit); err == nil {
		t.Fatal("expected unique violation on second ledger pair")
	}
}

func TestTransferFailed_NoLedgerNoBalanceChange(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	ctx := context.Background()
	seedWallets(t, ctx, 20, 0)
	claimKey(t, ctx, "key-fail", "from_w", "to_w", 50)

	wallets := repository.NewWalletRepository(pool)
	transfers := repository.NewTransferRepository(pool)
	ledger := repository.NewLedgerRepository(pool)
	idem := repository.NewIdempotencyRepository(pool)

	txCtx, tx, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tr, _ := model.NewTransfer("key-fail", "from_w", "to_w", 50)
	if err := transfers.Insert(txCtx, tr); err != nil {
		t.Fatal(err)
	}
	_, _ = wallets.LockByIDs(txCtx, tr.FromWalletID, tr.ToWalletID)
	ok, err := wallets.Debit(txCtx, tr.FromWalletID, tr.Amount)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected insufficient funds")
	}
	if err := tr.MarkFailed(model.FailureInsufficientFunds); err != nil {
		t.Fatal(err)
	}
	if err := transfers.UpdateStatus(txCtx, tr); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"status":"FAILED","failureReason":"insufficient_funds"}`)
	if err := idem.StoreResponse(txCtx, tr.IdempotencyKey, 422, body); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, _ := transfers.GetByIdempotencyKey(ctx, "key-fail")
	if got.Status != model.StatusFailed || got.FailureReason == nil || *got.FailureReason != model.FailureInsufficientFunds {
		t.Fatalf("%+v", got)
	}
	entries, _ := ledger.ListByTransfer(ctx, tr.ID)
	if len(entries) != 0 {
		t.Fatalf("expected 0 ledger rows, got %d", len(entries))
	}
	from, _ := wallets.GetByID(ctx, "from_w")
	to, _ := wallets.GetByID(ctx, "to_w")
	if from.Balance != 20 || to.Balance != 0 {
		t.Fatalf("balances changed: from=%d to=%d", from.Balance, to.Balance)
	}
}
