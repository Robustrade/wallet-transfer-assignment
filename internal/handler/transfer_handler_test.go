package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"wallet-transfer/internal/config"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/observability/metrics"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/tests/mock"
)

// TestCreate_TooLongKey_RejectedBeforeDB drives the real service over a mock store
// so an over-long idempotency key is rejected as 400 and never reaches the database.
func TestCreate_TooLongKey_RejectedBeforeDB(t *testing.T) {
	storeCalled := false
	store := &mock.Store{EnqueueFn: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		storeCalled = true
		return domain.Transfer{}, false, nil
	}}
	svc := service.NewTransferService(store, zap.NewNop(), service.NopRecorder{})
	h := New(config.Config{}, zap.NewNop(), nil, svc, metrics.New()).Handler()

	body := `{"idempotencyKey":"` + strings.Repeat("k", domain.MaxIdempotencyKeyLength+1) +
		`","fromWalletId":"` + uuid.NewString() + `","toWalletId":"` + uuid.NewString() + `","amount":10}`
	rec := postTransfer(h, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if storeCalled {
		t.Fatal("store must not be called for an invalid request")
	}
}

// fakeService is a stand-in TransferService for handler unit tests (no DB).
type fakeService struct {
	create func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error)
	get    func(context.Context, uuid.UUID) (domain.Transfer, error)
}

func (f fakeService) CreateTransfer(ctx context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error) {
	return f.create(ctx, p)
}
func (f fakeService) GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return f.get(ctx, id)
}

func newTestServer(svc TransferService) http.Handler {
	return New(config.Config{}, zap.NewNop(), nil, svc, metrics.New()).Handler()
}

func sampleTransfer() domain.Transfer {
	return domain.Transfer{
		ID:             uuid.New(),
		IdempotencyKey: "k",
		FromWalletID:   uuid.New(),
		ToWalletID:     uuid.New(),
		Amount:         decimal.RequireFromString("10"),
		State:          domain.StatePending,
	}
}

func postTransfer(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func validBody() string {
	return `{"idempotencyKey":"abc","fromWalletId":"` + uuid.NewString() +
		`","toWalletId":"` + uuid.NewString() + `","amount":10}`
}

func TestCreate_New_Returns202WithLocation(t *testing.T) {
	tr := sampleTransfer()
	h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return tr, true, nil
	}})
	rec := postTransfer(h, validBody())

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/api/v1/transfers/"+tr.ID.String() {
		t.Fatalf("Location = %q", got)
	}
	if !strings.Contains(rec.Body.String(), tr.ID.String()) {
		t.Fatalf("body missing id: %s", rec.Body.String())
	}
}

func TestCreate_UnprefixedAssignmentPath(t *testing.T) {
	tr := sampleTransfer()
	h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return tr, true, nil
	}})
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(validBody()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /transfers = %d, want 202", rec.Code)
	}
}

func TestCreate_BodyTooLarge(t *testing.T) {
	h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return sampleTransfer(), true, nil
	}})
	body := `{"idempotencyKey":"` + strings.Repeat("a", (1<<20)+100) + `"}`
	if rec := postTransfer(h, body); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", rec.Code)
	}
}

func TestCreate_TrailingDataRejected(t *testing.T) {
	h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return sampleTransfer(), true, nil
	}})
	if rec := postTransfer(h, validBody()+"{}"); rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing data = %d, want 400", rec.Code)
	}
}

func TestCreate_Replay_Returns200(t *testing.T) {
	h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return sampleTransfer(), false, nil // created=false -> idempotent replay
	}})
	if rec := postTransfer(h, validBody()); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestCreate_ErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"same wallet", domain.ErrSameWallet, http.StatusBadRequest},
		{"empty key", domain.ErrEmptyIdempotencyKey, http.StatusBadRequest},
		{"too long key", domain.ErrIdempotencyKeyTooLong, http.StatusBadRequest},
		{"invalid amount", domain.ErrInvalidAmount, http.StatusBadRequest},
		{"unknown wallet", domain.ErrWalletNotFound, http.StatusUnprocessableEntity},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
				return domain.Transfer{}, false, tt.err
			}})
			rec := postTransfer(h, validBody())
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode == http.StatusInternalServerError &&
				strings.Contains(rec.Body.String(), "boom") {
				t.Fatalf("internal error leaked to client: %s", rec.Body.String())
			}
		})
	}
}

func TestCreate_BadRequests(t *testing.T) {
	h := newTestServer(fakeService{create: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return sampleTransfer(), true, nil
	}})
	tests := map[string]string{
		"malformed json": `{`,
		"unknown field":  `{"idempotencyKey":"a","fromWalletId":"` + uuid.NewString() + `","toWalletId":"` + uuid.NewString() + `","amount":10,"x":1}`,
		"bad from uuid":  `{"idempotencyKey":"a","fromWalletId":"nope","toWalletId":"` + uuid.NewString() + `","amount":10}`,
		"bad to uuid":    `{"idempotencyKey":"a","fromWalletId":"` + uuid.NewString() + `","toWalletId":"nope","amount":10}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if rec := postTransfer(h, body); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestGet_OKAndNotFound(t *testing.T) {
	tr := sampleTransfer()
	h := newTestServer(fakeService{get: func(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
		if id == tr.ID {
			return tr, nil
		}
		return domain.Transfer{}, domain.ErrTransferNotFound
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transfers/"+tr.ID.String(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/transfers/"+uuid.NewString(), nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGet_BadUUID(t *testing.T) {
	h := newTestServer(fakeService{get: func(context.Context, uuid.UUID) (domain.Transfer, error) {
		return domain.Transfer{}, nil
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transfers/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
