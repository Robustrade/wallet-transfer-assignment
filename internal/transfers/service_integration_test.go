package transfers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pgadapter "wallet-transfer-assignment/internal/adapters/postgres"
	"wallet-transfer-assignment/internal/cache"
	"wallet-transfer-assignment/internal/idempotency"
	"wallet-transfer-assignment/internal/transfers"
)

func TestTransfer_Success_IdempotentReplay(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t, ctx)

	walletRepo := pgadapter.NewWalletRepository(pool)
	transferRepo := pgadapter.NewTransferRepository(pool)
	ledgerRepo := pgadapter.NewLedgerRepository(pool)
	idempoRepo := pgadapter.NewIdempotencyRepository(pool)

	idempoCache := cache.NewLRUTTL[idempotency.Key, idempotency.StoredResponse](
		cache.WithMaxEntries(1000),
		cache.WithDefaultTTL(1*time.Minute),
	)
	cachedIdempo := idempotency.NewCachedStore(idempoRepo, idempoCache)

	svc := transfers.NewService(transfers.ServiceDeps{
		Wallets:     walletRepo,
		Transfers:   transferRepo,
		Ledger:      ledgerRepo,
		Idempotency: cachedIdempo,
		DB:          pool,
	})

	from, err := walletRepo.Create(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	to, err := walletRepo.Create(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	req := transfers.CreateRequest{
		IdempotencyKey: "idem-1",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	}

	status1, body1, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if status1 != 201 {
		t.Fatalf("expected 201, got %d", status1)
	}
	status2, body2, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if status1 != status2 {
		t.Fatalf("status mismatch: %d vs %d", status1, status2)
	}
	if string(body1) != string(body2) {
		t.Fatalf("body mismatch: %s vs %s", string(body1), string(body2))
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("expected 2 ledger rows, got %d", ledgerCount)
	}

	var resp transfers.CreateResponse
	if err := json.Unmarshal(body1, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.State != transfers.StateProcessed || resp.TransferID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// Ledger consistency: exactly one DEBIT and one CREDIT with same amount.
	rows, err := pool.Query(ctx, `SELECT entry_type, amount FROM ledger_entries WHERE transfer_id = $1`, resp.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	amountByType := map[string]int64{}
	for rows.Next() {
		var entryType string
		var amt int64
		if err := rows.Scan(&entryType, &amt); err != nil {
			t.Fatal(err)
		}
		amountByType[entryType] += amt
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if amountByType["DEBIT"] != 100 || amountByType["CREDIT"] != 100 || len(amountByType) != 2 {
		t.Fatalf("unexpected ledger by type: %+v", amountByType)
	}

	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM transfers WHERE transfer_id = $1`, resp.TransferID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(transfers.StateProcessed) {
		t.Fatalf("expected PROCESSED, got %s", state)
	}
}

func TestTransfer_IdempotencyMismatch_409(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t, ctx)

	walletRepo := pgadapter.NewWalletRepository(pool)
	transferRepo := pgadapter.NewTransferRepository(pool)
	ledgerRepo := pgadapter.NewLedgerRepository(pool)
	idempoRepo := pgadapter.NewIdempotencyRepository(pool)
	cachedIdempo := idempotency.NewCachedStore(idempoRepo, cache.NewLRUTTL[idempotency.Key, idempotency.StoredResponse]())

	svc := transfers.NewService(transfers.ServiceDeps{
		Wallets:     walletRepo,
		Transfers:   transferRepo,
		Ledger:      ledgerRepo,
		Idempotency: cachedIdempo,
		DB:          pool,
	})

	from, err := walletRepo.Create(ctx, 500)
	if err != nil {
		t.Fatalf("create from-wallet: %v", err)
	}
	to, err := walletRepo.Create(ctx, 0)
	if err != nil {
		t.Fatalf("create to-wallet: %v", err)
	}

	req1 := transfers.CreateRequest{IdempotencyKey: "idem-2", FromWalletID: from.ID, ToWalletID: to.ID, Amount: 100}
	req2 := transfers.CreateRequest{IdempotencyKey: "idem-2", FromWalletID: from.ID, ToWalletID: to.ID, Amount: 101}

	_, _, _ = svc.Create(ctx, req1)

	status, body, err := svc.Create(ctx, req2)
	if status != 409 {
		t.Fatalf("expected 409, got %d (err=%v, body=%s)", status, err, string(body))
	}
}

func TestTransfer_InsufficientFunds_NoLedger(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t, ctx)

	walletRepo := pgadapter.NewWalletRepository(pool)
	transferRepo := pgadapter.NewTransferRepository(pool)
	ledgerRepo := pgadapter.NewLedgerRepository(pool)
	idempoRepo := pgadapter.NewIdempotencyRepository(pool)
	cachedIdempo := idempotency.NewCachedStore(idempoRepo, cache.NewLRUTTL[idempotency.Key, idempotency.StoredResponse]())

	svc := transfers.NewService(transfers.ServiceDeps{
		Wallets:     walletRepo,
		Transfers:   transferRepo,
		Ledger:      ledgerRepo,
		Idempotency: cachedIdempo,
		DB:          pool,
	})

	from, _ := walletRepo.Create(ctx, 50)
	to, _ := walletRepo.Create(ctx, 0)

	status1, body1, _ := svc.Create(ctx, transfers.CreateRequest{
		IdempotencyKey: "idem-3",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	})
	if status1 != 409 {
		t.Fatalf("expected 409, got %d", status1)
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("expected 0 ledger rows, got %d", ledgerCount)
	}

	w, err := walletRepo.Get(ctx, from.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance != 50 {
		t.Fatalf("expected balance 50, got %d", w.Balance)
	}

	// Retry with same idempotency key should be a stable replay (safe state transition).
	status2, body2, _ := svc.Create(ctx, transfers.CreateRequest{
		IdempotencyKey: "idem-3",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100,
	})
	if status2 != status1 {
		t.Fatalf("expected replay status %d, got %d", status1, status2)
	}
	if string(body2) != string(body1) {
		t.Fatalf("expected replay body to match. got=%s want=%s", string(body2), string(body1))
	}

	var failedTransfers int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers WHERE state='FAILED'`).Scan(&failedTransfers); err != nil {
		t.Fatal(err)
	}
	if failedTransfers != 1 {
		t.Fatalf("expected 1 FAILED transfer, got %d", failedTransfers)
	}
}

func TestTransfer_ConcurrentDebits_NoDoubleSpend(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t, ctx)

	walletRepo := pgadapter.NewWalletRepository(pool)
	transferRepo := pgadapter.NewTransferRepository(pool)
	ledgerRepo := pgadapter.NewLedgerRepository(pool)
	idempoRepo := pgadapter.NewIdempotencyRepository(pool)
	cachedIdempo := idempotency.NewCachedStore(idempoRepo, cache.NewLRUTTL[idempotency.Key, idempotency.StoredResponse]())

	svc := transfers.NewService(transfers.ServiceDeps{
		Wallets:     walletRepo,
		Transfers:   transferRepo,
		Ledger:      ledgerRepo,
		Idempotency: cachedIdempo,
		DB:          pool,
	})

	from, _ := walletRepo.Create(ctx, 1000)
	to, _ := walletRepo.Create(ctx, 0)

	const (
		n      = 50
		amount = 30
	)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, _, _ = svc.Create(ctx, transfers.CreateRequest{
				IdempotencyKey: "idem-conc-" + strconv.Itoa(i),
				FromWalletID:   from.ID,
				ToWalletID:     to.ID,
				Amount:         amount,
			})
		}()
	}
	wg.Wait()

	wFrom, _ := walletRepo.Get(ctx, from.ID)
	wTo, _ := walletRepo.Get(ctx, to.ID)

	if wFrom.Balance < 0 {
		t.Fatalf("from wallet balance negative: %d", wFrom.Balance)
	}
	if wFrom.Balance+wTo.Balance != 1000 {
		t.Fatalf("money not conserved: from=%d to=%d", wFrom.Balance, wTo.Balance)
	}

	var processed int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers WHERE state='PROCESSED'`).Scan(&processed); err != nil {
		t.Fatal(err)
	}
	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != processed*2 {
		t.Fatalf("ledger mismatch: ledger=%d processed=%d", ledgerCount, processed)
	}
}

func TestTransfer_ConcurrentSameIdempotencyKey_SameResponse_NoDuplicateSideEffects(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t, ctx)

	walletRepo := pgadapter.NewWalletRepository(pool)
	transferRepo := pgadapter.NewTransferRepository(pool)
	ledgerRepo := pgadapter.NewLedgerRepository(pool)
	idempoRepo := pgadapter.NewIdempotencyRepository(pool)
	cachedIdempo := idempotency.NewCachedStore(idempoRepo, cache.NewLRUTTL[idempotency.Key, idempotency.StoredResponse]())

	svc := transfers.NewService(transfers.ServiceDeps{
		Wallets:     walletRepo,
		Transfers:   transferRepo,
		Ledger:      ledgerRepo,
		Idempotency: cachedIdempo,
		DB:          pool,
	})

	from, _ := walletRepo.Create(ctx, 500)
	to, _ := walletRepo.Create(ctx, 0)

	req := transfers.CreateRequest{IdempotencyKey: "idem-race-1", FromWalletID: from.ID, ToWalletID: to.ID, Amount: 100}

	var wg sync.WaitGroup
	wg.Add(2)

	type result struct {
		status int
		body   []byte
		err    error
	}
	results := make([]result, 2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			defer wg.Done()
			st, b, err := svc.Create(ctx, req)
			results[i] = result{status: st, body: b, err: err}
		}()
	}
	wg.Wait()

	if results[0].err != nil || results[1].err != nil {
		t.Fatalf("unexpected errs: %v / %v", results[0].err, results[1].err)
	}
	if results[0].status != results[1].status || string(results[0].body) != string(results[1].body) {
		t.Fatalf("expected same replay response: r0=%d %s r1=%d %s", results[0].status, string(results[0].body), results[1].status, string(results[1].body))
	}

	var transfersCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM transfers WHERE idempotency_key = $1`, "idem-race-1").Scan(&transfersCount); err != nil {
		t.Fatal(err)
	}
	if transfersCount != 1 {
		t.Fatalf("expected 1 transfer row, got %d", transfersCount)
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("expected 2 ledger rows, got %d", ledgerCount)
	}
}

func newTestDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" {
		t.Skip("set TEST_DATABASE_URL (or DATABASE_URL) to run integration tests against Postgres")
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skip(fmt.Sprintf("postgres not reachable (%v); set TEST_DATABASE_URL to a running Postgres", err))
	}

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	migrationPath := filepath.Join(repoRoot, "db", "migrations", "001_init.up.sql")
	mig, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, string(mig)); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return pool
}
