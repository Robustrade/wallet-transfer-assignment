package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/amantiwari/wallet-transfer/internal/adapter/httpapi"
	"github.com/amantiwari/wallet-transfer/internal/adapter/postgres"
	"github.com/amantiwari/wallet-transfer/internal/app/transfer"
	"github.com/amantiwari/wallet-transfer/internal/app/wallet"
	"github.com/amantiwari/wallet-transfer/internal/platform/system"
	"github.com/amantiwari/wallet-transfer/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.Shutdown()
	os.Exit(code)
}

func TestTransferIsAtomicIdempotentAndBalanced(t *testing.T) {
	server, raw := newServer(t)
	createWallet(t, server, "wallet_1", 500)
	createWallet(t, server, "wallet_2", 25)

	body := `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	first := postJSON(t, server.URL+"/transfers", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", first.Code, first.Body)
	}
	replay := postJSON(t, server.URL+"/transfers", body)
	if replay.Code != http.StatusCreated || replay.Body != first.Body {
		t.Fatalf("replay changed the response\nfirst=%s\nreplay=%s", first.Body, replay.Body)
	}

	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decode(t, first.Body, &created)
	if created.Status != "PROCESSED" {
		t.Fatalf("status=%s", created.Status)
	}
	got := get(t, server.URL+"/transfers/"+created.ID)
	if got.Code != http.StatusOK || !strings.Contains(got.Body, created.ID) {
		t.Fatalf("get transfer status=%d body=%s", got.Code, got.Body)
	}
	health := get(t, server.URL+"/health")
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body)
	}

	ledger := get(t, server.URL+"/transfers/"+created.ID+"/ledger")
	if ledger.Code != http.StatusOK {
		t.Fatalf("ledger status=%d body=%s", ledger.Code, ledger.Body)
	}
	var entries struct {
		Entries []struct {
			Type   string `json:"type"`
			Amount int64  `json:"amount"`
		} `json:"entries"`
	}
	decode(t, ledger.Body, &entries)
	if len(entries.Entries) != 2 || entries.Entries[0].Type != "DEBIT" || entries.Entries[1].Type != "CREDIT" {
		t.Fatalf("entries=%+v", entries.Entries)
	}
	if entries.Entries[0].Amount != 100 || entries.Entries[1].Amount != 100 {
		t.Fatal("ledger amounts are not equal")
	}
	if balance(t, server, "wallet_1") != 400 || balance(t, server, "wallet_2") != 125 {
		t.Fatalf("balances wallet_1=%d wallet_2=%d", balance(t, server, "wallet_1"), balance(t, server, "wallet_2"))
	}
	assertNoDrift(t, raw)
}

func TestInsufficientFundsIsDurable(t *testing.T) {
	server, raw := newServer(t)
	createWallet(t, server, "wallet_1", 40)
	createWallet(t, server, "wallet_2", 0)
	body := `{"idempotencyKey":"poor","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	first := postJSON(t, server.URL+"/transfers", body)
	if first.Code != http.StatusUnprocessableEntity || !strings.Contains(first.Body, `"FAILED"`) || !strings.Contains(first.Body, "INSUFFICIENT_FUNDS") {
		t.Fatalf("status=%d body=%s", first.Code, first.Body)
	}
	replay := postJSON(t, server.URL+"/transfers", body)
	if replay.Body != first.Body || replay.Code != http.StatusUnprocessableEntity {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body)
	}
	if balance(t, server, "wallet_1") != 40 {
		t.Fatalf("balance changed to %d", balance(t, server, "wallet_1"))
	}
	assertNoDrift(t, raw)
}

func TestIdempotencyKeyRejectsDifferentPayload(t *testing.T) {
	server, _ := newServer(t)
	createWallet(t, server, "wallet_1", 100)
	createWallet(t, server, "wallet_2", 0)
	createWallet(t, server, "wallet_3", 0)
	ok := postJSON(t, server.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":10}`)
	if ok.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", ok.Code, ok.Body)
	}
	conflict := postJSON(t, server.URL+"/transfers", `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_3","amount":10}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body, "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("status=%d body=%s", conflict.Code, conflict.Body)
	}
	if balance(t, server, "wallet_1") != 90 || balance(t, server, "wallet_3") != 0 {
		t.Fatal("conflict moved money")
	}
}

func TestMissingWalletDoesNotConsumeIdempotencyKey(t *testing.T) {
	server, _ := newServer(t)
	createWallet(t, server, "wallet_2", 0)
	body := `{"idempotencyKey":"later","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":5}`
	missing := postJSON(t, server.URL+"/transfers", body)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", missing.Code, missing.Body)
	}
	createWallet(t, server, "wallet_1", 20)
	created := postJSON(t, server.URL+"/transfers", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("retry status=%d body=%s", created.Code, created.Body)
	}
}

func TestConcurrentDebitsCannotOverspend(t *testing.T) {
	server, raw := newServer(t)
	createWallet(t, server, "source", 1000)
	createWallet(t, server, "sink", 0)

	const workers = 20
	const amount = int64(100)
	codes := make([]int, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			body := fmt.Sprintf(`{"idempotencyKey":"debit-%d","fromWalletId":"source","toWalletId":"sink","amount":%d}`, i, amount)
			res, err := doPost(server.URL+"/transfers", body)
			if err != nil {
				errCh <- err
				return
			}
			codes[i] = res.Code
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	processed, failed := 0, 0
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			processed++
		case http.StatusUnprocessableEntity:
			failed++
		default:
			t.Fatalf("worker %d status %d", i, code)
		}
	}
	if processed != 10 || failed != 10 {
		t.Fatalf("processed=%d failed=%d", processed, failed)
	}
	if balance(t, server, "source") != 0 || balance(t, server, "sink") != 1000 {
		t.Fatalf("source=%d sink=%d", balance(t, server, "source"), balance(t, server, "sink"))
	}
	assertNoDrift(t, raw)
	assertLedgerPairs(t, raw)
}

func TestConcurrentReplayOfOneKeyMovesMoneyOnce(t *testing.T) {
	server, raw := newServer(t)
	createWallet(t, server, "wallet_1", 500)
	createWallet(t, server, "wallet_2", 0)

	const workers = 12
	bodies := make([]string, workers)
	codes := make([]int, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := doPost(server.URL+"/transfers", `{"idempotencyKey":"same-key","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":80}`)
			if err != nil {
				errCh <- err
				return
			}
			codes[i] = res.Code
			bodies[i] = res.Body
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	for i, code := range codes {
		if code != http.StatusCreated {
			t.Fatalf("worker %d status %d body %s", i, code, bodies[i])
		}
		if bodies[i] != bodies[0] {
			t.Fatalf("responses diverged\n%s\n%s", bodies[0], bodies[i])
		}
	}
	if balance(t, server, "wallet_1") != 420 || balance(t, server, "wallet_2") != 80 {
		t.Fatalf("wallet_1=%d wallet_2=%d", balance(t, server, "wallet_1"), balance(t, server, "wallet_2"))
	}
	assertNoDrift(t, raw)
}

func TestOppositeTransfersDoNotDeadlock(t *testing.T) {
	server, raw := newServer(t)
	createWallet(t, server, "wallet_a", 200)
	createWallet(t, server, "wallet_b", 200)

	const each = 8
	var wg sync.WaitGroup
	errCh := make(chan string, each*2)
	start := make(chan struct{})
	submit := func(key, from, to string) {
		defer wg.Done()
		<-start
		body := fmt.Sprintf(`{"idempotencyKey":"%s","fromWalletId":"%s","toWalletId":"%s","amount":10}`, key, from, to)
		res, err := doPost(server.URL+"/transfers", body)
		if err != nil {
			errCh <- err.Error()
			return
		}
		if res.Code != http.StatusCreated && res.Code != http.StatusUnprocessableEntity {
			errCh <- fmt.Sprintf("%s status %d body %s", key, res.Code, res.Body)
		}
	}
	wg.Add(each * 2)
	for i := 0; i < each; i++ {
		go submit(fmt.Sprintf("ab-%d", i), "wallet_a", "wallet_b")
		go submit(fmt.Sprintf("ba-%d", i), "wallet_b", "wallet_a")
	}
	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("opposite transfers did not finish; possible deadlock")
	}
	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
	if balance(t, server, "wallet_a")+balance(t, server, "wallet_b") != 400 {
		t.Fatalf("conservation failed: a=%d b=%d", balance(t, server, "wallet_a"), balance(t, server, "wallet_b"))
	}
	assertNoDrift(t, raw)
	assertLedgerPairs(t, raw)
}

func TestDatabaseRejectsInvalidLedgerAndPendingCommit(t *testing.T) {
	_, raw := newServer(t)
	ctx := context.Background()
	if _, err := raw.Exec(ctx, `INSERT INTO wallets (id, initial_balance, balance, created_at, updated_at) VALUES ('wallet_1', 10, 10, now(), now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(ctx, `INSERT INTO wallets (id, initial_balance, balance, created_at, updated_at) VALUES ('wallet_2', 0, 0, now(), now())`); err != nil {
		t.Fatal(err)
	}

	tx, err := raw.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		VALUES ('11111111-1111-4111-8111-111111111111', 'pending-key', 'wallet_1', 'wallet_2', 1, 'PENDING', now(), now())
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("committed a PENDING transfer")
	}

	tx, err = raw.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at)
		VALUES ('22222222-2222-4222-8222-222222222222', 'unbalanced-key', 'wallet_1', 'wallet_2', 1, 'PROCESSED', now(), now())
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("committed a processed transfer without a ledger")
	}

	if _, err := raw.Exec(ctx, `UPDATE wallets SET balance = -1 WHERE id = 'wallet_1'`); err == nil {
		t.Fatal("negative balance was accepted")
	}
}

func newServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	dsn := testutil.DSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log := slog.New(slog.DiscardHandler)
	db, err := postgres.Open(ctx, postgres.OpenConfig{
		DSN:                  dsn,
		MaxConns:             16,
		MinConns:             0,
		MaxConnLifetime:      time.Minute,
		MaxConnIdleTime:      time.Minute,
		ConnectRetries:       3,
		ConnectRetryInterval: 100 * time.Millisecond,
		LockTimeout:          10 * time.Second,
		StatementTimeout:     15 * time.Second,
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	raw, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)

	clock := system.Clock{}
	router := httpapi.NewRouter(httpapi.Dependencies{
		Wallets:        wallet.NewService(db, db.WalletReader(), clock, log),
		Transfers:      transfer.NewService(db, db.TransferReader(), db.LedgerReader(), clock, system.IDs{}, log),
		DB:             db,
		Logger:         log,
		RequestTimeout: 20 * time.Second,
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, raw
}

func createWallet(t *testing.T, server *httptest.Server, id string, balance int64) {
	t.Helper()
	res := postJSON(t, server.URL+"/wallets", fmt.Sprintf(`{"id":%q,"initialBalance":%d}`, id, balance))
	if res.Code != http.StatusCreated {
		t.Fatalf("create %s status=%d body=%s", id, res.Code, res.Body)
	}
}

func balance(t *testing.T, server *httptest.Server, id string) int64 {
	t.Helper()
	res := get(t, server.URL+"/wallets/"+id)
	if res.Code != http.StatusOK {
		t.Fatalf("get %s status=%d body=%s", id, res.Code, res.Body)
	}
	var payload struct {
		Balance int64 `json:"balance"`
	}
	decode(t, res.Body, &payload)
	return payload.Balance
}

type httpResult struct {
	Code int
	Body string
}

func postJSON(t *testing.T, url, body string) httpResult {
	t.Helper()
	res, err := doPost(url, body)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func doPost(url, body string) (httpResult, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return httpResult{}, err
	}
	defer func() { _ = res.Body.Close() }()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return httpResult{}, err
	}
	return httpResult{Code: res.StatusCode, Body: string(payload)}, nil
}

func get(t *testing.T, url string) httpResult {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResult{Code: res.StatusCode, Body: string(payload)}
}

func decode(t *testing.T, body string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), dst); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func assertNoDrift(t *testing.T, raw *pgxpool.Pool) {
	t.Helper()
	var drifts int
	err := raw.QueryRow(context.Background(), `
		SELECT count(*) FROM (
			SELECT w.id
			FROM wallets w
			LEFT JOIN ledger_entries e ON e.wallet_id = w.id
			GROUP BY w.id, w.balance, w.initial_balance
			HAVING w.balance <> w.initial_balance
				+ COALESCE(SUM(CASE WHEN e.entry_type = 'CREDIT' THEN e.amount ELSE -e.amount END), 0)
		) drifted
	`).Scan(&drifts)
	if err != nil {
		t.Fatal(err)
	}
	if drifts != 0 {
		t.Fatalf("%d wallets drifted from the ledger", drifts)
	}
}

func assertLedgerPairs(t *testing.T, raw *pgxpool.Pool) {
	t.Helper()
	var bad int
	err := raw.QueryRow(context.Background(), `
		SELECT count(*) FROM (
			SELECT t.id
			FROM transfers t
			LEFT JOIN ledger_entries e ON e.transfer_id = t.id
			WHERE t.status = 'PROCESSED'
			GROUP BY t.id, t.amount
			HAVING count(*) FILTER (WHERE e.entry_type = 'DEBIT') <> 1
				OR count(*) FILTER (WHERE e.entry_type = 'CREDIT') <> 1
				OR COALESCE(sum(e.amount) FILTER (WHERE e.entry_type = 'DEBIT'), 0) <> t.amount
				OR COALESCE(sum(e.amount) FILTER (WHERE e.entry_type = 'CREDIT'), 0) <> t.amount
		) bad
	`).Scan(&bad)
	if err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("%d processed transfers have an unbalanced ledger", bad)
	}
}
