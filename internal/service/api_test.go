package service_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
)

func (f fixture) post(request domain.TransferRequest) *httptest.ResponseRecorder {
	f.t.Helper()
	response := httptest.NewRecorder()
	handler.NewRouter(f.svc, testMaxTransferAmount).ServeHTTP(response, f.httpRequest(request))
	return response
}

func (f fixture) httpRequest(request domain.TransferRequest) *http.Request {
	f.t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		f.t.Fatal(err)
	}
	return httptest.NewRequestWithContext(f.ctx, http.MethodPost, "/transfers", strings.NewReader(string(body)))
}

func TestAPI_CreateAndReplayUseProductionRouter(t *testing.T) {
	f := setup(t)
	if err := repository.VerifySchema(f.ctx, f.db); err != nil {
		t.Fatal(err)
	}
	first := f.post(transferRequest("k1", 100))
	second := f.post(transferRequest("k1", 100))
	if first.Code != 201 || second.Code != 201 || first.Body.String() != second.Body.String() || second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("create/replay responses: %d %s; %d %s", first.Code, first.Body, second.Code, second.Body)
	}
	var body struct {
		TransferID string `json:"transferId"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil || len(body.TransferID) != 36 {
		t.Fatal("missing database-generated transfer UUID")
	}
	f.wantBalance("wallet_1", 900)
	f.wantBalance("wallet_2", 600)
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestAPI_ConflictingPayloadReturns409(t *testing.T) {
	f := setup(t)
	f.post(transferRequest("k1", 100))
	response := f.post(transferRequest("k1", 200))
	if response.Code != 409 || !strings.Contains(response.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("conflict response: %d %s", response.Code, response.Body)
	}
	f.wantBalance("wallet_1", 900)
	f.wantCount("transfers", 1)
}

func TestAPI_InsufficientFundsReturnsPersisted422(t *testing.T) {
	f := setup(t)
	first := f.post(transferRequest("k1", 1001))
	f.exec("UPDATE wallets SET balance=10000 WHERE id='wallet_1'")
	second := f.post(transferRequest("k1", 1001))
	if first.Code != 422 || second.Code != 422 || first.Body.String() != second.Body.String() || second.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(first.Body.String(), `"failureReason":"INSUFFICIENT_FUNDS"`) {
		t.Fatal("business failure was not persisted/replayed")
	}
	f.wantBalance("wallet_1", 10000)
	f.wantCount("ledger_entries", 0)
}

func TestAPI_PendingResponseIsNotStoredAsTerminal(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	locker := f.db.WithContext(f.ctx).Begin()
	if locker.Error != nil {
		t.Fatal(locker.Error)
	}
	defer func() { _ = locker.Rollback().Error }()
	var transfer domain.Transfer
	if err := locker.Raw("SELECT * FROM transfers WHERE id=? FOR UPDATE", id).Scan(&transfer).Error; err != nil {
		t.Fatal(err)
	}
	response := f.post(transferRequest("k1", 100))
	if response.Code != 202 || response.Header().Get("Retry-After") != "1" || response.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("pending response: %d %s", response.Code, response.Body)
	}
	f.wantPending(id)
	f.wantCount("ledger_entries", 0)
	if err := locker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	response = f.post(transferRequest("k1", 100))
	if response.Code != 201 {
		t.Fatalf("pending retry: %d %s", response.Code, response.Body)
	}
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

type disconnectedClient struct{ header http.Header }

func (w *disconnectedClient) Header() http.Header     { return w.header }
func (*disconnectedClient) WriteHeader(int)           {}
func (*disconnectedClient) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

func TestAPI_LostResponseReplaysCommittedTransfer(t *testing.T) {
	f := setup(t)
	body, err := json.Marshal(transferRequest("k1", 100))
	if err != nil {
		t.Fatal(err)
	}
	writer := &disconnectedClient{header: make(http.Header)}
	handler.NewRouter(f.svc, testMaxTransferAmount).ServeHTTP(writer, httptest.NewRequestWithContext(f.ctx, http.MethodPost, "/transfers", strings.NewReader(string(body))))
	response := f.post(transferRequest("k1", 100))
	if response.Code != 201 || response.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("lost response retry: %d %s", response.Code, response.Body)
	}
	f.wantBalance("wallet_1", 900)
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
}

func TestAPI_InvalidInputLeavesNoReservation(t *testing.T) {
	f := setup(t)
	response := f.post(transferRequest("k1", 0))
	if response.Code != 400 {
		t.Fatalf("invalid request status = %d", response.Code)
	}
	f.wantCount("transfers", 0)
	f.wantCount("idempotency_records", 0)
}
