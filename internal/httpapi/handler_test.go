package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/service"
)

type mockRepository struct {
	mu               sync.Mutex
	wallets          map[string]int64
	requests         map[string]domain.TransferRequest
	results          map[string]domain.TransferResult
	transfers        []domain.Transfer
	getWalletErr     error
	listTransfersErr error
	nextID           int
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		wallets:  make(map[string]int64),
		requests: make(map[string]domain.TransferRequest),
		results:  make(map[string]domain.TransferResult),
	}
}

func (m *mockRepository) CreateWallet(_ context.Context, walletID string, balance int64) (domain.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.wallets[walletID]; exists {
		return domain.Wallet{}, domain.ErrWalletExists
	}
	m.wallets[walletID] = balance
	return domain.Wallet{ID: walletID, Balance: balance}, nil
}

func (m *mockRepository) GetWallet(_ context.Context, walletID string) (domain.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getWalletErr != nil {
		return domain.Wallet{}, m.getWalletErr
	}
	balance, exists := m.wallets[walletID]
	if !exists {
		return domain.Wallet{}, domain.NewWalletNotFoundError(walletID)
	}
	return domain.Wallet{ID: walletID, Balance: balance}, nil
}

func (m *mockRepository) ListTransfers(_ context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listTransfersErr != nil {
		return nil, m.listTransfersErr
	}
	if _, exists := m.wallets[walletID]; !exists {
		return nil, domain.NewWalletNotFoundError(walletID)
	}
	transfers := make([]domain.Transfer, 0)
	for _, transfer := range m.transfers {
		if transfer.FromWalletID == walletID || transfer.ToWalletID == walletID {
			transfers = append(transfers, transfer)
		}
	}
	sort.Slice(transfers, func(i, j int) bool {
		if transfers[i].CreatedAt == transfers[j].CreatedAt {
			return transfers[i].ID > transfers[j].ID
		}
		return transfers[i].CreatedAt > transfers[j].CreatedAt
	})
	if len(transfers) > limit {
		transfers = transfers[:limit]
	}
	return transfers, nil
}

func (m *mockRepository) ExecuteTransfer(_ context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if original, exists := m.requests[request.IdempotencyKey]; exists {
		if original != request {
			return domain.TransferResult{}, domain.ErrIdempotencyConflict
		}
		result := m.results[request.IdempotencyKey]
		return result, nil
	}
	if _, exists := m.wallets[request.FromWalletID]; !exists {
		return domain.TransferResult{}, domain.NewWalletNotFoundError(request.FromWalletID)
	}
	if _, exists := m.wallets[request.ToWalletID]; !exists {
		return domain.TransferResult{}, domain.NewWalletNotFoundError(request.ToWalletID)
	}
	m.nextID++
	transfer := domain.Transfer{
		ID:             fmt.Sprintf("transfer-%03d", m.nextID),
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
		CreatedAt:      time.Unix(int64(m.nextID), 0).UTC().Format(time.RFC3339Nano),
	}
	httpStatus := http.StatusCreated
	if m.wallets[request.FromWalletID] < request.Amount {
		transfer.Status = domain.TransferFailed
		transfer.FailureReason = "insufficient funds"
		httpStatus = http.StatusUnprocessableEntity
	} else {
		m.wallets[request.FromWalletID] -= request.Amount
		m.wallets[request.ToWalletID] += request.Amount
		transfer.Status = domain.TransferProcessed
		transfer.LedgerEntries = []domain.LedgerEntry{
			{ID: int64(2*m.nextID - 1), WalletID: request.FromWalletID, TransferID: transfer.ID, Type: "DEBIT", Amount: request.Amount},
			{ID: int64(2 * m.nextID), WalletID: request.ToWalletID, TransferID: transfer.ID, Type: "CREDIT", Amount: request.Amount},
		}
	}
	result := domain.TransferResult{Transfer: transfer, HTTPStatus: httpStatus}
	m.requests[request.IdempotencyKey] = request
	m.results[request.IdempotencyKey] = result
	m.transfers = append(m.transfers, transfer)
	return result, nil
}

type testServer struct {
	client *http.Client
	url    string
	repo   *mockRepository
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	repository := newMockRepository()
	server := httptest.NewServer(httpapi.New(service.New(repository)))
	t.Cleanup(server.Close)
	return &testServer{client: server.Client(), url: server.URL, repo: repository}
}

func (ts *testServer) postJSON(t *testing.T, path string, body any) (*http.Response, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	response, err := ts.client.Post(ts.url+path, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	t.Cleanup(func() { response.Body.Close() })
	var data bytes.Buffer
	if _, err := data.ReadFrom(response.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	return response, data.Bytes()
}

func createWallet(t *testing.T, ts *testServer, walletID string, initialBalance int64) {
	t.Helper()
	response, body := ts.postJSON(t, "/wallets", map[string]any{"walletId": walletID, "initialBalance": initialBalance})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create wallet %s: status %d, body %s", walletID, response.StatusCode, body)
	}
}

type transferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	TransferID     string `json:"transferId"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	FailureReason  string `json:"failureReason"`
	LedgerEntries  []struct {
		WalletID string `json:"walletId"`
		Type     string `json:"type"`
		Amount   int64  `json:"amount"`
	} `json:"ledgerEntries"`
}

func TestRootReturnsAPIIndex(t *testing.T) {
	ts := newTestServer(t)
	response, err := ts.client.Get(ts.url + "/")
	if err != nil {
		t.Fatalf("get API index: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var index struct {
		Service string `json:"service"`
	}
	if err := json.NewDecoder(response.Body).Decode(&index); err != nil || index.Service != "wallet-transfer-service" {
		t.Fatalf("unexpected API index: %+v, error: %v", index, err)
	}
}

func TestTransferReplayAndRequestConflict(t *testing.T) {
	ts := newTestServer(t)
	createWallet(t, ts, "wallet-a", 300)
	createWallet(t, ts, "wallet-b", 0)
	request := transferRequest{IdempotencyKey: "stable-key", FromWalletID: "wallet-a", ToWalletID: "wallet-b", Amount: 125}
	firstResponse, firstBody := ts.postJSON(t, "/transfers", request)
	if firstResponse.StatusCode != http.StatusCreated {
		t.Fatalf("first transfer status=%d body=%s", firstResponse.StatusCode, firstBody)
	}
	var first transferResponse
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatalf("decode transfer response: %v", err)
	}
	if len(first.LedgerEntries) != 2 || first.LedgerEntries[0].Type != "DEBIT" || first.LedgerEntries[1].Type != "CREDIT" {
		t.Fatalf("transfer response ledger entries are not balanced: %+v", first.LedgerEntries)
	}
	secondResponse, secondBody := ts.postJSON(t, "/transfers", request)
	if secondResponse.StatusCode != http.StatusCreated || !bytes.Equal(firstBody, secondBody) {
		t.Fatalf("replay did not return saved response: status=%d body=%s", secondResponse.StatusCode, secondBody)
	}
	assertBalance(t, ts, "wallet-a", 175)
	assertBalance(t, ts, "wallet-b", 125)

	request.Amount = 100
	conflict, body := ts.postJSON(t, "/transfers", request)
	if conflict.StatusCode != http.StatusConflict || !bytes.Contains(body, []byte("idempotency_key_conflict")) {
		t.Fatalf("changed request should conflict: status=%d body=%s", conflict.StatusCode, body)
	}
}

func TestInsufficientFundsIsDurableAndReplayed(t *testing.T) {
	ts := newTestServer(t)
	createWallet(t, ts, "wallet-a", 30)
	createWallet(t, ts, "wallet-b", 0)
	request := transferRequest{IdempotencyKey: "too-large", FromWalletID: "wallet-a", ToWalletID: "wallet-b", Amount: 50}
	first, firstBody := ts.postJSON(t, "/transfers", request)
	second, secondBody := ts.postJSON(t, "/transfers", request)
	if first.StatusCode != http.StatusUnprocessableEntity || second.StatusCode != first.StatusCode || !bytes.Equal(firstBody, secondBody) {
		t.Fatalf("failed transfer replay mismatch: first=%d %s second=%d %s", first.StatusCode, firstBody, second.StatusCode, secondBody)
	}
	assertBalance(t, ts, "wallet-a", 30)
	assertBalance(t, ts, "wallet-b", 0)
}

func TestConcurrentDebitsCannotOverspend(t *testing.T) {
	ts := newTestServer(t)
	createWallet(t, ts, "source", 100)
	createWallet(t, ts, "destination-a", 0)
	createWallet(t, ts, "destination-b", 0)
	requests := []transferRequest{
		{IdempotencyKey: "concurrent-a", FromWalletID: "source", ToWalletID: "destination-a", Amount: 80},
		{IdempotencyKey: "concurrent-b", FromWalletID: "source", ToWalletID: "destination-b", Amount: 80},
	}
	statuses := make([]int, len(requests))
	var group sync.WaitGroup
	for index, request := range requests {
		group.Add(1)
		go func(index int, request transferRequest) {
			defer group.Done()
			response, _ := ts.postJSON(t, "/transfers", request)
			statuses[index] = response.StatusCode
		}(index, request)
	}
	group.Wait()
	successes, failures := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusCreated:
			successes++
		case http.StatusUnprocessableEntity:
			failures++
		default:
			t.Fatalf("unexpected transfer response status %d", status)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("got %d successes and %d failures; want one each", successes, failures)
	}
	assertBalance(t, ts, "source", 20)
}

func TestWalletHistoryReturnsStableNewestFirstTransfers(t *testing.T) {
	ts := newTestServer(t)
	createWallet(t, ts, "wallet-a", 200)
	createWallet(t, ts, "wallet-b", 0)
	for index := 0; index < 3; index++ {
		request := transferRequest{IdempotencyKey: fmt.Sprintf("history-%d", index), FromWalletID: "wallet-a", ToWalletID: "wallet-b", Amount: 1}
		response, body := ts.postJSON(t, "/transfers", request)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("create history transfer: %d %s", response.StatusCode, body)
		}
	}
	response, err := ts.client.Get(ts.url + "/wallets/wallet-a/transfers?limit=2")
	if err != nil {
		t.Fatalf("get transfer history: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", response.StatusCode)
	}
	var result struct {
		Transfers []transferResponse `json:"transfers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(result.Transfers) != 2 || result.Transfers[0].IdempotencyKey != "history-2" || result.Transfers[1].IdempotencyKey != "history-1" {
		t.Fatalf("history not newest-first or limit ignored: %+v", result.Transfers)
	}
}

func TestInvalidAndUnknownWalletRequests(t *testing.T) {
	ts := newTestServer(t)
	response, body := ts.postJSON(t, "/transfers", transferRequest{IdempotencyKey: "unknown", FromWalletID: "missing", ToWalletID: "also-missing", Amount: 1})
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown wallets: status=%d", response.StatusCode)
	}
	if !bytes.Contains(body, []byte("missing")) {
		t.Fatalf("not-found response does not identify the wallet: %s", body)
	}
	response, _ = ts.postJSON(t, "/transfers", transferRequest{FromWalletID: "a", ToWalletID: "b", Amount: 0})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid transfer: status=%d", response.StatusCode)
	}
}

func TestWalletReadRepositoryErrorsReturnGenericInternalServerError(t *testing.T) {
	ts := newTestServer(t)
	ts.repo.getWalletErr = fmt.Errorf("postgres: connection refused")
	ts.repo.listTransfersErr = fmt.Errorf("postgres: connection refused")

	for _, path := range []string{"/wallets/wallet-a", "/wallets/wallet-a/transfers"} {
		response, err := ts.client.Get(ts.url + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		var body bytes.Buffer
		_, readErr := body.ReadFrom(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s response: %v", path, readErr)
		}
		if response.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s status=%d, want %d; body=%s", path, response.StatusCode, http.StatusInternalServerError, body.String())
		}
		if bytes.Contains(body.Bytes(), []byte("connection refused")) {
			t.Errorf("GET %s leaked repository error: %s", path, body.String())
		}
	}
}

func TestTransferHandlerRejectsTrailingJSON(t *testing.T) {
	ts := newTestServer(t)
	response, err := ts.client.Post(ts.url+"/transfers", "application/json", bytes.NewBufferString(`{"idempotencyKey":"x","fromWalletId":"a","toWalletId":"b","amount":1} {}`))
	if err != nil {
		t.Fatalf("post malformed transfer: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing JSON status=%d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func assertBalance(t *testing.T, ts *testServer, walletID string, want int64) {
	t.Helper()
	response, err := ts.client.Get(ts.url + "/wallets/" + walletID)
	if err != nil {
		t.Fatalf("get wallet %s: %v", walletID, err)
	}
	defer response.Body.Close()
	var wallet struct {
		Balance int64 `json:"balance"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&wallet) != nil || wallet.Balance != want {
		t.Fatalf("wallet %s response status=%d balance=%d, want %d", walletID, response.StatusCode, wallet.Balance, want)
	}
}

var _ service.Repository = (*mockRepository)(nil)
