package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
	"wallet-service/internal/service"
)

type transferFunc func(context.Context, domain.Request) (repository.Result, error)

func (f transferFunc) Transfer(ctx context.Context, req domain.Request) (repository.Result, error) {
	return f(ctx, req)
}

const validRequest = `{"idempotencyKey":"key","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

func TestRejectInvalidRequestsBeforeService(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
		code                    string
	}{
		{"missing content type", "", validRequest, 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"wrong content type", "text/plain", validRequest, 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"too large", "application/json", validRequest + strings.Repeat(" ", 4097-len(validRequest)), 413, "REQUEST_TOO_LARGE"},
		{"malformed", "application/json", `{`, 400, "INVALID_REQUEST"},
		{"non-object", "application/json", `[]`, 400, "INVALID_REQUEST"},
		{"unknown field", "application/json", strings.Replace(validRequest, `"amount"`, `"extra":1,"amount"`, 1), 400, "INVALID_REQUEST"},
		{"duplicate field", "application/json", strings.Replace(validRequest, `"amount"`, `"amount":1,"amount"`, 1), 400, "INVALID_REQUEST"},
		{"missing field", "application/json", strings.Replace(validRequest, `,"amount":100`, ``, 1), 400, "INVALID_REQUEST"},
		{"null", "application/json", strings.Replace(validRequest, `"key"`, `null`, 1), 400, "INVALID_REQUEST"},
		{"wrong type", "application/json", strings.Replace(validRequest, `100`, `"100"`, 1), 400, "INVALID_REQUEST"},
		{"fraction", "application/json", strings.Replace(validRequest, `100`, `100.5`, 1), 400, "INVALID_REQUEST"},
		{"overflow", "application/json", strings.Replace(validRequest, `100`, `9223372036854775808`, 1), 400, "INVALID_REQUEST"},
		{"zero", "application/json", strings.Replace(validRequest, `100`, `0`, 1), 400, "INVALID_REQUEST"},
		{"trailing JSON", "application/json", validRequest + `{}`, 400, "INVALID_REQUEST"},
		{"invalid utf8", "application/json", strings.Replace(validRequest, `key`, "\xff", 1), 400, "INVALID_REQUEST"},
		{"self transfer", "application/json", strings.Replace(validRequest, `wallet_2`, `wallet_1`, 1), 400, "SELF_TRANSFER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewRouter(transferFunc(func(context.Context, domain.Request) (repository.Result, error) {
				t.Fatal("service called for invalid input")
				return repository.Result{}, nil
			}))
			r := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || strings.TrimSpace(w.Body.String()) != fmt.Sprintf(`{"error":{"code":%q}}`, tc.code) {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing response headers")
			}
		})
	}
}

func TestResponsesAndContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result repository.Result
		err    error
		status int
		code   string
	}{
		{"exact saved success", repository.Result{HTTPStatus: 201, Body: "{ \"transferId\": \"t1\", \"status\": \"PROCESSED\" }\n"}, nil, 201, ""},
		{"exact saved failure", repository.Result{HTTPStatus: 422, Body: `{"error":{"code":"INSUFFICIENT_FUNDS"}}`}, nil, 422, ""},
		{"invalid", repository.Result{}, domain.ErrInvalidRequest, 400, "INVALID_REQUEST"},
		{"self", repository.Result{}, domain.ErrSelfTransfer, 400, "SELF_TRANSFER"},
		{"conflict", repository.Result{}, fmt.Errorf("wrapped: %w", domain.ErrIdempotencyConflict), 409, "IDEMPOTENCY_CONFLICT"},
		{"unavailable", repository.Result{}, service.ErrUnavailable, 503, "TEMPORARILY_UNAVAILABLE"},
		{"unknown commit", repository.Result{}, service.ErrCommitUnknown, 503, "TEMPORARILY_UNAVAILABLE"},
		{"internal", repository.Result{}, errors.New("private database details"), 500, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			called := false
			h := NewRouter(transferFunc(func(ctx context.Context, req domain.Request) (repository.Result, error) {
				called = true
				if ctx.Err() != context.Canceled {
					t.Fatal("request cancellation not propagated")
				}
				if req.Amount != math.MaxInt64 || req.IdempotencyKey != "key" {
					t.Fatalf("decoded request: %+v", req)
				}
				return tc.result, tc.err
			}))
			body := strings.Replace(validRequest, `100`, `9223372036854775807`, 1)
			body += strings.Repeat(" ", 4096-len(body))
			r := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if !called || w.Code != tc.status {
				t.Fatalf("called=%t status=%d", called, w.Code)
			}
			if tc.err == nil {
				if w.Body.String() != tc.result.Body {
					t.Fatalf("response changed: %q", w.Body.String())
				}
			} else if strings.TrimSpace(w.Body.String()) != fmt.Sprintf(`{"error":{"code":%q}}`, tc.code) {
				t.Fatalf("unexpected error body: %s", w.Body.String())
			}
		})
	}
}
