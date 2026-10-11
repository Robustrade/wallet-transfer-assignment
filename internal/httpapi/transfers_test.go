package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/transfer"
)

type serviceFunc func(context.Context, transfer.CreateRequest) (transfer.Outcome, error)

func (f serviceFunc) Create(ctx context.Context, request transfer.CreateRequest) (transfer.Outcome, error) {
	return f(ctx, request)
}

func TestTransferHandlerRejectsMalformedAndUnknownFields(t *testing.T) {
	handler := NewTransferHandler(serviceFunc(func(context.Context, transfer.CreateRequest) (transfer.Outcome, error) {
		t.Fatal("service called for malformed request")
		return transfer.Outcome{}, nil
	}))

	for _, body := range []string{
		`{"idempotencyKey":`,
		`{"idempotencyKey":"key","fromWalletId":"a","toWalletId":"b","amount":1,"extra":true}`,
		`{"idempotencyKey":"key","fromWalletId":"a","toWalletId":"b","amount":1} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
			recorder := httptest.NewRecorder()
			handler.create(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestTransferHandlerMapsServiceOutcomeAndError(t *testing.T) {
	responseBody := `{"transferId":"id","state":"PROCESSED"}`
	handler := NewTransferHandler(serviceFunc(func(_ context.Context, request transfer.CreateRequest) (transfer.Outcome, error) {
		if request.Amount == 1 {
			return transfer.Outcome{StatusCode: http.StatusCreated, Body: []byte(responseBody)}, nil
		}
		return transfer.Outcome{}, &transfer.Error{StatusCode: http.StatusConflict, Code: "idempotency_key_reused", Message: "key conflict"}
	}))

	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"idempotencyKey":"key","fromWalletId":"a","toWalletId":"b","amount":1}`,
	))
	recorder := httptest.NewRecorder()
	handler.create(recorder, request)
	if recorder.Code != http.StatusCreated || strings.TrimSpace(recorder.Body.String()) != responseBody {
		t.Fatalf("success response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q, want application/json", recorder.Header().Get("Content-Type"))
	}

	request = httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"idempotencyKey":"key","fromWalletId":"a","toWalletId":"b","amount":2}`,
	))
	recorder = httptest.NewRecorder()
	handler.create(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"idempotency_key_reused"`) {
		t.Fatalf("conflict response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestTransferHandlerHidesUnexpectedServiceErrors(t *testing.T) {
	handler := NewTransferHandler(serviceFunc(func(context.Context, transfer.CreateRequest) (transfer.Outcome, error) {
		return transfer.Outcome{}, errors.New("database password leaked")
	}))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(
		`{"idempotencyKey":"key","fromWalletId":"a","toWalletId":"b","amount":1}`,
	))
	recorder := httptest.NewRecorder()
	handler.create(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("unexpected internal error response: %d %q", recorder.Code, recorder.Body.String())
	}
}
