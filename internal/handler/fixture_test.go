package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/service"
)

const validBody = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

type transferStub struct {
	calls   int
	request domain.TransferRequest
	result  service.TransferResult
	err     error
}

func (s *transferStub) CreateTransfer(_ context.Context, request domain.TransferRequest) (service.TransferResult, error) {
	s.calls++
	s.request = request
	return s.result, s.err
}

type fixture struct {
	t       *testing.T
	service *transferStub
	handler http.Handler
}

func setup(t *testing.T) fixture {
	t.Helper()
	stub := &transferStub{result: service.TransferResult{
		StatusCode: http.StatusCreated,
		Body:       []byte(`{"transferId":"T1","status":"PROCESSED"}`),
	}}
	return fixture{t: t, service: stub, handler: handler.NewTransferHandler(stub, 1000)}
}

func (f fixture) post(body string) *httptest.ResponseRecorder {
	f.t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/transfers", strings.NewReader(body))
	f.handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		f.t.Fatalf("Content-Type = %q, want application/json", got)
	}
	return response
}

func (f fixture) wantResponse(response *httptest.ResponseRecorder, status int, body string) {
	f.t.Helper()
	if response.Code != status {
		f.t.Fatalf("status = %d, want %d", response.Code, status)
	}
	if response.Body.String() != body {
		f.t.Fatalf("body = %q, want %q", response.Body.String(), body)
	}
}

func (f fixture) wantError(response *httptest.ResponseRecorder, status int, code string) {
	f.t.Helper()
	if response.Code != status {
		f.t.Fatalf("status = %d, want %d", response.Code, status)
	}
	var body handler.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		f.t.Fatal(err)
	}
	if body.Error.Code != code {
		f.t.Fatalf("error code = %q, want %q", body.Error.Code, code)
	}
}
