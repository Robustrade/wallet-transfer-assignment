package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/service"
)

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPostgresTransferReplayAndLedger(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	mustWallet(t, svc, "wallet_1", 500)
	mustWallet(t, svc, "wallet_2", 200)

	req := service.Request{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	first := mustTransfer(t, svc, req)
	if first.Status != http.StatusCreated {
		t.Fatalf("status = %d body = %s", first.Status, first.Body)
	}
	second := mustTransfer(t, svc, req)
	if !second.Replayed || !bytes.Equal(first.Body, second.Body) {
		t.Fatalf("replay body = %s", second.Body)
	}

	_, err := svc.CreateTransfer(ctx, service.Request{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("mismatch err = %v", err)
	}

	from := mustGetWallet(t, svc, "wallet_1")
	to := mustGetWallet(t, svc, "wallet_2")
	if from.Balance != 400 || to.Balance != 300 {
		t.Fatalf("balances = %d %d", from.Balance, to.Balance)
	}
	checkInvariants(t)
}

func TestPostgresInsufficientFundsReplay(t *testing.T) {
	svc := newService(t)
	mustWallet(t, svc, "wallet_1", 10)
	mustWallet(t, svc, "wallet_2", 0)
	req := service.Request{IdempotencyKey: "short", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 11}

	first := mustTransfer(t, svc, req)
	second := mustTransfer(t, svc, req)
	if first.Status != http.StatusUnprocessableEntity || !second.Replayed || !bytes.Equal(first.Body, second.Body) {
		t.Fatalf("first=%d second replayed=%v", first.Status, second.Replayed)
	}
	if mustGetWallet(t, svc, "wallet_1").Balance != 10 || mustGetWallet(t, svc, "wallet_2").Balance != 0 {
		t.Fatal("failed transfer changed a balance")
	}
	checkInvariants(t)
}

func TestPostgresMissingWalletDoesNotConsumeKey(t *testing.T) {
	svc := newService(t)
	req := service.Request{IdempotencyKey: "later", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 5}
	if _, err := svc.CreateTransfer(context.Background(), req); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	mustWallet(t, svc, "wallet_1", 20)
	mustWallet(t, svc, "wallet_2", 0)
	result := mustTransfer(t, svc, req)
	if result.Status != http.StatusCreated || result.Replayed {
		t.Fatalf("status=%d replayed=%v", result.Status, result.Replayed)
	}
	checkInvariants(t)
}

func TestPostgresConcurrentDebitsDoNotOverspend(t *testing.T) {
	svc := newService(t)
	mustWallet(t, svc, "src", 10)
	mustWallet(t, svc, "dst", 0)

	const attempts = 25
	codes := make([]int, attempts)
	errs := make([]error, attempts)
	runParallel(t, attempts, func(i int) {
		result, err := svc.CreateTransfer(context.Background(), service.Request{
			IdempotencyKey: fmt.Sprintf("debit-%d", i),
			FromWalletID:   "src",
			ToWalletID:     "dst",
			Amount:         1,
		})
		errs[i] = err
		codes[i] = result.Status
	})

	var processed, failed int
	for i := range attempts {
		if errs[i] != nil {
			t.Fatalf("transfer %d: %v", i, errs[i])
		}
		switch codes[i] {
		case http.StatusCreated:
			processed++
		case http.StatusUnprocessableEntity:
			failed++
		default:
			t.Fatalf("status %d", codes[i])
		}
	}
	if processed != 10 || failed != 15 {
		t.Fatalf("processed=%d failed=%d", processed, failed)
	}
	if mustGetWallet(t, svc, "src").Balance != 0 || mustGetWallet(t, svc, "dst").Balance != 10 {
		t.Fatal("concurrent debits left an unexpected balance")
	}
	checkInvariants(t)
}

func TestPostgresConcurrentReplayOfOneKey(t *testing.T) {
	svc := newService(t)
	mustWallet(t, svc, "src", 50)
	mustWallet(t, svc, "dst", 0)

	const attempts = 20
	results := make([]service.Result, attempts)
	errs := make([]error, attempts)
	runParallel(t, attempts, func(i int) {
		results[i], errs[i] = svc.CreateTransfer(context.Background(), service.Request{
			IdempotencyKey: "same-key",
			FromWalletID:   "src",
			ToWalletID:     "dst",
			Amount:         15,
		})
	})

	var originals int
	var body []byte
	for i := range attempts {
		if errs[i] != nil {
			t.Fatalf("transfer %d: %v", i, errs[i])
		}
		if results[i].Status != http.StatusCreated {
			t.Fatalf("status = %d body = %s", results[i].Status, results[i].Body)
		}
		if body == nil {
			body = results[i].Body
		}
		if !bytes.Equal(body, results[i].Body) {
			t.Fatal("callers observed different bodies")
		}
		if !results[i].Replayed {
			originals++
		}
	}
	if originals != 1 {
		t.Fatalf("original responses = %d", originals)
	}
	if mustGetWallet(t, svc, "src").Balance != 35 || mustGetWallet(t, svc, "dst").Balance != 15 {
		t.Fatal("replayed key moved money more than once")
	}
	checkInvariants(t)
}

func TestPostgresOpposingTransfersConserveMoney(t *testing.T) {
	svc := newService(t)
	mustWallet(t, svc, "acct_a", 50)
	mustWallet(t, svc, "acct_b", 50)

	const each = 8
	errs := make([]error, each*2)
	runParallel(t, each*2, func(i int) {
		req := service.Request{Amount: 10, IdempotencyKey: fmt.Sprintf("opp-%d", i)}
		if i < each {
			req.FromWalletID = "acct_a"
			req.ToWalletID = "acct_b"
		} else {
			req.FromWalletID = "acct_b"
			req.ToWalletID = "acct_a"
		}
		_, errs[i] = svc.CreateTransfer(context.Background(), req)
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d: %v", i, err)
		}
	}
	left := mustGetWallet(t, svc, "acct_a")
	right := mustGetWallet(t, svc, "acct_b")
	if left.Balance < 0 || right.Balance < 0 || left.Balance+right.Balance != 100 {
		t.Fatalf("balances = %d + %d", left.Balance, right.Balance)
	}
	checkInvariants(t)
}

func TestPostgresSecondDebitIsRejected(t *testing.T) {
	svc := newService(t)
	mustWallet(t, svc, "wallet_1", 30)
	mustWallet(t, svc, "wallet_2", 0)
	result := mustTransfer(t, svc, service.Request{
		IdempotencyKey: "once",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         10,
	})
	var view service.TransferView
	if err := json.Unmarshal(result.Body, &view); err != nil {
		t.Fatal(err)
	}
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
		VALUES ('wallet_1', $1::uuid, 'DEBIT', 10)
	`, view.TransferID)
	if err == nil {
		t.Fatal("database accepted a second debit")
	}
	checkInvariants(t)
}

func TestHTTPReplayReturnsOriginalBody(t *testing.T) {
	svc := newService(t)
	mustWallet(t, svc, "wallet_1", 40)
	mustWallet(t, svc, "wallet_2", 0)
	srv := httptest.NewServer(httpapi.New(svc))
	t.Cleanup(srv.Close)

	payload := `{"idempotencyKey":"http_key","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":12}`
	first := post(t, srv.URL+"/transfers", payload)
	second := post(t, srv.URL+"/transfers", payload)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d %d", first.Code, second.Code)
	}
	if first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("first response was marked as a replay")
	}
	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("replay header missing")
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("bodies differ: %s vs %s", first.Body.String(), second.Body.String())
	}
	checkInvariants(t)
}

func newService(t *testing.T) *service.Service {
	t.Helper()
	return service.New(requireDB(t))
}

func mustWallet(t *testing.T, svc *service.Service, id string, balance int64) {
	t.Helper()
	if _, err := svc.CreateWallet(context.Background(), id, balance); err != nil {
		t.Fatal(err)
	}
}

func mustGetWallet(t *testing.T, svc *service.Service, id string) service.WalletView {
	t.Helper()
	view, err := svc.GetWallet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func mustTransfer(t *testing.T, svc *service.Service, req service.Request) service.Result {
	t.Helper()
	result, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func runParallel(t *testing.T, n int, fn func(i int)) {
	t.Helper()
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			<-start
			fn(i)
		}(i)
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
		t.Fatal("timed out, possible lock deadlock")
	}
}

func post(t *testing.T, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	// The URL is only used to build a request against the handler via the client.
	res, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	recorder := httptest.NewRecorder()
	recorder.Code = res.StatusCode
	for key, values := range res.Header {
		for _, value := range values {
			recorder.Header().Add(key, value)
		}
	}
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Body.Write(payload); err != nil {
		t.Fatal(err)
	}
	return recorder
}

func checkInvariants(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	rows, err := testPool.Query(ctx, `
		SELECT w.id,
		       w.balance,
		       w.initial_balance
		         + COALESCE(SUM(e.amount) FILTER (WHERE e.entry_type = 'CREDIT'), 0)
		         - COALESCE(SUM(e.amount) FILTER (WHERE e.entry_type = 'DEBIT'), 0) AS derived
		FROM wallets w
		LEFT JOIN ledger_entries e ON e.wallet_id = w.id
		GROUP BY w.id, w.balance, w.initial_balance
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var balance, derived int64
		if err := rows.Scan(&id, &balance, &derived); err != nil {
			t.Fatal(err)
		}
		if balance != derived {
			t.Fatalf("wallet %s balance %d derived %d", id, balance, derived)
		}
		if balance < 0 {
			t.Fatalf("wallet %s balance %d", id, balance)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var debits, credits int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE entry_type = 'DEBIT'), 0),
		       COALESCE(SUM(amount) FILTER (WHERE entry_type = 'CREDIT'), 0)
		FROM ledger_entries
	`).Scan(&debits, &credits); err != nil {
		t.Fatal(err)
	}
	if debits != credits {
		t.Fatalf("debits %d credits %d", debits, credits)
	}

	var badProcessed, failedWithLedger, pending int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT t.id
			FROM transfers t
			LEFT JOIN ledger_entries e ON e.transfer_id = t.id
			WHERE t.status = 'PROCESSED'
			GROUP BY t.id
			HAVING count(e.id) <> 2 OR min(e.amount) <> max(e.amount)
		) bad
	`).Scan(&badProcessed); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM transfers t
		JOIN ledger_entries e ON e.transfer_id = t.id
		WHERE t.status = 'FAILED'
	`).Scan(&failedWithLedger); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE status = 'PENDING'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if badProcessed != 0 || failedWithLedger != 0 || pending != 0 {
		t.Fatalf("bad processed=%d failed with ledger=%d pending=%d", badProcessed, failedWithLedger, pending)
	}
}
