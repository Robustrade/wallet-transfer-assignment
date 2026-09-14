package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"wallet-transfer-assigment/models"

	"go.uber.org/zap"
)

type serverTestLogger struct{}

func (serverTestLogger) Info(string, ...zap.Field)  {}
func (serverTestLogger) Error(string, ...zap.Field) {}
func (serverTestLogger) Warn(string, ...zap.Field)  {}
func (serverTestLogger) Debug(string, ...zap.Field) {}
func (serverTestLogger) Sync()                      {}

type failingResponseWriter struct {
	header http.Header
}

func (writer *failingResponseWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}

func (*failingResponseWriter) WriteHeader(int) {}

func (*failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("response writer failed")
}

type serverControllerMock struct {
	wallets         []models.Wallet
	wallet          *models.Wallet
	transactionID   string
	walletsErr      error
	walletErr       error
	transferErr     error
	transactions    []models.Transfers
	transactionsErr error
	statements      []models.Statement
	statementsErr   error
	requestedWallet string
	transferRequest models.TransferRequest
}

func (mock *serverControllerMock) GetWalletsDetails(context.Context) ([]models.Wallet, error) {
	return mock.wallets, mock.walletsErr
}

func (mock *serverControllerMock) GetWalletById(_ context.Context, walletID string) (*models.Wallet, error) {
	mock.requestedWallet = walletID
	return mock.wallet, mock.walletErr
}

func (mock *serverControllerMock) CreateWallet(context.Context, models.WalletRequest) (*models.Wallet, error) {
	return mock.wallet, mock.walletErr
}

func (mock *serverControllerMock) Transfer(_ context.Context, request models.TransferRequest) (*string, error) {
	mock.transferRequest = request
	if mock.transferErr != nil {
		return nil, mock.transferErr
	}
	return &mock.transactionID, nil
}

func (mock *serverControllerMock) GetStatements(context.Context) ([]models.Statement, error) {
	return nil, nil
}

func (mock *serverControllerMock) GetTransactions(context.Context) ([]models.Transfers, error) {
	return mock.transactions, mock.transactionsErr
}

func (mock *serverControllerMock) GetWalletStatementByWalletId(context.Context, string) ([]models.Statement, error) {
	return mock.statements, mock.statementsErr
}

func newTestServer(controllerMock *serverControllerMock) *Server {
	server := &Server{
		Conrollerer: controllerMock,
		Router:      http.NewServeMux(),
		logger:      serverTestLogger{},
	}
	server.Routes()
	return server
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) models.Response {
	t.Helper()
	var response models.Response
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func walletByIDRequest(method string) *http.Request {
	request := httptest.NewRequest(method, "/wallets/wallet_1", nil)
	request.SetPathValue("id", "wallet_1")
	return request
}

func TestServerHandlerRegistersWalletRoute(t *testing.T) {
	server := newTestServer(&serverControllerMock{wallets: []models.Wallet{{WalletID: "wallet_1", Balance: 100}}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/wallets", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type: got %q, want application/json", got)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "SUCCESS" || response.Status != "200" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerWalletByIDDelegatesRequestedID(t *testing.T) {
	controllerMock := &serverControllerMock{wallet: &models.Wallet{WalletID: "wallet_1", Balance: 100}}
	server := newTestServer(controllerMock)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusOK)
	}
	if controllerMock.requestedWallet != "wallet_1" {
		t.Fatalf("requested wallet: got %q, want wallet_1", controllerMock.requestedWallet)
	}
}

func TestServerTransferReturnsCreatedResponse(t *testing.T) {
	controllerMock := &serverControllerMock{transactionID: "transaction_1"}
	server := newTestServer(controllerMock)
	recorder := httptest.NewRecorder()
	body := `{"idempotencyKey":"request_1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusCreated)
	}
	if controllerMock.transferRequest.IdempotencyKey != "request_1" || controllerMock.transferRequest.Amount != 100 {
		t.Fatalf("transfer request: got %#v", controllerMock.transferRequest)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "Successfull" || response.Status != "201" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerTransferRejectsInvalidRequest(t *testing.T) {
	controllerMock := &serverControllerMock{}
	server := newTestServer(controllerMock)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"amount":0}`))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "invalid request" || response.Status != "400" {
		t.Fatalf("response: got %#v", response)
	}
	if controllerMock.transferRequest != (models.TransferRequest{}) {
		t.Fatal("controller should not be called for an invalid request")
	}
}

func TestServerRejectsUnsupportedMethod(t *testing.T) {
	server := newTestServer(&serverControllerMock{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/transfers", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "Invalid method type" || response.Status != "405" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerReturnsInternalErrorFromController(t *testing.T) {
	server := newTestServer(&serverControllerMock{walletsErr: errors.New("database unavailable")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/wallets", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "failed to fetch wallets" || response.Status != "500" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerTransactionsReturnsSuccessResponse(t *testing.T) {
	server := newTestServer(&serverControllerMock{
		transactions: []models.Transfers{{TransactionID: "transaction_1", Amount: 100}},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/transactions", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "SUCCESS" || response.Status != "200" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerTransactionsReturnsControllerError(t *testing.T) {
	server := newTestServer(&serverControllerMock{transactionsErr: errors.New("transaction lookup failed")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/transactions", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "failed to fetch wallets" || response.Status != "500" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerTransactionsRejectsUnsupportedMethod(t *testing.T) {
	server := newTestServer(&serverControllerMock{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/transactions", nil)

	server.getTransactions(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestNewServerInitializesInEmptyWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())

	server, err := NewServer(serverTestLogger{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if server == nil {
		t.Fatal("new server returned nil")
	}
}

func TestServerTransferRejectsMalformedJSON(t *testing.T) {
	server := newTestServer(&serverControllerMock{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader("{"))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "invalid request body" || response.Status != "400" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerTransferReturnsControllerError(t *testing.T) {
	server := newTestServer(&serverControllerMock{transferErr: errors.New("transfer failed")})
	recorder := httptest.NewRecorder()
	body := `{"idempotencyKey":"request_1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "transaction failed" || response.Status != "500" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerCreatesWallet(t *testing.T) {
	server := newTestServer(&serverControllerMock{wallet: &models.Wallet{WalletID: "wallet_1", Balance: 100}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"balance":100}`))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusCreated)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "successful" || response.Status != "201" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerCreateWalletValidationAndErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		mock   *serverControllerMock
		status int
	}{
		{name: "malformed body", body: "{", mock: &serverControllerMock{}, status: http.StatusBadRequest},
		{name: "invalid request", body: `{"balance":0}`, mock: &serverControllerMock{}, status: http.StatusBadRequest},
		{name: "controller error", body: `{"balance":100}`, mock: &serverControllerMock{walletErr: errors.New("create failed")}, status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(test.mock)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(test.body))
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestServerWalletStatementBranches(t *testing.T) {
	tests := []struct {
		name   string
		method string
		url    string
		status int
	}{
		{name: "method", method: http.MethodPost, url: "/wallets/wallet_1/statements", status: http.StatusMethodNotAllowed},
		{name: "missing id", method: http.MethodGet, url: "/wallets//statements", status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(&serverControllerMock{})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.url, nil)
			request.SetPathValue("id", "")
			server.walletStatementByWalletId(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestServerWalletStatementsSuccessAndControllerError(t *testing.T) {
	tests := []struct {
		name   string
		mock   *serverControllerMock
		status int
	}{
		{name: "success", mock: &serverControllerMock{statements: []models.Statement{{EntryID: "entry_1"}}}, status: http.StatusOK},
		{name: "controller error", mock: &serverControllerMock{statementsErr: errors.New("lookup failed")}, status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(test.mock)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/wallets/wallet_1/statements", nil)
			request.SetPathValue("id", "wallet_1")
			server.walletStatementByWalletId(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status: got %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestServerWalletListRejectsUnsupportedMethod(t *testing.T) {
	server := newTestServer(&serverControllerMock{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/wallets", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "Invalid method type" || response.Status != "405" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerWalletByIDRejectsUnsupportedMethod(t *testing.T) {
	server := newTestServer(&serverControllerMock{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/wallets/wallet_1", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "Invalid method type" || response.Status != "405" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerWalletByIDRequiresID(t *testing.T) {
	server := newTestServer(&serverControllerMock{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/wallets/", nil)

	server.getWalletById(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "wallet id required" || response.Status != "400" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestServerWalletByIDReturnsControllerError(t *testing.T) {
	server := newTestServer(&serverControllerMock{walletErr: errors.New("wallet lookup failed")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/wallets/wallet_1", nil)

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	response := decodeResponse(t, recorder)
	if response.Message != "failed to fetch wallets" || response.Status != "500" {
		t.Fatalf("response: got %#v", response)
	}
}

func TestNewServerReturnsDatabaseInitializationError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("wallet.db", 0o755); err != nil {
		t.Fatalf("create database blocker: %v", err)
	}

	server, err := NewServer(serverTestLogger{})
	if err == nil || server != nil {
		t.Fatalf("expected constructor error, got server=%v err=%v", server, err)
	}
}

func TestHandlersLogResponseWriterErrors(t *testing.T) {
	validTransfer := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"idempotencyKey":"request_1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`,
	))

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		request *http.Request
	}{
		{
			name:    "transfer method error",
			handler: newTestServer(&serverControllerMock{}).transfer,
			request: httptest.NewRequest(http.MethodGet, "/transfers", nil),
		},
		{
			name:    "transfer controller error",
			handler: newTestServer(&serverControllerMock{transferErr: errors.New("failed")}).transfer,
			request: validTransfer,
		},
		{
			name:    "transfer success",
			handler: newTestServer(&serverControllerMock{transactionID: "transaction_1"}).transfer,
			request: httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(validTransferBody)),
		},
		{
			name:    "wallet by ID method error",
			handler: newTestServer(&serverControllerMock{}).getWalletById,
			request: httptest.NewRequest(http.MethodPost, "/wallets/wallet_1", nil),
		},
		{
			name:    "wallet by ID missing ID",
			handler: newTestServer(&serverControllerMock{}).getWalletById,
			request: httptest.NewRequest(http.MethodGet, "/wallets/", nil),
		},
		{
			name:    "wallet by ID controller error",
			handler: newTestServer(&serverControllerMock{walletErr: errors.New("failed")}).getWalletById,
			request: walletByIDRequest(http.MethodGet),
		},
		{
			name:    "wallet by ID success",
			handler: newTestServer(&serverControllerMock{wallet: &models.Wallet{WalletID: "wallet_1"}}).getWalletById,
			request: walletByIDRequest(http.MethodGet),
		},
		{
			name:    "wallet list method error",
			handler: newTestServer(&serverControllerMock{}).getWallets,
			request: httptest.NewRequest(http.MethodPost, "/wallets", nil),
		},
		{
			name:    "wallet list controller error",
			handler: newTestServer(&serverControllerMock{walletsErr: errors.New("failed")}).getWallets,
			request: httptest.NewRequest(http.MethodGet, "/wallets", nil),
		},
		{
			name:    "wallet list success",
			handler: newTestServer(&serverControllerMock{}).getWallets,
			request: httptest.NewRequest(http.MethodGet, "/wallets", nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.handler(&failingResponseWriter{}, test.request)
		})
	}
}

const validTransferBody = `{"idempotencyKey":"request_1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`
