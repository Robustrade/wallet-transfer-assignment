//go:build integration

// Package integration runs the real service + repositories against PostgreSQL.
//
// Run with:  go test -tags integration -race ./internal/integration/
//
// It uses the database from .env (migrations must already be applied) and never
// touches existing rows: every test creates its own wallets and only asserts on
// those.
package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/handler"
	"wallet-transfer-assignment/internal/feature/wallet/model"
	"wallet-transfer-assignment/internal/feature/wallet/repository"
	"wallet-transfer-assignment/internal/feature/wallet/route"
	"wallet-transfer-assignment/internal/feature/wallet/service"

	"github.com/gofiber/fiber/v3"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	_ = godotenv.Load("../../../../.env") // env vars already set take precedence

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_SSLMODE"))

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration tests need PostgreSQL (see .env): %v\n", err)
		os.Exit(1)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(25)
	testDB = db

	os.Exit(m.Run())
}

// ---- helpers -------------------------------------------------------------

func newService() service.TransferService {
	return service.NewTransferService(
		repository.NewWalletRepository(),
		repository.NewTransferRepository(),
		repository.NewLedgerRepository(),
		repository.NewIdempotencyRepository(),
		repository.NewTransactionManager(testDB),
	)
}

var keySeq atomic.Int64

// uniqueKey returns a key that is unique across tests and across runs
// (idempotency_key is globally UNIQUE and rows are never cleaned up).
func uniqueKey(_ *testing.T, _ int) string {
	return fmt.Sprintf("it-%d-%d", time.Now().UnixNano(), keySeq.Add(1))
}

func newWallet(t *testing.T, balance int64) int64 {
	t.Helper()
	w := model.Wallet{Balance: balance}
	if err := testDB.Create(&w).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	return w.ID
}

func balance(t *testing.T, id int64) int64 {
	t.Helper()
	var w model.Wallet
	if err := testDB.First(&w, id).Error; err != nil {
		t.Fatalf("load wallet %d: %v", id, err)
	}
	return w.Balance
}

func count(t *testing.T, mdl any, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(mdl).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// ledgerNet returns SUM(credits) - SUM(debits) for a wallet: must equal its
// balance change when the wallet started at 0 and only moved via transfers.
func ledgerNet(t *testing.T, walletID int64) int64 {
	t.Helper()
	var net int64
	err := testDB.Raw(`SELECT COALESCE(SUM(CASE entry_type WHEN 'CREDIT' THEN amount ELSE -amount END),0)
	                   FROM ledger_entries WHERE wallet_id = ?`, walletID).Scan(&net).Error
	if err != nil {
		t.Fatal(err)
	}
	return net
}

// assertEveryProcessedTransferHasBalancedPair checks the double-entry invariant
// for the transfers touching the given wallets.
func assertLedgerInvariants(t *testing.T, walletIDs ...int64) {
	t.Helper()

	var bad []struct {
		TransferID int64
		Entries    int64
		Debit      int64
		Credit     int64
	}
	err := testDB.Raw(`
		SELECT t.id AS transfer_id,
		       COUNT(l.id) AS entries,
		       COALESCE(SUM(CASE WHEN l.entry_type='DEBIT'  THEN l.amount END),0) AS debit,
		       COALESCE(SUM(CASE WHEN l.entry_type='CREDIT' THEN l.amount END),0) AS credit
		FROM transfers t
		LEFT JOIN ledger_entries l ON l.transfer_id = t.id
		WHERE (t.from_wallet_id IN ? OR t.to_wallet_id IN ?)
		GROUP BY t.id, t.status, t.amount
		HAVING (t.status = 'PROCESSED' AND (COUNT(l.id) <> 2
		        OR SUM(CASE WHEN l.entry_type='DEBIT'  THEN l.amount END) <> t.amount
		        OR SUM(CASE WHEN l.entry_type='CREDIT' THEN l.amount END) <> t.amount))
		    OR (t.status <> 'PROCESSED' AND COUNT(l.id) <> 0)`, walletIDs, walletIDs).Scan(&bad).Error
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("ledger invariant violated: %+v", bad)
	}

	if n := count(t, &model.Transfer{}, "status = 'PENDING' AND (from_wallet_id IN ? OR to_wallet_id IN ?)", walletIDs, walletIDs); n != 0 {
		t.Fatalf("%d transfers left PENDING", n)
	}
}

// sameJSON requires replays to be byte-identical to the original response, even
// though response_body is JSONB (which Postgres re-formats on read).
func sameJSON(_ *testing.T, a, b []byte) bool { return string(a) == string(b) }

func fire(t *testing.T, n int, fn func(i int) (*service.TransferResult, error)) []*service.TransferResult {
	t.Helper()
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release everyone at once to maximise contention
			results[i], errs[i] = fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
	return results
}

// ---- tests ---------------------------------------------------------------

func TestTransfer_PersistsTransferLedgerAndBalances(t *testing.T) {
	from, to := newWallet(t, 1000), newWallet(t, 0)
	key := uniqueKey(t, 0)

	res, err := newService().CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 250,
	})
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	if balance(t, from) != 750 || balance(t, to) != 250 {
		t.Errorf("balances = %d/%d, want 750/250", balance(t, from), balance(t, to))
	}

	var tr model.Transfer
	if err := testDB.Where("idempotency_key = ?", key).First(&tr).Error; err != nil {
		t.Fatalf("transfer not saved: %v", err)
	}
	if tr.Status != string(domain.TransferProcessed) {
		t.Errorf("status = %s", tr.Status)
	}

	entries, err := repository.NewLedgerRepository().GetByTransferID(context.Background(), testDB, tr.ID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if entries[0].WalletID != from || entries[0].EntryType != domain.LedgerDebit ||
		entries[1].WalletID != to || entries[1].EntryType != domain.LedgerCredit {
		t.Errorf("ledger = %+v", entries)
	}
	assertLedgerInvariants(t, from, to)
}

func TestTransfer_RejectsWhenBalanceTooLowAndRecordsFailure(t *testing.T) {
	from, to := newWallet(t, 50), newWallet(t, 0)
	key := uniqueKey(t, 0)

	res, err := newService().CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 100,
	})
	if err != nil || res.StatusCode != 409 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if balance(t, from) != 50 || balance(t, to) != 0 {
		t.Errorf("balances moved")
	}
	var tr model.Transfer
	if err := testDB.Where("idempotency_key = ?", key).First(&tr).Error; err != nil {
		t.Fatal(err)
	}
	if tr.Status != "FAILED" || tr.FailureReason == nil || *tr.FailureReason != "INSUFFICIENT_FUNDS" {
		t.Errorf("transfer = %+v", tr)
	}
	assertLedgerInvariants(t, from, to)
}

func TestTransfer_UnknownWalletLeavesNoTraceAndKeyStaysUsable(t *testing.T) {
	from := newWallet(t, 100)
	key := uniqueKey(t, 0)
	svc := newService()

	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: 9_000_000_000, Amount: 10,
	})
	if err == nil {
		t.Fatal("expected wallet-not-found error")
	}
	if count(t, &model.IdempotencyRecord{}, "idempotency_key = ?", key) != 0 ||
		count(t, &model.Transfer{}, "idempotency_key = ?", key) != 0 {
		t.Fatal("failed attempt leaked rows")
	}

	// Same key is reusable now that the receiver exists.
	to := newWallet(t, 0)
	res, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 10,
	})
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestIdempotency_SequentialReplayReturnsOriginalAndMovesMoneyOnce(t *testing.T) {
	from, to := newWallet(t, 1000), newWallet(t, 0)
	key := uniqueKey(t, 0)
	svc := newService()
	r := service.CreateTransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 100}

	first, err := svc.CreateTransfer(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := svc.CreateTransfer(context.Background(), r)
		if err != nil || again.StatusCode != first.StatusCode || !sameJSON(t, again.Body, first.Body) {
			t.Fatalf("replay %d: %+v err=%v (first %s)", i, again, err, first.Body)
		}
	}
	if balance(t, from) != 900 || balance(t, to) != 100 {
		t.Errorf("balances = %d/%d", balance(t, from), balance(t, to))
	}
	if count(t, &model.Transfer{}, "idempotency_key = ?", key) != 1 {
		t.Error("duplicate transfer rows")
	}
	assertLedgerInvariants(t, from, to)
}

func TestIdempotency_SameKeyDifferentPayloadConflicts(t *testing.T) {
	from, to := newWallet(t, 1000), newWallet(t, 0)
	key := uniqueKey(t, 0)
	svc := newService()

	if _, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 100}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 200})
	if err == nil || !strings.Contains(err.Error(), domain.ErrIdempotencyConflict.Error()) {
		t.Fatalf("got %v, want idempotency conflict", err)
	}
	if balance(t, from) != 900 {
		t.Errorf("conflicting request moved money")
	}
}

// Many clients retry the same request at once (e.g. after a timeout). Exactly
// one transfer must happen and every caller must see the same answer.
func TestConcurrency_SameKeyFiredInParallelExecutesOnce(t *testing.T) {
	from, to := newWallet(t, 10_000), newWallet(t, 0)
	key := uniqueKey(t, 0)
	svc := newService()

	results := fire(t, 20, func(int) (*service.TransferResult, error) {
		return svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
			IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: 100,
		})
	})

	for i, r := range results {
		if r.StatusCode != 201 || !sameJSON(t, r.Body, results[0].Body) {
			t.Fatalf("request %d: %d %s (want identical %s)", i, r.StatusCode, r.Body, results[0].Body)
		}
	}
	if balance(t, from) != 9_900 || balance(t, to) != 100 {
		t.Errorf("balances = %d/%d, want 9900/100", balance(t, from), balance(t, to))
	}
	if n := count(t, &model.Transfer{}, "idempotency_key = ?", key); n != 1 {
		t.Errorf("%d transfer rows, want 1", n)
	}
	assertLedgerInvariants(t, from, to)
}

// The classic double-spend: balance covers 5 transfers, 20 try at once.
func TestConcurrency_NoDoubleSpend(t *testing.T) {
	const price, affordable, attempts = int64(100), 5, 20
	from, to := newWallet(t, price*affordable), newWallet(t, 0)
	svc := newService()

	results := fire(t, attempts, func(i int) (*service.TransferResult, error) {
		return svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
			IdempotencyKey: uniqueKey(t, i), FromWalletID: from, ToWalletID: to, Amount: price,
		})
	})

	var ok, rejected int
	for _, r := range results {
		switch r.StatusCode {
		case 201:
			ok++
		case 409:
			rejected++
		default:
			t.Fatalf("unexpected status %d: %s", r.StatusCode, r.Body)
		}
	}
	if ok != affordable || rejected != attempts-affordable {
		t.Errorf("succeeded=%d rejected=%d, want %d/%d", ok, rejected, affordable, attempts-affordable)
	}
	if balance(t, from) != 0 || balance(t, to) != price*affordable {
		t.Errorf("balances = %d/%d", balance(t, from), balance(t, to))
	}
	assertLedgerInvariants(t, from, to)
}

// Regression for the FK/lock deadlock: many transfers on the same pair of
// wallets must all complete without any 500s.
func TestConcurrency_ManyTransfersSameWalletPairAllSucceed(t *testing.T) {
	from, to := newWallet(t, 1_000_000), newWallet(t, 0)
	svc := newService()

	results := fire(t, 30, func(i int) (*service.TransferResult, error) {
		return svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
			IdempotencyKey: uniqueKey(t, i), FromWalletID: from, ToWalletID: to, Amount: 1000,
		})
	})
	for i, r := range results {
		if r.StatusCode != 201 {
			t.Fatalf("request %d: %d %s", i, r.StatusCode, r.Body)
		}
	}
	if balance(t, from) != 1_000_000-30*1000 || balance(t, to) != 30*1000 {
		t.Errorf("balances = %d/%d", balance(t, from), balance(t, to))
	}
}

// A->B and B->A at the same time is the textbook deadlock; ordered locking must prevent it.
func TestConcurrency_OppositeDirectionsDoNotDeadlockAndConserveMoney(t *testing.T) {
	a, b := newWallet(t, 10_000), newWallet(t, 10_000)
	svc := newService()

	results := fire(t, 40, func(i int) (*service.TransferResult, error) {
		from, to := a, b
		if i%2 == 1 {
			from, to = b, a
		}
		return svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
			IdempotencyKey: uniqueKey(t, i), FromWalletID: from, ToWalletID: to, Amount: 10,
		})
	})
	for i, r := range results {
		if r.StatusCode != 201 {
			t.Fatalf("request %d: %d %s", i, r.StatusCode, r.Body)
		}
	}
	// 20 each way, equal amounts: balances return to the start.
	if balance(t, a) != 10_000 || balance(t, b) != 10_000 {
		t.Errorf("balances = %d/%d, want 10000/10000", balance(t, a), balance(t, b))
	}
	assertLedgerInvariants(t, a, b)
}

// A ring of wallets with random-ish traffic: total money is conserved, no
// wallet goes negative (DB CHECK would error), and the ledger agrees with balances.
func TestConcurrency_RingOfWalletsConservesMoneyAndLedgerMatchesBalances(t *testing.T) {
	const wallets, start = 4, int64(0)
	const funding = int64(1_000)

	// Fund wallets via a funding wallet so the ledger explains every unit.
	source := newWallet(t, funding*wallets)
	ids := make([]int64, wallets)
	svc := newService()
	for i := range ids {
		ids[i] = newWallet(t, start)
		res, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
			IdempotencyKey: uniqueKey(t, 1000+i), FromWalletID: source, ToWalletID: ids[i], Amount: funding,
		})
		if err != nil || res.StatusCode != 201 {
			t.Fatalf("funding: %+v %v", res, err)
		}
	}

	fire(t, 60, func(i int) (*service.TransferResult, error) {
		from, to := ids[i%wallets], ids[(i+1)%wallets]
		return svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
			IdempotencyKey: uniqueKey(t, i), FromWalletID: from, ToWalletID: to, Amount: int64(10 + i%7),
		})
	})

	var total int64
	for _, id := range ids {
		bal := balance(t, id)
		total += bal
		if bal < 0 {
			t.Errorf("wallet %d negative: %d", id, bal)
		}
		if net := ledgerNet(t, id); net != bal-start {
			t.Errorf("wallet %d: ledger net %d != balance change %d", id, net, bal-start)
		}
	}
	if total != funding*wallets {
		t.Errorf("money not conserved: %d, want %d", total, funding*wallets)
	}
	assertLedgerInvariants(t, append(ids, source)...)
}

// ---- database constraints (defence in depth below the service) -----------

func TestSchema_RejectsNegativeBalanceAndDuplicateLedgerEntries(t *testing.T) {
	w := newWallet(t, 10)
	if err := testDB.Exec("UPDATE wallets SET balance = -1 WHERE id = ?", w).Error; err == nil {
		t.Error("negative balance should violate chk_wallet_balance")
	}

	from, to := newWallet(t, 100), newWallet(t, 0)
	res, err := newService().CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: uniqueKey(t, 0), FromWalletID: from, ToWalletID: to, Amount: 1,
	})
	if err != nil || res.StatusCode != 201 {
		t.Fatal(res, err)
	}
	var tr model.Transfer
	testDB.Where("from_wallet_id = ?", from).First(&tr)
	err = testDB.Exec(`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount) VALUES (?, ?, 'DEBIT', 1)`, tr.ID, from).Error
	if err == nil {
		t.Error("second DEBIT for the same transfer should violate uq_ledger_transfer_entry_type")
	}
}

// ---- HTTP end-to-end -----------------------------------------------------

func TestHTTP_EndToEnd(t *testing.T) {
	app := fiber.New()
	route.RegisterRoutes(app, route.Handlers{Transfer: handler.NewTransferHandler(newService())})

	from, to := newWallet(t, 100), newWallet(t, 0)
	key := uniqueKey(t, 0)
	send := func(amount int64) (int, string) {
		body := fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%d,"toWalletId":%d,"amount":%d}`, key, from, to, amount)
		req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	s1, b1 := send(40)
	s2, b2 := send(40) // replay
	s3, _ := send(41)  // same key, different payload

	if s1 != 201 || s2 != 201 || !sameJSON(t, []byte(b1), []byte(b2)) {
		t.Errorf("first=%d %s replay=%d %s", s1, b1, s2, b2)
	}
	if s3 != 409 {
		t.Errorf("conflicting payload status = %d, want 409", s3)
	}
	if balance(t, from) != 60 || balance(t, to) != 40 {
		t.Errorf("balances = %d/%d", balance(t, from), balance(t, to))
	}
}
