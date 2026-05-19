package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/handler"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/service"
)

type stubAPI struct {
	tr        domain.Transfer
	created   bool
	createErr error
	getErr    error
	called    bool
	gotInput  service.CreateTransferInput
}

func (s *stubAPI) CreateTransfer(_ context.Context, in service.CreateTransferInput) (domain.Transfer, bool, error) {
	s.called = true
	s.gotInput = in
	return s.tr, s.created, s.createErr
}

func (s *stubAPI) GetTransfer(_ context.Context, _ string) (domain.Transfer, error) {
	if s.getErr != nil {
		return domain.Transfer{}, s.getErr
	}
	return s.tr, nil
}

func sampleTransfer() domain.Transfer {
	return domain.Transfer{
		ID: "id-1", IdempotencyKey: "k1",
		FromWalletID: "w1", ToWalletID: "w2",
		Amount:    decimal.RequireFromString("100.00"),
		Status:    domain.TransferProcessed,
		CreatedAt: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
	}
}

func do(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

const validBody = `{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2","amount":100}`

func TestCreate_New_201(t *testing.T) {
	s := &stubAPI{tr: sampleTransfer(), created: true}
	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", validBody)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, `"amount":"`) || !strings.Contains(body, `"amount":`) {
		t.Fatalf("amount must be a JSON number, got: %s", body)
	}
	if !strings.Contains(body, `"status":"PROCESSED"`) {
		t.Fatalf("missing status in body: %s", body)
	}
	if !s.gotInput.Amount.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("service got amount %s, want 100", s.gotInput.Amount)
	}
}

func TestCreate_DuplicateKey_200(t *testing.T) {
	s := &stubAPI{tr: sampleTransfer(), created: false}
	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", validBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for duplicate key", rr.Code)
	}
}

func TestCreate_FailedTransfer_IsNotAnError(t *testing.T) {
	tr := sampleTransfer()
	tr.Status = domain.TransferFailed
	tr.FailureReason = "insufficient funds"
	s := &stubAPI{tr: tr, created: true}

	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", validBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (FAILED is a created resource)", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"status":"FAILED"`) ||
		!strings.Contains(body, `"failureReason":"insufficient funds"`) {
		t.Fatalf("body missing FAILED/reason: %s", body)
	}
}

func TestCreate_ValidationError_400(t *testing.T) {
	s := &stubAPI{createErr: domain.ErrSameWallet}
	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", validBody)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestCreate_WalletNotFound_404(t *testing.T) {
	s := &stubAPI{createErr: domain.ErrWalletNotFound}
	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", validBody)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestCreate_MalformedJSON_400_ServiceNotCalled(t *testing.T) {
	s := &stubAPI{tr: sampleTransfer(), created: true}
	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", `{"amount":`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if s.called {
		t.Fatal("service must not be called on malformed JSON")
	}
}

func TestCreate_MissingAmount_400(t *testing.T) {
	s := &stubAPI{tr: sampleTransfer(), created: true}
	body := `{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w2"}`
	rr := do(handler.NewRouter(s), http.MethodPost, "/transfers", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing amount", rr.Code)
	}
	if s.called {
		t.Fatal("service must not be called when amount is unparseable")
	}
}

func TestGet_Found_200(t *testing.T) {
	s := &stubAPI{tr: sampleTransfer()}
	rr := do(handler.NewRouter(s), http.MethodGet, "/transfers/id-1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"id":"id-1"`) {
		t.Fatalf("body missing id: %s", rr.Body.String())
	}
}

func TestGet_NotFound_404(t *testing.T) {
	s := &stubAPI{getErr: domain.ErrTransferNotFound}
	rr := do(handler.NewRouter(s), http.MethodGet, "/transfers/missing", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}
