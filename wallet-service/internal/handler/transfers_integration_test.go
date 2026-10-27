package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wallet-service/internal/service"
	"wallet-service/internal/testutil"
)

func TestHTTPTransferReplayAndFailure(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	server := httptest.NewServer(NewRouter(service.New(pool)))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 10 * time.Second
	post := func(key string, amount int) (int, string, string) {
		t.Helper()
		body := fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":%d}`, key, amount)
		response, err := client.Post(server.URL+"/transfers", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		id := response.Header.Get("X-Request-ID")
		if id == "" || response.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing response headers")
		}
		return response.StatusCode, string(data), id
	}
	status, success, id1 := post("private-key-success", 100)
	if status != 201 || !strings.Contains(success, `"status":"PROCESSED"`) {
		t.Fatalf("success: %d %s", status, success)
	}
	status, replay, id2 := post("private-key-success", 100)
	if status != 201 || replay != success || id1 == id2 {
		t.Fatalf("replay: %d %s", status, replay)
	}
	status, failure, id3 := post("private-key-failure", 1000)
	if status != 422 || !strings.Contains(failure, `"status":"FAILED"`) || !strings.Contains(failure, `"code":"INSUFFICIENT_FUNDS"`) {
		t.Fatalf("failure: %d %s", status, failure)
	}
	status, replay, id4 := post("private-key-failure", 1000)
	if status != 422 || replay != failure {
		t.Fatalf("failure replay: %d %s", status, replay)
	}
	status, conflict, id5 := post("private-key-success", 101)
	if status != 409 || strings.TrimSpace(conflict) != `{"error":{"code":"IDEMPOTENCY_CONFLICT"}}` {
		t.Fatalf("conflict: %d %s", status, conflict)
	}
	var from, to, processed, failed, ledger, matching, keys, incomplete int
	err := pool.QueryRow(t.Context(), `SELECT
		(SELECT balance_minor FROM wallets WHERE id='wallet_1'),
		(SELECT balance_minor FROM wallets WHERE id='wallet_2'),
		(SELECT count(*) FROM transfers WHERE status='PROCESSED'),
		(SELECT count(*) FROM transfers WHERE status='FAILED'),
		(SELECT count(*) FROM ledger_entries),
		(SELECT count(*) FROM ledger_entries e JOIN transfers t ON e.transfer_id=t.id
		 WHERE t.status='PROCESSED' AND e.amount_minor=100 AND e.amount_minor=t.amount_minor
		 AND e.wallet_id=CASE e.entry_type WHEN 'DEBIT' THEN t.from_wallet_id ELSE t.to_wallet_id END),
		(SELECT count(*) FROM idempotency_records),
		(SELECT count(*) FROM transfers WHERE status='PENDING') +
		(SELECT count(*) FROM idempotency_records WHERE http_status IS NULL OR response_body IS NULL)`).
		Scan(&from, &to, &processed, &failed, &ledger, &matching, &keys, &incomplete)
	if err != nil {
		t.Fatal(err)
	}
	if from != 400 || to != 300 || processed != 1 || failed != 1 || ledger != 2 || matching != 2 || keys != 2 || incomplete != 0 {
		t.Fatalf("unexpected state: %d/%d processed=%d failed=%d ledger=%d matching=%d keys=%d incomplete=%d", from, to, processed, failed, ledger, matching, keys, incomplete)
	}
	response, err := client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("health route regressed")
	}
	server.Close() // All request logging finishes before inspecting the buffer.
	if strings.Contains(logs.String(), "private-key") || strings.Contains(logs.String(), "balance_minor") {
		t.Fatal("sensitive request data in logs")
	}
	ids := []string{id1, id2, id3, id4, id5}
	outcomes := []string{"processed", "replayed", "failed", "replayed", "conflict"}
	decoder := json.NewDecoder(&logs)
	seen := 0
	for {
		var entry struct {
			Message   string `json:"msg"`
			RequestID string `json:"request_id"`
			Outcome   string `json:"outcome"`
		}
		err := decoder.Decode(&entry)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if entry.Message != "transfer" {
			continue
		}
		if seen >= len(outcomes) || entry.RequestID != ids[seen] || entry.Outcome != outcomes[seen] {
			t.Fatalf("unexpected log: %+v", entry)
		}
		seen++
	}
	if seen != len(outcomes) {
		t.Fatalf("transfer logs=%d", seen)
	}
}
