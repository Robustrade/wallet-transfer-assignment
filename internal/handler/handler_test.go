package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/handler"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
)

type fakeTransfers struct {
	create func(service.CreateTransferInput) (*service.TransferResult, error)
	get    func(uuid.UUID) (*service.TransferDetails, error)
	gotIn  service.CreateTransferInput
}

func (f *fakeTransfers) CreateTransfer(_ context.Context, in service.CreateTransferInput) (*service.TransferResult, error) {
	f.gotIn = in
	return f.create(in)
}

func (f *fakeTransfers) GetTransfer(_ context.Context, id uuid.UUID) (*service.TransferDetails, error) {
	return f.get(id)
}

type fakeWallets struct {
	create func(string, int64) (*domain.Wallet, error)
	get    func(string) (*domain.Wallet, error)
}

func (f *fakeWallets) CreateWallet(_ context.Context, id string, bal int64) (*domain.Wallet, error) {
	return f.create(id, bal)
}

func (f *fakeWallets) GetWallet(_ context.Context, id string) (*domain.Wallet, error) {
	return f.get(id)
}

var ts = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

func transferFixture(status domain.TransferStatus, reason domain.FailureReason) *domain.Transfer {
	return &domain.Transfer{
		ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), IdempotencyKey: "abc123",
		FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100,
		Status: status, FailureReason: reason, CreatedAt: ts, UpdatedAt: ts,
	}
}

func do(t *testing.T, h http.Handler, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newHandler(tr *fakeTransfers, w *fakeWallets) http.Handler {
	return handler.New(tr, w, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

const validBody = `{"idempotencyKey":"abc123","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

func TestCreateTransfer_StatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		result     *service.TransferResult
		err        error
		wantStatus int
		wantCode   string
		wantReplay bool
	}{
		{"processed", &service.TransferResult{Transfer: transferFixture(domain.TransferProcessed, "")}, nil, http.StatusCreated, "", false},
		{"replayed", &service.TransferResult{Transfer: transferFixture(domain.TransferProcessed, ""), Replayed: true}, nil, http.StatusOK, "", true},
		{"insufficient funds", &service.TransferResult{Transfer: transferFixture(domain.TransferFailed, domain.FailureInsufficientFunds)}, nil, http.StatusUnprocessableEntity, "insufficient_funds", false},
		{"replayed failure", &service.TransferResult{Transfer: transferFixture(domain.TransferFailed, domain.FailureInsufficientFunds), Replayed: true}, nil, http.StatusUnprocessableEntity, "insufficient_funds", true},
		{"validation", nil, domain.ErrInvalidRequest, http.StatusBadRequest, "invalid_request", false},
		{"wallet not found", nil, domain.ErrWalletNotFound, http.StatusNotFound, "wallet_not_found", false},
		{"key reused", nil, domain.ErrIdempotencyKeyReused, http.StatusUnprocessableEntity, "idempotency_key_reused", false},
		{"timeout", nil, context.DeadlineExceeded, http.StatusServiceUnavailable, "unavailable", false},
		{"unexpected", nil, errors.New("pq: secret connection detail"), http.StatusInternalServerError, "internal_error", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandler(&fakeTransfers{create: func(service.CreateTransferInput) (*service.TransferResult, error) {
				return tc.result, tc.err
			}}, &fakeWallets{})

			rec := do(t, h, http.MethodPost, "/transfers", validBody)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantReplay, rec.Header().Get(handler.ReplayedHeader) == "true")
			assert.NotContains(t, rec.Body.String(), "secret connection detail", "internal errors must not leak")
			if tc.wantCode != "" {
				var body struct {
					Error struct{ Code string } `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tc.wantCode, body.Error.Code)
			}
		})
	}
}

func TestCreateTransfer_ResponseBodyAndInput(t *testing.T) {
	tr := &fakeTransfers{create: func(service.CreateTransferInput) (*service.TransferResult, error) {
		return &service.TransferResult{Transfer: transferFixture(domain.TransferProcessed, "")}, nil
	}}

	rec := do(t, newHandler(tr, &fakeWallets{}), http.MethodPost, "/transfers", validBody)

	assert.Equal(t, service.CreateTransferInput{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}, tr.gotIn)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "PROCESSED", body["status"])
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", body["id"])
	assert.EqualValues(t, 100, body["amount"])
	assert.NotContains(t, body, "error")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestCreateTransfer_IdempotencyKeyFromHeader(t *testing.T) {
	newTr := func() *fakeTransfers {
		return &fakeTransfers{create: func(service.CreateTransferInput) (*service.TransferResult, error) {
			return &service.TransferResult{Transfer: transferFixture(domain.TransferProcessed, "")}, nil
		}}
	}
	noKey := `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	t.Run("header supplies the key", func(t *testing.T) {
		tr := newTr()
		rec := do(t, newHandler(tr, &fakeWallets{}), http.MethodPost, "/transfers", noKey, handler.IdempotencyKeyHeader, "from-header")
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, "from-header", tr.gotIn.IdempotencyKey)
	})

	t.Run("matching header and body are accepted", func(t *testing.T) {
		rec := do(t, newHandler(newTr(), &fakeWallets{}), http.MethodPost, "/transfers", validBody, handler.IdempotencyKeyHeader, "abc123")
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("conflicting header and body are rejected", func(t *testing.T) {
		rec := do(t, newHandler(newTr(), &fakeWallets{}), http.MethodPost, "/transfers", validBody, handler.IdempotencyKeyHeader, "other")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestCreateTransfer_RejectsMalformedBodies(t *testing.T) {
	h := newHandler(&fakeTransfers{create: func(service.CreateTransferInput) (*service.TransferResult, error) {
		t.Fatal("service must not be called for a malformed request")
		return nil, nil
	}}, &fakeWallets{})

	for name, body := range map[string]string{
		"not json":         `nope`,
		"empty":            ``,
		"unknown field":    `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1,"extra":true}`,
		"fractional":       `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1.5}`,
		"string amount":    `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":"1"}`,
		"trailing content": validBody + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, do(t, h, http.MethodPost, "/transfers", body).Code)
		})
	}
}

// Go's JSON decoder silently turns invalid UTF-8 into U+FFFD, which would make
// distinct malformed keys or wallet ids collapse into one identity. The raw body
// must be rejected before decoding.
func TestRejectsInvalidUTF8InRequestBodies(t *testing.T) {
	h := newHandler(&fakeTransfers{create: func(service.CreateTransferInput) (*service.TransferResult, error) {
		t.Fatal("service must not be called for an invalid request")
		return nil, nil
	}}, &fakeWallets{create: func(string, int64) (*domain.Wallet, error) {
		t.Fatal("service must not be called for an invalid request")
		return nil, nil
	}})

	cases := map[string]struct{ path, body string }{
		"key":           {"/transfers", "{\"idempotencyKey\":\"k\xff\",\"fromWalletId\":\"a\",\"toWalletId\":\"b\",\"amount\":1}"},
		"other key":     {"/transfers", "{\"idempotencyKey\":\"k\xfe\",\"fromWalletId\":\"a\",\"toWalletId\":\"b\",\"amount\":1}"},
		"source":        {"/transfers", "{\"idempotencyKey\":\"k\",\"fromWalletId\":\"a\xc3\",\"toWalletId\":\"b\",\"amount\":1}"},
		"destination":   {"/transfers", "{\"idempotencyKey\":\"k\",\"fromWalletId\":\"a\",\"toWalletId\":\"\x80b\",\"amount\":1}"},
		"new wallet id": {"/wallets", "{\"id\":\"w\xff\",\"balance\":1}"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, do(t, h, http.MethodPost, tc.path, tc.body).Code)
		})
	}
}

func TestRejectsOversizedRequestBodies(t *testing.T) {
	h := newHandler(&fakeTransfers{}, &fakeWallets{})
	body := `{"idempotencyKey":"` + strings.Repeat("a", 2<<20) + `"}`

	rec := do(t, h, http.MethodPost, "/transfers", body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestGetTransfer(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tr := &fakeTransfers{get: func(got uuid.UUID) (*service.TransferDetails, error) {
		if got != id {
			return nil, domain.ErrTransferNotFound
		}
		return &service.TransferDetails{
			Transfer: transferFixture(domain.TransferProcessed, ""),
			Entries: []domain.LedgerEntry{
				{ID: 1, WalletID: "wallet_1", Type: domain.EntryDebit, Amount: 100},
				{ID: 2, WalletID: "wallet_2", Type: domain.EntryCredit, Amount: 100},
			},
		}, nil
	}}
	h := newHandler(tr, &fakeWallets{})

	t.Run("found, with ledger entries", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/transfers/"+id.String(), "")
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			LedgerEntries []struct{ Type string } `json:"ledgerEntries"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.LedgerEntries, 2)
		assert.Equal(t, "DEBIT", body.LedgerEntries[0].Type)
		assert.Equal(t, "CREDIT", body.LedgerEntries[1].Type)
	})
	t.Run("unknown id", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodGet, "/transfers/"+uuid.NewString(), "").Code)
	})
	t.Run("not a uuid", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, do(t, h, http.MethodGet, "/transfers/nope", "").Code)
	})
}

func TestWalletEndpoints(t *testing.T) {
	w := &fakeWallets{
		create: func(id string, bal int64) (*domain.Wallet, error) {
			if id == "dup" {
				return nil, domain.ErrWalletExists
			}
			return &domain.Wallet{ID: id, Balance: bal, CreatedAt: ts, UpdatedAt: ts}, nil
		},
		get: func(id string) (*domain.Wallet, error) {
			if id != "wallet_1" {
				return nil, domain.ErrWalletNotFound
			}
			return &domain.Wallet{ID: id, Balance: 250, CreatedAt: ts, UpdatedAt: ts}, nil
		},
	}
	h := newHandler(&fakeTransfers{}, w)

	assert.Equal(t, http.StatusCreated, do(t, h, http.MethodPost, "/wallets", `{"id":"wallet_1","balance":250}`).Code)
	assert.Equal(t, http.StatusConflict, do(t, h, http.MethodPost, "/wallets", `{"id":"dup","balance":1}`).Code)

	rec := do(t, h, http.MethodGet, "/wallets/wallet_1", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"wallet_1","balance":250,"createdAt":"2026-10-25T10:00:00Z","updatedAt":"2026-10-25T10:00:00Z"}`, rec.Body.String())
	assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodGet, "/wallets/ghost", "").Code)
}

func TestHealthz(t *testing.T) {
	rec := do(t, newHandler(&fakeTransfers{}, &fakeWallets{}), http.MethodGet, "/healthz", "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestPanicIsRecoveredAsInternalError(t *testing.T) {
	h := newHandler(&fakeTransfers{create: func(service.CreateTransferInput) (*service.TransferResult, error) {
		panic("boom")
	}}, &fakeWallets{})

	rec := do(t, h, http.MethodPost, "/transfers", validBody)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
