package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// TestClassify is the lock-down test for the error → HTTP status
// mapping. Every sentinel error the codebase returns from the domain
// or repository layers should have a case here and produce the
// correct status code. Anything unmatched falls through to 500 with
// a generic message — exercised explicitly by the "other" case.
func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"invalid key", domain.ErrInvalidIdempotencyKey, http.StatusBadRequest},
		{"invalid amount", domain.ErrInvalidAmount, http.StatusBadRequest},
		{"same wallet", domain.ErrSameWallet, http.StatusBadRequest},
		{"conflict", domain.ErrIdempotencyConflict, http.StatusConflict},
		{"repo not found", repository.ErrNotFound, http.StatusNotFound},
		{"domain not found", domain.ErrWalletNotFound, http.StatusNotFound},
		{"wrapped not found", errWrap(repository.ErrNotFound), http.StatusNotFound},
		{"other", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, msg := classify(c.err)
			if status != c.status {
				t.Fatalf("status: got %d want %d", status, c.status)
			}
			if msg == "" {
				t.Fatal("empty message")
			}
		})
	}
}

// TestWriteError_Status verifies that writeError flows the status
// code from classify into the actual HTTP response, and that the
// response body is the documented {"error": "..."} shape.
func TestWriteError_Status(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/transfers", nil)
	writeError(w, r, domain.ErrInvalidAmount)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", w.Code)
	}
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Fatal("error field empty")
	}
}

// TestWriteError_500HidesInternals is a SECURITY regression test.
// AGENTS.md §S-7 says 5xx responses must never leak DB error text,
// schema names, constraint names, etc. The test feeds writeError a
// classic pq error string and verifies that none of those fragments
// appear in the response body the client would see.
func TestWriteError_500HidesInternals(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/transfers", nil)
	writeError(w, r, errors.New("pq: duplicate key value violates unique constraint \"idempotency_pkey\""))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", w.Code)
	}
	body := w.Body.String()
	if body == "" || (containsAny(body, "pq:", "constraint", "idempotency_pkey")) {
		t.Fatalf("500 body leaks internals: %s", body)
	}
}

// TestWriteJSON_NilBody verifies the no-body branch of writeJSON:
// a nil body produces only the status line and Content-Type header
// with an empty body. This is the path used for any future 204-style
// responses.
func TestWriteJSON_NilBody(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusNoContent, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type: %s", w.Header().Get("Content-Type"))
	}
}

// helpers ------------------------------------------------------------

type wrappedErr struct{ inner error }

func (w wrappedErr) Error() string { return "wrapped: " + w.inner.Error() }
func (w wrappedErr) Unwrap() error { return w.inner }

func errWrap(e error) error { return wrappedErr{inner: e} }

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
