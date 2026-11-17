package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/config"
	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/Robustrade/wallet-transfer-assignment/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testEnv(t *testing.T) (*service.Service, *pgxpool.Pool) {
	t.Helper()
	config.LoadDotEnv()
	// Prefer TEST_DATABASE_URL; never fall back to DATABASE_URL so TRUNCATE
	// cannot wipe the hand-used app database.
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5433/wallet_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		failOrSkipDB(t, "cannot connect: "+err.Error())
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		failOrSkipDB(t, "ping failed: "+err.Error())
	}
	t.Cleanup(pool.Close)

	_, file, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(file), "..", "migrations")
	if err := repository.Migrate(ctx, pool, migrationsDir); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.Exec(ctx, `TRUNCATE ledger_entries, transfers, idempotency_records, wallets RESTART IDENTITY CASCADE`)
	return service.New(pool), pool
}

// failOrSkipDB fails the test on DB setup errors unless SKIP_DB_TESTS=1.
// Skipping on every connect failure would hide broken Docker/local Postgres.
func failOrSkipDB(t *testing.T, msg string) {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") == "1" {
		t.Skip("SKIP_DB_TESTS=1: " + msg)
	}
	t.Fatalf("postgres test setup failed: %s (start Postgres with docker compose up -d, or set SKIP_DB_TESTS=1 to skip)", msg)
}

func TestCreateWallet_DuplicateID(t *testing.T) {
	svc, _ := testEnv(t)
	ctx := context.Background()

	if _, err := svc.CreateWallet(ctx, "dup_w", 100); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateWallet(ctx, "dup_w", 50)
	if !errors.Is(err, service.ErrWalletAlreadyExists) {
		t.Fatalf("got %v, want ErrWalletAlreadyExists", err)
	}
}

func TestCreateTransfer_ProcessedLedgerReplayAndOpeningImmutable(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()

	created, err := svc.CreateWallet(ctx, "w_from", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "w_to", 0); err != nil {
		t.Fatal(err)
	}

	out, err := svc.CreateTransfer(ctx, "k1", "w_from", "w_to", 40)
	if err != nil {
		t.Fatal(err)
	}
	if out.StatusCode != 201 || out.Transfer == nil || out.Transfer.Status != "PROCESSED" {
		t.Fatalf("%+v", out)
	}

	from, _ := svc.GetWallet(ctx, "w_from")
	to, _ := svc.GetWallet(ctx, "w_to")
	if from.Balance != 60 || to.Balance != 40 {
		t.Fatalf("balances from=%d to=%d", from.Balance, to.Balance)
	}
	if from.OpeningBalance != created.OpeningBalance || from.OpeningBalance != 100 {
		t.Fatalf("opening_balance must stay immutable, got %d", from.OpeningBalance)
	}

	transferID, err := uuid.Parse(out.Transfer.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	ledger := repository.NewLedgerRepository(pool)
	entries, err := ledger.ListByTransfer(ctx, transferID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entries=%d, want 2", len(entries))
	}
	var debit, credit bool
	for _, e := range entries {
		if e.EntryType == model.EntryDebit && e.WalletID == "w_from" && e.Amount.Int64() == 40 {
			debit = true
		}
		if e.EntryType == model.EntryCredit && e.WalletID == "w_to" && e.Amount.Int64() == 40 {
			credit = true
		}
	}
	if !debit || !credit {
		t.Fatalf("ledger pair incomplete: %+v", entries)
	}

	replay, err := svc.CreateTransfer(ctx, "k1", "w_from", "w_to", 40)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IsReplay() || replay.StatusCode != 201 {
		t.Fatalf("replay=%+v", replay)
	}
	from2, _ := svc.GetWallet(ctx, "w_from")
	if from2.Balance != 60 {
		t.Fatal("replay must not move money")
	}
	entries2, err := ledger.ListByTransfer(ctx, transferID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != 2 {
		t.Fatalf("replay must not add ledger rows, got %d", len(entries2))
	}

	_, err = svc.CreateTransfer(ctx, "k1", "w_from", "w_to", 41)
	if !errors.Is(err, service.ErrIdempotencyMismatch) {
		t.Fatalf("mismatch after PROCESSED: %v", err)
	}
	from3, _ := svc.GetWallet(ctx, "w_from")
	to3, _ := svc.GetWallet(ctx, "w_to")
	if from3.Balance != 60 || to3.Balance != 40 {
		t.Fatalf("mismatch must not debit again: from=%d to=%d", from3.Balance, to3.Balance)
	}
	entries3, err := ledger.ListByTransfer(ctx, transferID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries3) != 2 {
		t.Fatalf("mismatch must not add ledger rows, got %d", len(entries3))
	}
}

func TestCreateTransfer_InsufficientFundsAndMismatch(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	_, _ = svc.CreateWallet(ctx, "w_from", 10)
	_, _ = svc.CreateWallet(ctx, "w_to", 0)

	out, err := svc.CreateTransfer(ctx, "k-fail", "w_from", "w_to", 50)
	if err != nil {
		t.Fatal(err)
	}
	if out.StatusCode != 422 || out.Transfer.Status != "FAILED" {
		t.Fatalf("%+v", out)
	}

	transferID, err := uuid.Parse(out.Transfer.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := repository.NewLedgerRepository(pool).ListByTransfer(ctx, transferID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed transfer must not write ledger, got %d", len(entries))
	}

	_, err = svc.CreateTransfer(ctx, "k-fail", "w_from", "w_to", 9)
	if !errors.Is(err, service.ErrIdempotencyMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
}

func TestCreateTransfer_RejectsNonPositiveAmountAndSameWallet(t *testing.T) {
	svc, _ := testEnv(t)
	ctx := context.Background()
	_, _ = svc.CreateWallet(ctx, "a", 100)
	_, _ = svc.CreateWallet(ctx, "b", 0)

	_, err := svc.CreateTransfer(ctx, "k-zero", "a", "b", 0)
	if !errors.Is(err, model.ErrInvalidAmount) {
		t.Fatalf("zero amount: %v", err)
	}
	_, err = svc.CreateTransfer(ctx, "k-neg", "a", "b", -5)
	if !errors.Is(err, model.ErrInvalidAmount) {
		t.Fatalf("negative amount: %v", err)
	}
	_, err = svc.CreateTransfer(ctx, "k-same", "a", "a", 10)
	if !errors.Is(err, model.ErrSameWallet) {
		t.Fatalf("same wallet: %v", err)
	}
}

func TestCreateTransfer_RejectsCreditOverflowWithoutMutating(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	if _, err := svc.CreateWallet(ctx, "src", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "dst", math.MaxInt64); err != nil {
		t.Fatal(err)
	}

	_, err := svc.CreateTransfer(ctx, "k-overflow", "src", "dst", 1)
	if !errors.Is(err, model.ErrBalanceOverflow) {
		t.Fatalf("got %v, want ErrBalanceOverflow", err)
	}

	src, err := svc.GetWallet(ctx, "src")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := svc.GetWallet(ctx, "dst")
	if err != nil {
		t.Fatal(err)
	}
	if src.Balance != 10 || dst.Balance != math.MaxInt64 {
		t.Fatalf("balances mutated: src=%d dst=%d", src.Balance, dst.Balance)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE idempotency_key = @idempotency_key`,
		pgx.NamedArgs{"idempotency_key": "k-overflow"}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("idempotency rows=%d, want 0 after overflow", n)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transfers`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("transfers=%d, want 0 after overflow", n)
	}
}

func TestCreateTransfer_UnknownWalletDoesNotConsumeKey(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	if _, err := svc.CreateWallet(ctx, "exists", 100); err != nil {
		t.Fatal(err)
	}

	_, err := svc.CreateTransfer(ctx, "later-ok", "missing", "exists", 10)
	if !errors.Is(err, service.ErrWalletNotFound) {
		t.Fatalf("got %v, want ErrWalletNotFound", err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE idempotency_key = @idempotency_key`,
		pgx.NamedArgs{"idempotency_key": "later-ok"}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("idempotency rows=%d, want 0 after unknown wallet", n)
	}

	if _, err := svc.CreateWallet(ctx, "missing", 50); err != nil {
		t.Fatal(err)
	}
	out, err := svc.CreateTransfer(ctx, "later-ok", "missing", "exists", 10)
	if err != nil || out.StatusCode != 201 {
		t.Fatalf("retry after create: out=%+v err=%v", out, err)
	}
}

func TestCreateTransfer_FailedReplaySameBody(t *testing.T) {
	svc, _ := testEnv(t)
	ctx := context.Background()
	_, _ = svc.CreateWallet(ctx, "w_from", 10)
	_, _ = svc.CreateWallet(ctx, "w_to", 0)

	first, err := svc.CreateTransfer(ctx, "k-fail-replay", "w_from", "w_to", 50)
	if err != nil || first.StatusCode != 422 || first.Transfer == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}

	replay, err := svc.CreateTransfer(ctx, "k-fail-replay", "w_from", "w_to", 50)
	if err != nil {
		t.Fatal(err)
	}
	assertFailedReplayMatches(t, first, replay)

	replay2, err := svc.CreateTransfer(ctx, "k-fail-replay", "w_from", "w_to", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(replay.ReplayBody, replay2.ReplayBody) {
		t.Fatalf("stored replay bytes must be stable: %s vs %s", replay.ReplayBody, replay2.ReplayBody)
	}

	from, _ := svc.GetWallet(ctx, "w_from")
	if from.Balance != 10 {
		t.Fatalf("balance=%d, want unchanged 10", from.Balance)
	}
}

// Insufficient funds is sticky: funding the source later must not turn the same key into a success.
func TestCreateTransfer_FailedStickyAfterFundsArrive(t *testing.T) {
	svc, pool := testEnv(t)
	ctx := context.Background()
	if _, err := svc.CreateWallet(ctx, "w_from", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "w_to", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWallet(ctx, "w_funder", 100); err != nil {
		t.Fatal(err)
	}

	first, err := svc.CreateTransfer(ctx, "k-sticky", "w_from", "w_to", 50)
	if err != nil || first.StatusCode != 422 || first.Transfer == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	failedID := first.Transfer.TransferID

	fund, err := svc.CreateTransfer(ctx, "k-fund", "w_funder", "w_from", 50)
	if err != nil || fund.StatusCode != 201 {
		t.Fatalf("fund=%+v err=%v", fund, err)
	}
	funded, err := svc.GetWallet(ctx, "w_from")
	if err != nil {
		t.Fatal(err)
	}
	if funded.Balance != 60 {
		t.Fatalf("after fund balance=%d, want 60", funded.Balance)
	}

	replay, err := svc.CreateTransfer(ctx, "k-sticky", "w_from", "w_to", 50)
	if err != nil {
		t.Fatal(err)
	}
	assertFailedReplayMatches(t, first, replay)

	afterReplay, err := svc.GetWallet(ctx, "w_from")
	if err != nil {
		t.Fatal(err)
	}
	to, err := svc.GetWallet(ctx, "w_to")
	if err != nil {
		t.Fatal(err)
	}
	if afterReplay.Balance != 60 || to.Balance != 0 {
		t.Fatalf("sticky retry must not move money: from=%d to=%d", afterReplay.Balance, to.Balance)
	}

	failedUUID, err := uuid.Parse(failedID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := repository.NewLedgerRepository(pool).ListByTransfer(ctx, failedUUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed transfer must still have no ledger, got %d", len(entries))
	}

	ok, err := svc.CreateTransfer(ctx, "k-sticky-new", "w_from", "w_to", 50)
	if err != nil || ok.StatusCode != 201 || ok.Transfer == nil {
		t.Fatalf("new key after fund=%+v err=%v", ok, err)
	}
	if ok.Transfer.TransferID == failedID {
		t.Fatal("new key must create a different transfer")
	}
	fromFinal, _ := svc.GetWallet(ctx, "w_from")
	toFinal, _ := svc.GetWallet(ctx, "w_to")
	if fromFinal.Balance != 10 || toFinal.Balance != 50 {
		t.Fatalf("new key balances from=%d to=%d", fromFinal.Balance, toFinal.Balance)
	}
}

func assertFailedReplayMatches(t *testing.T, first, replay dto.TransferOutcome) {
	t.Helper()
	if !replay.IsReplay() || replay.StatusCode != 422 {
		t.Fatalf("replay=%+v", replay)
	}
	var replayed dto.TransferView
	if err := json.Unmarshal(replay.ReplayBody, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.TransferID != first.Transfer.TransferID {
		t.Fatalf("transferId=%s, want %s", replayed.TransferID, first.Transfer.TransferID)
	}
	if replayed.Status != first.Transfer.Status || replayed.Amount != first.Transfer.Amount {
		t.Fatalf("replay view=%+v, want %+v", replayed, first.Transfer)
	}
	if replayed.FailureReason == nil || first.Transfer.FailureReason == nil ||
		*replayed.FailureReason != *first.Transfer.FailureReason {
		t.Fatalf("failureReason replay=%v first=%v", replayed.FailureReason, first.Transfer.FailureReason)
	}
	if replayed.IdempotencyKey != first.Transfer.IdempotencyKey ||
		replayed.FromWalletID != first.Transfer.FromWalletID ||
		replayed.ToWalletID != first.Transfer.ToWalletID {
		t.Fatalf("replay identity fields=%+v, want %+v", replayed, first.Transfer)
	}
}

func TestGetWalletAndTransfer_NotFound(t *testing.T) {
	svc, _ := testEnv(t)
	ctx := context.Background()

	_, err := svc.GetWallet(ctx, "nope")
	if !errors.Is(err, service.ErrWalletNotFound) {
		t.Fatalf("GetWallet: %v", err)
	}
	_, err = svc.GetTransfer(ctx, uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	if !errors.Is(err, service.ErrTransferNotFound) {
		t.Fatalf("GetTransfer: %v", err)
	}
}
