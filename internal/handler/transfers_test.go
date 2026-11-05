package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/repository"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/service"
)

type transferExecutorFunc func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error)

func (f transferExecutorFunc) ExecuteIdempotent(ctx context.Context, key string, request service.TransferRequest, encode service.ResponseEncoder) (service.IdempotentResponse, error) {
	return f(ctx, key, request, encode)
}

func completedResponse(transfer domain.Transfer) service.IdempotentResponse {
	body, _ := encodeTransferResponse(transfer, nil)
	return service.IdempotentResponse{Transfer: transfer, StatusCode: http.StatusCreated, Body: body}
}

func validTransfer() domain.Transfer {
	return domain.Transfer{
		ID:           "transfer-1",
		FromWalletID: "wallet-a",
		ToWalletID:   "wallet-b",
		Amount:       100,
		Status:       domain.TransferProcessed,
	}
}

func TestCreateTransferUsesBoundedExecutionContext(t *testing.T) {
	var gotDeadline bool
	handler := NewTransferHandler(transferExecutorFunc(func(ctx context.Context, _ string, _ service.TransferRequest, encode service.ResponseEncoder) (service.IdempotentResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("transfer context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > transferExecutionTimeout {
			t.Fatalf("unexpected transfer deadline remaining: %v", remaining)
		}
		gotDeadline = true
		return completedResponse(validTransfer()), nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "key-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !gotDeadline {
		t.Fatal("service was not called")
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCreateTransferUsesIdempotencyHeader(t *testing.T) {
	called := false
	handler := NewTransferHandler(transferExecutorFunc(func(_ context.Context, key string, request service.TransferRequest, encode service.ResponseEncoder) (service.IdempotentResponse, error) {
		called = true
		if key != "key-123" {
			t.Fatalf("idempotency key = %q", key)
		}
		if request.FromWalletID != "wallet-a" || request.ToWalletID != "wallet-b" || request.Amount != 100 {
			t.Fatalf("request = %+v", request)
		}
		return completedResponse(validTransfer()), nil
	}))

	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "key-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called {
		t.Fatal("service was not called")
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body transferAPIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Transfer == nil || body.Transfer.ID != "transfer-1" || body.Transfer.Status != domain.TransferProcessed {
		t.Fatalf("response = %+v", body)
	}
}

func TestCreateTransferAcceptsIdempotencyKeyInBody(t *testing.T) {
	handler := NewTransferHandler(transferExecutorFunc(func(_ context.Context, key string, _ service.TransferRequest, encode service.ResponseEncoder) (service.IdempotentResponse, error) {
		if key != "body-key" {
			t.Fatalf("idempotency key = %q", key)
		}
		return completedResponse(validTransfer()), nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"idempotencyKey":"body-key","fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`,
	))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCreateTransferRejectsMismatchedIdempotencyKeys(t *testing.T) {
	called := false
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		called = true
		return service.IdempotentResponse{}, nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"idempotencyKey":"body-key","fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`,
	))
	request.Header.Set("Idempotency-Key", "header-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("status = %d, called = %v, body = %s", response.Code, called, response.Body.String())
	}
}

func TestCreateTransferMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid idempotency key", err: service.ErrInvalidIdempotencyKey, wantStatus: http.StatusBadRequest, wantCode: "invalid_idempotency_key"},
		{name: "idempotency conflict", err: service.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantCode: "idempotency_conflict"},
		{name: "invalid wallet", err: domain.ErrInvalidWalletID, wantStatus: http.StatusBadRequest, wantCode: "invalid_wallet_id"},
		{name: "same wallet", err: domain.ErrSameWallet, wantStatus: http.StatusBadRequest, wantCode: "same_wallet"},
		{name: "invalid amount", err: domain.ErrInvalidAmount, wantStatus: http.StatusBadRequest, wantCode: "invalid_amount"},
		{name: "wallet not found", err: repository.ErrWalletNotFound, wantStatus: http.StatusNotFound, wantCode: "wallet_not_found"},
		{name: "insufficient funds", err: domain.ErrInsufficientFunds, wantStatus: http.StatusUnprocessableEntity, wantCode: "insufficient_funds"},
		{name: "currency mismatch", err: domain.ErrCurrencyMismatch, wantStatus: http.StatusUnprocessableEntity, wantCode: "currency_mismatch"},
		{name: "balance overflow", err: domain.ErrBalanceOverflow, wantStatus: http.StatusUnprocessableEntity, wantCode: "balance_overflow"},
		{name: "internal error", err: errors.New("database password leaked"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transfer := domain.Transfer{}
			if errors.Is(test.err, domain.ErrInsufficientFunds) {
				transfer = domain.Transfer{
					ID:           "transfer-failed",
					FromWalletID: "wallet-a",
					ToWalletID:   "wallet-b",
					Amount:       200,
					Status:       domain.TransferFailed,
					FailureCode:  "INSUFFICIENT_FUNDS",
				}
			}
			handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
				return service.IdempotentResponse{Transfer: transfer}, test.err
			}))
			request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
				`{"fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`,
			))
			request.Header.Set("Idempotency-Key", "key-123")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			var body transferAPIResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error == nil || body.Error.Code != test.wantCode {
				t.Fatalf("error = %+v, want code %q", body.Error, test.wantCode)
			}
			if test.wantStatus == http.StatusInternalServerError && strings.Contains(response.Body.String(), "password") {
				t.Fatal("internal error details must not be exposed")
			}
			if errors.Is(test.err, domain.ErrInsufficientFunds) && (body.Transfer == nil || body.Transfer.Status != domain.TransferFailed) {
				t.Fatalf("failed transfer missing from response: %+v", body)
			}
		})
	}
}

func TestCreateTransferRejectsInvalidJSONAndUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"fromWalletId":`},
		{name: "unknown field", body: `{"fromWalletId":"a","toWalletId":"b","amount":1,"extra":true}`},
		{name: "multiple JSON values", body: `{"fromWalletId":"a","toWalletId":"b","amount":1} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
				called = true
				return service.IdempotentResponse{}, nil
			}))
			request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(test.body))
			request.Header.Set("Idempotency-Key", "key-123")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest || called {
				t.Fatalf("status = %d, called = %v, body = %s", response.Code, called, response.Body.String())
			}
		})
	}
}

func TestCreateTransferRejectsUnsupportedContentType(t *testing.T) {
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		t.Fatal("service must not be called")
		return service.IdempotentResponse{}, nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestTransferHandlerRejectsWrongMethod(t *testing.T) {
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		t.Fatal("service must not be called")
		return service.IdempotentResponse{}, nil
	}))
	request := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", response.Header().Get("Allow"))
	}
}

func TestTransferHandlerRejectsOversizedTrailingWhitespace(t *testing.T) {
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		t.Fatal("service must not be called")
		return service.IdempotentResponse{}, nil
	}))
	validJSON := `{"fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`
	body := validJSON + strings.Repeat(" ", maxTransferRequestBytes-len(validJSON)+1)
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "key-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
	var bodyResponse transferAPIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &bodyResponse); err != nil {
		t.Fatal(err)
	}
	if bodyResponse.Error == nil || bodyResponse.Error.Code != "request_too_large" {
		t.Fatalf("error = %+v, want request_too_large", bodyResponse.Error)
	}
}

func TestTransferHandlerRejectsOversizedBody(t *testing.T) {
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		t.Fatal("service must not be called")
		return service.IdempotentResponse{}, nil
	}))
	body := strings.Repeat(" ", maxTransferRequestBytes+1)
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "key-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestCreateTransferEmitsStructuredLogWithoutRawIdempotencyKey(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	const idempotencyKey = "private-idempotency-key"
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		return completedResponse(validTransfer()), nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":100}`,
	))
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	logOutput := output.String()
	for _, expected := range []string{
		"transfer request completed",
		"outcome=PROCESSED",
		"http_status=201",
		"transfer_id=transfer-1",
		"from_wallet_id=wallet-a",
		"to_wallet_id=wallet-b",
		"amount=100",
		"idempotency_key_hash=",
		"duration_ms=",
	} {
		if !strings.Contains(logOutput, expected) {
			t.Errorf("structured log does not contain %q: %s", expected, logOutput)
		}
	}
	if strings.Contains(logOutput, idempotencyKey) {
		t.Errorf("raw idempotency key was logged: %s", logOutput)
	}
}

func TestCreateTransferLogsFailedBusinessOutcome(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	failedTransfer := domain.Transfer{
		ID:           "transfer-failed",
		FromWalletID: "wallet-a",
		ToWalletID:   "wallet-b",
		Amount:       200,
		Status:       domain.TransferFailed,
		FailureCode:  "INSUFFICIENT_FUNDS",
	}
	handler := NewTransferHandler(transferExecutorFunc(func(context.Context, string, service.TransferRequest, service.ResponseEncoder) (service.IdempotentResponse, error) {
		bodyStatus, body, encodeErr := encodeTransferResponse(failedTransfer, domain.ErrInsufficientFunds)
		if encodeErr != nil { return service.IdempotentResponse{}, encodeErr }
		return service.IdempotentResponse{Transfer: failedTransfer, StatusCode: bodyStatus, Body: body}, domain.ErrInsufficientFunds
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"fromWalletId":"wallet-a","toWalletId":"wallet-b","amount":200}`,
	))
	request.Header.Set("Idempotency-Key", "failed-request-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	logOutput := output.String()
	for _, expected := range []string{
		"transfer request rejected",
		"outcome=FAILED",
		"http_status=422",
		"transfer_id=transfer-failed",
		"failure_code=INSUFFICIENT_FUNDS",
		"error_code=insufficient_funds",
	} {
		if !strings.Contains(logOutput, expected) {
			t.Errorf("structured log does not contain %q: %s", expected, logOutput)
		}
	}
}
