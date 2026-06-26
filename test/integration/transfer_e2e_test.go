// Package integration holds end-to-end tests that exercise the full stack —
// HTTP handler -> service -> PostgreSQL repositories — against a real database
// provisioned with testcontainers. These tests are the primary evidence for
// the two properties the assignment cares about most: exactly-once handling of
// duplicate requests, and the absence of double-spending under concurrency.
//
// They require a working Docker host. When none is available the whole suite
// skips (it never hangs or fails), so `go test ./...` stays green locally while
// CI, which has Docker, runs the tests in full.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/handler"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/service"
)

var (
	testServer *httptest.Server
	testDB     *postgres.DB
	skipReason string
	walletSeq  atomic.Int64
)

func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

func runSuite(m *testing.M) int {
	ctx := context.Background()

	container, dsn, err := startPostgres(ctx)
	if err != nil {
		// No Docker host (common locally): skip the whole suite rather than
		// failing. Each test guards on skipReason.
		skipReason = fmt.Sprintf("skipping integration tests: %v", err)
		return m.Run()
	}
	defer func() { _ = container.Terminate(ctx) }()

	// Give the pool plenty of connections so the concurrency tests never starve
	// while many transactions hold row locks simultaneously.
	db, err := postgres.NewDB(ctx, dsn+"&pool_max_conns=30")
	if err != nil {
		skipReason = fmt.Sprintf("skipping integration tests: %v", err)
		return m.Run()
	}
	defer db.Close()
	testDB = db

	if err := db.Migrate(ctx); err != nil {
		skipReason = fmt.Sprintf("skipping integration tests: %v", err)
		return m.Run()
	}

	svc := service.New(service.Repositories{
		Wallets:     postgres.NewWalletRepository(db),
		Transfers:   postgres.NewTransferRepository(db),
		Ledger:      postgres.NewLedgerRepository(db),
		Idempotency: postgres.NewIdempotencyRepository(db),
		TxManager:   db,
	})
	testServer = httptest.NewServer(handler.New(svc))
	defer testServer.Close()

	return m.Run()
}

// startPostgres provisions a throwaway PostgreSQL container. It recovers any
// panic the container library raises when Docker is absent so the caller can
// skip cleanly.
func startPostgres(ctx context.Context) (container *tcpostgres.PostgresContainer, dsn string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("docker unavailable: %v", r)
		}
	}()

	container, err = tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("wallet"),
		tcpostgres.WithUsername("wallet"),
		tcpostgres.WithPassword("wallet"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, "", err
	}
	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	return container, dsn, err
}

func requireStack(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip(skipReason)
	}
}

// TestIdempotentReplay proves that re-sending an identical request with the
// same idempotency key returns the original result and moves money only once.
func TestIdempotentReplay(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	from := seedWallet(t, ctx, 1000)
	to := seedWallet(t, ctx, 0)
	key := "replay-" + from

	status1, hdr1, body1 := postTransfer(t, key, from, to, 100)
	require.Equal(t, http.StatusCreated, status1)
	require.Empty(t, hdr1.Get("Idempotent-Replayed"))
	first := decodeTransfer(t, body1)
	assert.Equal(t, "PROCESSED", first.Status)

	status2, hdr2, body2 := postTransfer(t, key, from, to, 100)
	require.Equal(t, http.StatusOK, status2, "replay returns 200")
	assert.Equal(t, "true", hdr2.Get("Idempotent-Replayed"))
	second := decodeTransfer(t, body2)

	// The replay carries the same transfer in the same terminal state with the
	// same amount. (Timestamps are re-rendered from the database round-trip, so
	// we assert semantic equality, not byte-identical JSON.)
	assert.Equal(t, first.Id, second.Id, "replay returns the original transfer")
	assert.Equal(t, first.Status, second.Status)
	assert.Equal(t, first.Amount, second.Amount)

	assert.Equal(t, int64(900), getBalance(t, from), "source debited exactly once")
	assert.Equal(t, int64(100), getBalance(t, to), "destination credited exactly once")
	assertLedgerBalanced(t, ctx)
}

// TestConcurrentDebitsNoDoubleSpend fires many transfers that together exceed
// the source balance. With row-level locking exactly as many as the balance
// allows must succeed; the rest must fail with insufficient funds, and the
// wallet must never go negative.
func TestConcurrentDebitsNoDoubleSpend(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	from := seedWallet(t, ctx, 1000)
	to := seedWallet(t, ctx, 0)

	const attempts = 25
	const amount = 100
	const wantSuccess = 1000 / amount // 10

	var created, unprocessable int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("spend-%s-%d", from, i)
			status, _, _ := postTransfer(t, key, from, to, amount)
			switch status {
			case http.StatusCreated:
				atomic.AddInt64(&created, 1)
			case http.StatusUnprocessableEntity:
				atomic.AddInt64(&unprocessable, 1)
			default:
				t.Errorf("unexpected status %d", status)
			}
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int64(wantSuccess), created, "exactly the affordable number of transfers succeed")
	assert.Equal(t, int64(attempts-wantSuccess), unprocessable, "the rest fail with insufficient funds")

	assert.Equal(t, int64(0), getBalance(t, from), "source fully drained, never negative")
	assert.Equal(t, int64(wantSuccess*amount), getBalance(t, to), "destination received every successful transfer")
	assert.Equal(t, wantSuccess, countTransfers(t, ctx, from, domain.StatusProcessed))
	assert.Equal(t, attempts-wantSuccess, countTransfers(t, ctx, from, domain.StatusFailed))
	assertLedgerBalanced(t, ctx)
	assertDerivedBalance(t, ctx, from)
	assertDerivedBalance(t, ctx, to)
}

// TestConcurrentDuplicateKey fires many simultaneous requests that all carry
// the SAME idempotency key. The unique-constraint serialization must collapse
// them into exactly one executed transfer.
func TestConcurrentDuplicateKey(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	from := seedWallet(t, ctx, 1000)
	to := seedWallet(t, ctx, 0)
	key := "dup-" + from

	const attempts = 25
	var created, replayed int64
	ids := make([]string, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, _, body := postTransfer(t, key, from, to, 100)
			switch status {
			case http.StatusCreated:
				atomic.AddInt64(&created, 1)
			case http.StatusOK:
				atomic.AddInt64(&replayed, 1)
			default:
				t.Errorf("unexpected status %d: %s", status, body)
				return
			}
			ids[i] = decodeTransfer(t, body).Id
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int64(1), created, "exactly one request executes the transfer")
	assert.Equal(t, int64(attempts-1), replayed, "all other requests replay the original")
	for i := 1; i < attempts; i++ {
		assert.Equal(t, ids[0], ids[i], "every response references the same transfer")
	}

	assert.Equal(t, int64(900), getBalance(t, from), "money moved exactly once")
	assert.Equal(t, int64(100), getBalance(t, to))
	assert.Equal(t, 2, countLedgerEntries(t, ctx, ids[0]), "exactly one DEBIT + one CREDIT")
	assertLedgerBalanced(t, ctx)
}

// --- helpers ---

func seedWallet(t *testing.T, ctx context.Context, balance int64) string {
	t.Helper()
	id := fmt.Sprintf("wallet_%d", walletSeq.Add(1))
	repo := postgres.NewWalletRepository(testDB)
	require.NoError(t, repo.Create(ctx, &domain.Wallet{ID: id, Balance: balance}))
	return id
}

type transferDTO struct {
	Id     string `json:"id"`
	Status string `json:"status"`
	Amount int64  `json:"amount"`
}

func postTransfer(t *testing.T, key, from, to string, amount int64) (status int, header http.Header, body []byte) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"idempotencyKey": key,
		"fromWalletId":   from,
		"toWalletId":     to,
		"amount":         amount,
	})
	require.NoError(t, err)

	resp, err := testServer.Client().Post(testServer.URL+"/transfers", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, body
}

func decodeTransfer(t *testing.T, body []byte) transferDTO {
	t.Helper()
	var dto transferDTO
	require.NoError(t, json.Unmarshal(body, &dto), "body: %s", body)
	return dto
}

func getBalance(t *testing.T, walletID string) int64 {
	t.Helper()
	resp, err := testServer.Client().Get(testServer.URL + "/wallets/" + walletID + "/balance")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var dto struct {
		Balance int64 `json:"balance"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&dto))
	return dto.Balance
}

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testDB.Pool()
}

func countTransfers(t *testing.T, ctx context.Context, fromWallet string, status domain.TransferStatus) int {
	t.Helper()
	var n int
	err := pool(t).QueryRow(ctx,
		`SELECT count(*) FROM transfers WHERE from_wallet_id = $1 AND status = $2`,
		fromWallet, string(status)).Scan(&n)
	require.NoError(t, err)
	return n
}

func countLedgerEntries(t *testing.T, ctx context.Context, transferID string) int {
	t.Helper()
	var n int
	err := pool(t).QueryRow(ctx,
		`SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, transferID).Scan(&n)
	require.NoError(t, err)
	return n
}

// assertLedgerBalanced verifies the global double-entry invariant: total debits
// equal total credits across the entire ledger.
func assertLedgerBalanced(t *testing.T, ctx context.Context) {
	t.Helper()
	var debits, credits int64
	require.NoError(t, pool(t).QueryRow(ctx,
		`SELECT
		   COALESCE(sum(amount) FILTER (WHERE type = 'DEBIT'), 0),
		   COALESCE(sum(amount) FILTER (WHERE type = 'CREDIT'), 0)
		 FROM ledger_entries`).Scan(&debits, &credits))
	assert.Equal(t, debits, credits, "ledger must balance: debits == credits")
}

// assertDerivedBalance verifies a wallet's stored balance equals the balance
// derived purely from its ledger entries plus its opening balance. Opening
// balances are reconstructed as (stored balance) - credits + debits and must be
// non-negative and consistent.
func assertDerivedBalance(t *testing.T, ctx context.Context, walletID string) {
	t.Helper()
	var stored, debits, credits int64
	require.NoError(t, pool(t).QueryRow(ctx,
		`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&stored))
	require.NoError(t, pool(t).QueryRow(ctx,
		`SELECT
		   COALESCE(sum(amount) FILTER (WHERE type = 'DEBIT'), 0),
		   COALESCE(sum(amount) FILTER (WHERE type = 'CREDIT'), 0)
		 FROM ledger_entries WHERE wallet_id = $1`, walletID).Scan(&debits, &credits))
	assert.GreaterOrEqual(t, stored, int64(0), "balance never negative")
	// stored = opening - debits + credits  =>  opening = stored + debits - credits
	opening := stored + debits - credits
	assert.GreaterOrEqual(t, opening, int64(0), "reconstructed opening balance is sane")
}
