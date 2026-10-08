package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

type transferFunc func(context.Context, service.Request) (repository.Result, error)

func (f transferFunc) Transfer(ctx context.Context, r service.Request) (repository.Result, error) {
	return f(ctx, r)
}

const validRequest = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

func invoke(h http.Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body)))
	return w
}
func assertJSONError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("invalid error shape: %s", w.Body.String())
	}
}
func TestTransferTransportValidation(t *testing.T) {
	for _, body := range []string{"", `{`, `null`, `[]`, validRequest + ` {}`, `{"amount":1.5}`, `{"amount":9223372036854775808}`, `{"fromWalletId":"a","toWalletId":"b","amount":0}`, `{"fromWalletId":"a","toWalletId":"a","amount":1}`, `{"fromWalletId":" ","toWalletId":"b","amount":1}`, `{"fromWalletId":"a","toWalletId":"b","amount":1,"extra":true}`, strings.Repeat(" ", maxRequestBytes+1)} {
		t.Run(fmt.Sprintf("input_%d", len(body)), func(t *testing.T) {
			called := false
			h := New(transferFunc(func(context.Context, service.Request) (repository.Result, error) {
				called = true
				return repository.Result{}, nil
			}))
			w := invoke(h, body)
			if called {
				t.Fatal("invalid transport request reached service")
			}
			if w.Code != 400 || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("expected JSON 400: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestTransferErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid", service.ErrInvalidRequest, 400, "INVALID_REQUEST"},
		{"missing", service.ErrWalletNotFound, 404, "WALLET_NOT_FOUND"},
		{"conflict", service.ErrIdempotencyConflict, 409, "IDEMPOTENCY_CONFLICT"},
		{"database", errors.New("SQL secret password"), 500, "INTERNAL_ERROR"},
		{"incomplete", service.ErrIncompleteResult, 500, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(transferFunc(func(context.Context, service.Request) (repository.Result, error) {
				return repository.Result{}, fmt.Errorf("internal detail: %w", tc.err)
			}))
			w := invoke(h, validRequest)
			assertJSONError(t, w, tc.status, tc.code)
			if strings.Contains(w.Body.String(), "SQL") || strings.Contains(w.Body.String(), "internal detail") {
				t.Fatal("internal error exposed")
			}
		})
	}
}
func TestTransferResultsAndReplay(t *testing.T) {
	for _, result := range []repository.Result{
		{StatusCode: 201, Body: `{"transferId":"t1","state":"PROCESSED","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`},
		{StatusCode: 422, Body: `{"transferId":"t2","state":"FAILED","error":{"code":"INSUFFICIENT_BALANCE","message":"Source wallet has insufficient balance"}}`},
	} {
		t.Run(fmt.Sprint(result.StatusCode), func(t *testing.T) {
			calls := 0
			h := New(transferFunc(func(ctx context.Context, r service.Request) (repository.Result, error) {
				calls++
				if r.IdempotencyKey != "abc123" || r.FromWalletID != "wallet_1" || r.ToWalletID != "wallet_2" || r.Amount != 100 {
					t.Fatalf("wrong request: %+v", r)
				}
				return result, nil
			}))
			for i := 0; i < 2; i++ {
				w := invoke(h, validRequest)
				if w.Code != result.StatusCode || w.Body.String() != result.Body || w.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("changed persisted result: %d %s", w.Code, w.Body.String())
				}
			}
			if calls != 2 {
				t.Fatal("handler must delegate each retry to service")
			}
		})
	}
}
func TestTransferContextAndMethod(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "request")
	h := New(transferFunc(func(got context.Context, _ service.Request) (repository.Result, error) {
		if got != ctx {
			t.Fatal("request context not forwarded")
		}
		return repository.Result{StatusCode: 201, Body: `{}`}, nil
	}))
	r := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(validRequest)).WithContext(ctx)
	h.ServeHTTP(httptest.NewRecorder(), r)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/transfers", nil))
	assertJSONError(t, w, 405, "METHOD_NOT_ALLOWED")
	if w.Header().Get("Allow") != "POST" {
		t.Fatal("missing Allow")
	}
}

func TestInvalidUTF8RejectedBeforeService(t *testing.T) {
	body := strings.Replace(validRequest, "abc123", string([]byte{0xff}), 1)
	called := false
	h := New(transferFunc(func(context.Context, service.Request) (repository.Result, error) {
		called = true
		return repository.Result{StatusCode: 201, Body: `{}`}, nil
	}))
	w := invoke(h, body)
	if called {
		t.Fatal("invalid bytes were silently replaced before invoking service")
	}
	assertJSONError(t, w, 400, "INVALID_REQUEST")
}

func TestUnpairedUnicodeEscapesCannotAliasIdentifiers(t *testing.T) {
	for _, escaped := range []string{`\ud800`, `\udfff`, `\ud800\u0041`} {
		called := false
		h := New(transferFunc(func(context.Context, service.Request) (repository.Result, error) {
			called = true
			return repository.Result{StatusCode: 201, Body: `{}`}, nil
		}))
		w := invoke(h, strings.Replace(validRequest, "abc123", escaped, 1))
		if called || w.Code != 400 {
			t.Fatalf("unpaired escape accepted: %s", escaped)
		}
	}
	for _, escaped := range []string{`\ud83d\ude00`, `\\ud800`, `\ufffd`} {
		called := false
		h := New(transferFunc(func(context.Context, service.Request) (repository.Result, error) {
			called = true
			return repository.Result{StatusCode: 201, Body: `{}`}, nil
		}))
		w := invoke(h, strings.Replace(validRequest, "abc123", escaped, 1))
		if !called || w.Code != 201 {
			t.Fatalf("valid escape rejected: %s", escaped)
		}
	}
}
