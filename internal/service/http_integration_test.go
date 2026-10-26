package service_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/handler"
	"wallettransfer/internal/repository"
	"wallettransfer/internal/service"
)

// These tests exercise the real HTTP boundary and the real PostgreSQL-backed
// service in-process. They reuse the isolated-schema fixture, migration, and
// reconciliation assertions from earlier service integration tests.
func TestPostgresHTTPTransfers(t *testing.T) {
	t.Run("successful POST persists matching balances and ledger", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		server := serveTransfers(t, db)

		req := domain.TransferRequest{IdempotencyKey: "http-success", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		response := postTransfer(t, server, `{"idempotencyKey":"http-success","fromWalletId":"A","toWalletId":"B","amount":100}`)
		assertHTTPTransfer(t, response, http.StatusCreated, domain.StatusProcessed, "")
		assertStoredTransfer(t, db, response.result.TransferID, req, domain.StatusProcessed, "")
		assertLedger(t, db, response.result.TransferID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertTransferKeyCount(t, db, req.IdempotencyKey, 1)
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})

	t.Run("duplicate POST replays identical HTTP status body and persisted outcome", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		server := serveTransfers(t, db)
		payload := `{"idempotencyKey":"http-repeat","fromWalletId":"A","toWalletId":"B","amount":100}`
		first := postTransfer(t, server, payload)
		second := postTransfer(t, server, payload)
		assertHTTPTransfer(t, first, http.StatusCreated, domain.StatusProcessed, "")
		assertHTTPTransfer(t, second, http.StatusCreated, domain.StatusProcessed, "")
		if string(first.body) != string(second.body) {
			t.Errorf("HTTP response replay differs: first=%s replay=%s", first.body, second.body)
		}
		req := domain.TransferRequest{IdempotencyKey: "http-repeat", FromWalletID: "A", ToWalletID: "B", Amount: 100}
		assertStoredTransfer(t, db, first.result.TransferID, req, domain.StatusProcessed, "")
		assertLedger(t, db, first.result.TransferID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferCount(t, db, 1)
		assertTransferKeyCount(t, db, req.IdempotencyKey, 1)
		assertTotalLedgerCount(t, db, 2)
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertReconciles(t, db)
	})

	t.Run("different payload with same key beats missing wallet", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		server := serveTransfers(t, db)
		original := postTransfer(t, server, `{"idempotencyKey":"http-conflict","fromWalletId":"A","toWalletId":"B","amount":100}`)
		assertHTTPTransfer(t, original, http.StatusCreated, domain.StatusProcessed, "")
		for _, conflicting := range []string{
			`{"idempotencyKey":"http-conflict","fromWalletId":"A","toWalletId":"B","amount":101}`,
			`{"idempotencyKey":"http-conflict","fromWalletId":"A","toWalletId":"missing-wallet","amount":100}`,
		} {
			response := postTransfer(t, server, conflicting)
			assertHTTPError(t, response, http.StatusConflict, "idempotency_conflict")
		}
		assertStoredTransfer(t, db, original.result.TransferID,
			domain.TransferRequest{IdempotencyKey: "http-conflict", FromWalletID: "A", ToWalletID: "B", Amount: 100}, domain.StatusProcessed, "")
		assertLedger(t, db, original.result.TransferID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertReconciles(t, db)
	})

	t.Run("insufficient funds is durably FAILED and returns same HTTP response", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 50, "B": 200, "reserve": 1000})
		server := serveTransfers(t, db)
		payload := `{"idempotencyKey":"http-insufficient","fromWalletId":"A","toWalletId":"B","amount":100}`
		first := postTransfer(t, server, payload)
		assertHTTPTransfer(t, first, http.StatusConflict, domain.StatusFailed, domain.ReasonInsufficientFunds)

		// A legitimate reserve->A transfer funds A, but replay must stay FAILED.
		fund := postTransfer(t, server, `{"idempotencyKey":"http-funding","fromWalletId":"reserve","toWalletId":"A","amount":200}`)
		assertHTTPTransfer(t, fund, http.StatusCreated, domain.StatusProcessed, "")
		replay := postTransfer(t, server, payload)
		assertHTTPTransfer(t, replay, http.StatusConflict, domain.StatusFailed, domain.ReasonInsufficientFunds)
		if string(first.body) != string(replay.body) {
			t.Errorf("FAILED response replay differs: first=%s replay=%s", first.body, replay.body)
		}

		assertStoredTransfer(t, db, first.result.TransferID,
			domain.TransferRequest{IdempotencyKey: "http-insufficient", FromWalletID: "A", ToWalletID: "B", Amount: 100}, domain.StatusFailed, domain.ReasonInsufficientFunds)
		assertStoredTransfer(t, db, fund.result.TransferID,
			domain.TransferRequest{IdempotencyKey: "http-funding", FromWalletID: "reserve", ToWalletID: "A", Amount: 200}, domain.StatusProcessed, "")
		assertLedger(t, db, first.result.TransferID, nil)
		assertLedger(t, db, fund.result.TransferID, []ledgerExpectation{
			{wallet: "reserve", kind: "DEBIT", amount: 200},
			{wallet: "A", kind: "CREDIT", amount: 200},
		})
		assertTransferCount(t, db, 2)
		assertTotalLedgerCount(t, db, 2)
		assertBalance(t, db, "A", 250)
		assertBalance(t, db, "B", 200)
		assertBalance(t, db, "reserve", 800)
		assertReconciles(t, db)
	})

	t.Run("invalid requests are rejected without any database writes", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		server := serveTransfers(t, db)
		cases := []struct {
			name        string
			payload     string
			contentType string
			wantStatus  int
			wantError   string
		}{
			{"empty key", `{"idempotencyKey":"","fromWalletId":"A","toWalletId":"B","amount":100}`, "application/json", 400, "invalid_request"},
			{"NUL wallet ID", `{"idempotencyKey":"bad-nul","fromWalletId":"A","toWalletId":"B\u0000","amount":100}`, "application/json", 400, "invalid_request"},
			{"negative amount", `{"idempotencyKey":"neg","fromWalletId":"A","toWalletId":"B","amount":-1}`, "application/json", 400, "invalid_request"},
			{"fractional amount", `{"idempotencyKey":"float","fromWalletId":"A","toWalletId":"B","amount":1.5}`, "application/json", 400, "invalid_json"},
			{"same wallet", `{"idempotencyKey":"same","fromWalletId":"A","toWalletId":"A","amount":1}`, "application/json", 400, "invalid_request"},
			{"missing source", `{"idempotencyKey":"missing-from","toWalletId":"B","amount":1}`, "application/json", 400, "invalid_request"},
			{"malformed JSON", `{"idempotencyKey":`, "application/json", 400, "invalid_json"},
			{"unknown field", `{"idempotencyKey":"u","fromWalletId":"A","toWalletId":"B","amount":1,"extra":true}`, "application/json", 400, "invalid_json"},
			{"multiple JSON objects", `{"idempotencyKey":"x","fromWalletId":"A","toWalletId":"B","amount":1}{}`, "application/json", 400, "invalid_json"},
			{"missing new-key destination", `{"idempotencyKey":"new-missing","fromWalletId":"A","toWalletId":"absent","amount":100}`, "application/json", 404, "wallet_not_found"},
			{"wrong content type", `{"idempotencyKey":"type","fromWalletId":"A","toWalletId":"B","amount":1}`, "text/plain", 415, "unsupported_media_type"},
			{"oversized body", `{"idempotencyKey":"large","fromWalletId":"A","toWalletId":"B","amount":1}` + strings.Repeat(" ", 9000), "application/json", 413, "request_too_large"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				response := postTransferAs(t, server, tc.payload, tc.contentType)
				assertHTTPError(t, response, tc.wantStatus, tc.wantError)
				assertTransferCount(t, db, 0)
				assertTotalLedgerCount(t, db, 0)
				assertBalance(t, db, "A", 500)
				assertBalance(t, db, "B", 200)
				assertReconciles(t, db)
			})
		}
		resp, err := server.Client().Get(server.URL + "/transfers")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
			t.Errorf("GET /transfers: status=%d Allow=%q; want 405 Allow: POST", resp.StatusCode, resp.Header.Get("Allow"))
		}
		assertTransferCount(t, db, 0)
		assertTotalLedgerCount(t, db, 0)
	})

	t.Run("response lost after HTTP commit replays after handler recreation", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 500, "B": 200})
		server := serveTransfers(t, db)
		payload := `{"idempotencyKey":"http-lost","fromWalletId":"A","toWalletId":"B","amount":100}`
		// Simulate a client that does not receive/process the first response.
		resp, err := server.Client().Post(server.URL+"/transfers", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		server.Close()

		var id, status string
		if err := db.QueryRow(`SELECT id::text, status FROM transfers WHERE idempotency_key='http-lost'`).Scan(&id, &status); err != nil {
			t.Fatal(err)
		}
		if status != domain.StatusProcessed {
			t.Fatalf("stored transfer status=%s", status)
		}
		freshServer := serveTransfers(t, db) // new handler, service and repository objects
		replay := postTransfer(t, freshServer, payload)
		assertHTTPTransfer(t, replay, http.StatusCreated, domain.StatusProcessed, "")
		if replay.result.TransferID != id {
			t.Fatalf("replayed transfer ID %s; committed ID %s", replay.result.TransferID, id)
		}
		assertStoredTransfer(t, db, id, domain.TransferRequest{
			IdempotencyKey: "http-lost", FromWalletID: "A", ToWalletID: "B", Amount: 100,
		}, domain.StatusProcessed, "")
		assertLedger(t, db, id, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 100},
			{wallet: "B", kind: "CREDIT", amount: 100},
		})
		assertBalance(t, db, "A", 400)
		assertBalance(t, db, "B", 300)
		assertTransferCount(t, db, 1)
		assertTotalLedgerCount(t, db, 2)
		assertReconciles(t, db)
	})
}

type httpTransferResult struct {
	TransferID    string `json:"transferId"`
	Status        string `json:"status"`
	FailureReason string `json:"failureReason"`
}

type httpReply struct {
	status int
	body   []byte
	result httpTransferResult
	code   string
}

func serveTransfers(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler.New(service.New(repository.NewPostgres(db))))
	t.Cleanup(server.Close)
	return server
}

func postTransfer(t *testing.T, server *httptest.Server, payload string) httpReply {
	t.Helper()
	return postTransferAs(t, server, payload, "application/json")
}

func postTransferAs(t *testing.T, server *httptest.Server, payload, contentType string) httpReply {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/transfers", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("POST /transfers: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("response Content-Type=%q, body=%s", response.Header.Get("Content-Type"), body)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("response Cache-Control=%q", response.Header.Get("Cache-Control"))
	}
	var transfer httpTransferResult
	var apiErr struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &transfer); err != nil {
		t.Fatalf("invalid JSON response: %v: %s", err, body)
	}
	if err := json.Unmarshal(body, &apiErr); err != nil {
		t.Fatalf("invalid JSON error response: %v: %s", err, body)
	}
	return httpReply{status: response.StatusCode, body: body, result: transfer, code: apiErr.Error}
}

func assertHTTPTransfer(t *testing.T, got httpReply, wantStatus int, wantTransferStatus, wantReason string) {
	t.Helper()
	if got.status != wantStatus || got.result.TransferID == "" || got.result.Status != wantTransferStatus ||
		got.result.FailureReason != wantReason || got.code != "" {
		t.Fatalf("HTTP transfer result=%+v; want status=%d transferStatus=%s reason=%s (body=%s)",
			got, wantStatus, wantTransferStatus, wantReason, got.body)
	}
}

func assertHTTPError(t *testing.T, got httpReply, wantStatus int, wantCode string) {
	t.Helper()
	if got.status != wantStatus || got.code != wantCode || got.result.TransferID != "" || got.result.Status != "" {
		t.Fatalf("HTTP error=%s status=%d; want status=%d error=%s", got.body, got.status, wantStatus, wantCode)
	}
}
