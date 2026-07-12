package integration

// Package integration holds tests that run the real service wiring against a
// live Postgres. They are gated on TEST_DATABASE_URL so `go test ./...` still
// passes with no database available; set the variable to opt in:
//
//	TEST_DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable" \
//	    go test ./test/integration/...
//
// The tests below hammer POST /transfers-equivalent service calls concurrently
// and assert the two invariants that matter for a money system: no double spend
// and a ledger that always balances (sum of DEBITs == sum of CREDITs).

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/logging"
	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	pgrepo "github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// openDB connects to the test Postgres, migrates the schema, and truncates all
// tables so each test starts from a clean, isolated state. It skips (not fails)
// when TEST_DATABASE_URL is unset.
func openDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	if err := db.AutoMigrate(
		&model.Wallet{},
		&model.Transfer{},
		&model.LedgerEntry{},
		&model.IdempotencyRecord{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	if err := db.Exec(
		"TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE",
	).Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return db
}

// newService wires the real repositories and transfer service against db.
func newService(db *gorm.DB) service.TransferService {
	return service.NewTransferService(
		db,
		pgrepo.NewWalletRepository(db),
		pgrepo.NewTransferRepository(db),
		pgrepo.NewLedgerRepository(db),
		pgrepo.NewIdempotencyRepository(db),
		logging.NewLogger(logging.LevelError, nil),
	)
}

func seedWallet(t *testing.T, db *gorm.DB, id string, balance int64) {
	t.Helper()
	if err := db.Create(&model.Wallet{WalletID: id, Balance: balance}).Error; err != nil {
		t.Fatalf("seed wallet %s: %v", id, err)
	}
}

func walletBalance(t *testing.T, db *gorm.DB, id string) int64 {
	t.Helper()
	var wallet model.Wallet
	if err := db.Where("wallet_id = ?", id).First(&wallet).Error; err != nil {
		t.Fatalf("read wallet %s: %v", id, err)
	}
	return wallet.Balance
}

func ledgerSum(t *testing.T, db *gorm.DB, entryType model.EntryType) int64 {
	t.Helper()
	var total int64
	if err := db.Model(&model.LedgerEntry{}).
		Where("type = ?", entryType).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&total).Error; err != nil {
		t.Fatalf("sum %s ledger entries: %v", entryType, err)
	}
	return total
}

func countRows(t *testing.T, db *gorm.DB, dest any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := db.Model(dest).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

// TestConcurrentDistinctTransfersNoDoubleSpend fires many concurrent transfers
// (each with a unique idempotency key) that together demand far more than the
// source wallet holds. Only the funded fraction may succeed; the rest must fail
// safely. The source balance must never go negative and the ledger must balance.
func TestConcurrentDistinctTransfersNoDoubleSpend(t *testing.T) {
	db := openDB(t)
	svc := newService(db)

	const (
		amount         = 100
		concurrency    = 50
		initialBalance = 1000 // funds exactly 10 of the 50 transfers
	)
	expectedProcessed := int64(initialBalance / amount)

	seedWallet(t, db, "src", initialBalance)
	seedWallet(t, db, "dst", 0)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		processed int
		failed    int
	)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
				IdempotencyKey: "distinct-" + model.NewID(),
				FromWalletID:   "src",
				ToWalletID:     "dst",
				Amount:         amount,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && resp.State == model.TransferStateProcessed:
				processed++
			case err == service.ErrInsufficientFunds:
				failed++
			default:
				t.Errorf("unexpected transfer outcome: state=%s err=%v", resp.State, err)
			}
		}()
	}
	wg.Wait()

	if int64(processed) != expectedProcessed {
		t.Fatalf("expected %d processed transfers, got %d (failed=%d)", expectedProcessed, processed, failed)
	}

	// No double spend: the source drained by exactly the processed amount and
	// never went negative; the destination received exactly that amount.
	srcBalance := walletBalance(t, db, "src")
	dstBalance := walletBalance(t, db, "dst")
	if srcBalance < 0 {
		t.Fatalf("source balance went negative: %d", srcBalance)
	}
	if wantSrc := initialBalance - expectedProcessed*amount; srcBalance != wantSrc {
		t.Fatalf("source balance: got %d, want %d", srcBalance, wantSrc)
	}
	if wantDst := expectedProcessed * amount; dstBalance != wantDst {
		t.Fatalf("destination balance: got %d, want %d", dstBalance, wantDst)
	}

	// The ledger always balances: total DEBIT == total CREDIT, and there are
	// exactly two entries per processed transfer.
	debits := ledgerSum(t, db, model.EntryTypeDebit)
	credits := ledgerSum(t, db, model.EntryTypeCredit)
	if debits != credits {
		t.Fatalf("ledger not balanced: debits=%d credits=%d", debits, credits)
	}
	if entries := countRows(t, db, &model.LedgerEntry{}, "1 = 1"); entries != expectedProcessed*2 {
		t.Fatalf("expected %d ledger entries, got %d", expectedProcessed*2, entries)
	}

	// Every non-processed transfer is durably recorded as FAILED (replayable).
	if failedRows := countRows(t, db, &model.Transfer{}, "state = ?", model.TransferStateFailed); failedRows != int64(failed) {
		t.Fatalf("expected %d FAILED transfer rows, got %d", failed, failedRows)
	}
}

// TestConcurrentDuplicatesSingleSideEffect fires many concurrent requests that
// all share one idempotency key. Exactly one may take effect; every caller must
// observe the same result and the money must move only once.
func TestConcurrentDuplicatesSingleSideEffect(t *testing.T) {
	db := openDB(t)
	svc := newService(db)

	const (
		amount         = 100
		concurrency    = 30
		initialBalance = 1000
		key            = "shared-idempotency-key"
	)

	seedWallet(t, db, "src", initialBalance)
	seedWallet(t, db, "dst", 0)

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		transferIDs = map[string]struct{}{}
	)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
				IdempotencyKey: key,
				FromWalletID:   "src",
				ToWalletID:     "dst",
				Amount:         amount,
			})
			if err != nil {
				t.Errorf("duplicate request returned error: %v", err)
				return
			}
			mu.Lock()
			transferIDs[resp.TransferID] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Every caller saw the same transfer id (one logical result).
	if len(transferIDs) != 1 {
		t.Fatalf("expected all callers to observe one transfer id, got %d distinct ids", len(transferIDs))
	}

	// The money moved exactly once, regardless of the number of duplicates.
	if got, want := walletBalance(t, db, "src"), int64(initialBalance-amount); got != want {
		t.Fatalf("source balance: got %d, want %d (money moved more than once)", got, want)
	}
	if got, want := walletBalance(t, db, "dst"), int64(amount); got != want {
		t.Fatalf("destination balance: got %d, want %d", got, want)
	}

	// Exactly one transfer, one idempotency record, and two ledger entries exist.
	if n := countRows(t, db, &model.Transfer{}, "1 = 1"); n != 1 {
		t.Fatalf("expected 1 transfer row, got %d", n)
	}
	if n := countRows(t, db, &model.IdempotencyRecord{}, "1 = 1"); n != 1 {
		t.Fatalf("expected 1 idempotency record, got %d", n)
	}
	if n := countRows(t, db, &model.LedgerEntry{}, "1 = 1"); n != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", n)
	}

	debits := ledgerSum(t, db, model.EntryTypeDebit)
	credits := ledgerSum(t, db, model.EntryTypeCredit)
	if debits != credits || debits != amount {
		t.Fatalf("ledger not balanced: debits=%d credits=%d", debits, credits)
	}
}
