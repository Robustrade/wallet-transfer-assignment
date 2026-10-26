package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/handler"
)

type fakeTransferer struct {
	calls  int
	result domain.TransferResult
	err    error
}

func (f *fakeTransferer) Transfer(_ context.Context, r domain.TransferRequest) (domain.TransferResult, error) {
	f.calls++
	if err := r.ValidateStructure(); err != nil {
		return domain.TransferResult{}, err
	}
	return f.result, f.err
}

func TestHTTPBoundaryValidation(t *testing.T) {
	const valid = `{"idempotencyKey":"key","fromWalletId":"A","toWalletId":"B","amount":100}`
	cases := []struct {
		name        string
		body        string
		contentType string
		want        int
		calls       int
	}{
		{"valid JSON", valid, "application/json; charset=utf-8", 201, 1},
		{"non-JSON content type", valid, "text/plain", 415, 0},
		{"unknown JSON field", `{"idempotencyKey":"key","fromWalletId":"A","toWalletId":"B","amount":100,"foo":1}`, "application/json", 400, 0},
		{"invalid request", `{"idempotencyKey":"key","fromWalletId":"A","toWalletId":"A","amount":100}`, "application/json", 400, 1},
		{"multiple JSON objects", valid + `{}`, "application/json", 400, 0},
		{"large JSON body", valid + strings.Repeat(" ", 10000), "application/json", 413, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransferer{result: domain.TransferResult{ID: "transfer-123", Status: domain.StatusProcessed}}
			r := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			handler.New(fake).ServeHTTP(w, r)
			if w.Code != tc.want || fake.calls != tc.calls {
				t.Fatalf("status=%d calls=%d; want status=%d calls=%d, body=%s", w.Code, fake.calls, tc.want, tc.calls, w.Body.String())
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatalf("response is not JSON: %s", w.Body.String())
			}
		})
	}
}

func TestHTTPBoundaryDomainErrors(t *testing.T) {
	const valid = `{"idempotencyKey":"key","fromWalletId":"A","toWalletId":"B","amount":100}`
	for _, tc := range []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{domain.ErrIdempotencyConflict, 409, "idempotency_conflict"},
		{domain.ErrWalletNotFound, 404, "wallet_not_found"},
		{domain.ErrRetryableTransaction, 503, "outcome_unknown"},
		{errors.New("unavailable database"), 500, "outcome_unknown"},
	} {
		f := &fakeTransferer{err: tc.err}
		r := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(valid))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.New(f).ServeHTTP(w, r)
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.wantStatus || body.Error != tc.wantCode {
			t.Errorf("error %v: status=%d body=%s; want %d %s", tc.err, w.Code, w.Body.String(), tc.wantStatus, tc.wantCode)
		}
	}
}
