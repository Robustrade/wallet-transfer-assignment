package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/service"
)

type mockTransferService struct {
	initResult     int64
	transferResult *domain.Transfer
	transferErr    error
	getResult      *domain.Transfer
	getErr         error
}

func (m *mockTransferService) InitTransfer() int64 { return m.initResult }

func (m *mockTransferService) Transfer(_ context.Context, _ service.TransferRequest) (*domain.Transfer, error) {
	return m.transferResult, m.transferErr
}

func (m *mockTransferService) GetTransfer(_ context.Context, _ int64) (*domain.Transfer, error) {
	return m.getResult, m.getErr
}

func testRouter(svc *mockTransferService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewTransferHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := gin.New()
	r.POST("/transfers/init", h.InitTransfer)
	r.POST("/transfers", h.CreateTransfer)
	r.GET("/transfers/:id", h.GetTransfer)
	return r
}

const validWalletA = "018f0000-0000-7000-8000-000000000001"
const validWalletB = "018f0000-0000-7000-8000-000000000002"

func validCreateBody() string {
	return `{"idempotencyKey":"123","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":"10.00"}`
}

func doRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestInitTransferHandler(t *testing.T) {
	r := testRouter(&mockTransferService{initResult: 123456789})

	rec := doRequest(r, http.MethodPost, "/transfers/init", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp InitTransferResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TransactionID != "123456789" {
		t.Fatalf("expected transactionId as string \"123456789\", got %q", resp.TransactionID)
	}
}

func TestCreateTransfer_MalformedJSON(t *testing.T) {
	r := testRouter(&mockTransferService{})
	rec := doRequest(r, http.MethodPost, "/transfers", "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransfer_InvalidIdempotencyKey(t *testing.T) {
	r := testRouter(&mockTransferService{})
	body := `{"idempotencyKey":"not-a-number","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":"10.00"}`
	rec := doRequest(r, http.MethodPost, "/transfers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransfer_InvalidWalletID(t *testing.T) {
	r := testRouter(&mockTransferService{})
	body := `{"idempotencyKey":"123","fromWalletId":"not-a-uuid","toWalletId":"` + validWalletB + `","amount":"10.00"}`
	rec := doRequest(r, http.MethodPost, "/transfers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransfer_InvalidAmountShape(t *testing.T) {
	cases := []string{"abc", "-5", "1.234", "0.001"}
	for _, amount := range cases {
		body := `{"idempotencyKey":"123","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":"` + amount + `"}`
		r := testRouter(&mockTransferService{})
		rec := doRequest(r, http.MethodPost, "/transfers", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("amount %q: expected 400, got %d", amount, rec.Code)
		}
	}
}

func TestCreateTransfer_DomainErrorMapping(t *testing.T) {
	cases := []struct {
		code domain.ErrorCode
		want int
	}{
		{domain.ErrCodeSameWallet, http.StatusUnprocessableEntity},
		{domain.ErrCodeInvalidAmount, http.StatusUnprocessableEntity},
		{domain.ErrCodeWalletNotFound, http.StatusUnprocessableEntity},
		{domain.ErrCodeInsufficientBalance, http.StatusUnprocessableEntity},
		{domain.ErrCodeIdempotencyKeyReused, http.StatusConflict},
	}
	for _, tc := range cases {
		svc := &mockTransferService{transferErr: &domain.Error{Code: tc.code, Message: "boom"}}
		r := testRouter(svc)
		rec := doRequest(r, http.MethodPost, "/transfers", validCreateBody())
		if rec.Code != tc.want {
			t.Fatalf("code %s: expected %d, got %d", tc.code, tc.want, rec.Code)
		}
	}
}

func TestCreateTransfer_UnexpectedError(t *testing.T) {
	svc := &mockTransferService{transferErr: io.ErrUnexpectedEOF}
	r := testRouter(svc)
	rec := doRequest(r, http.MethodPost, "/transfers", validCreateBody())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestCreateTransfer_Processed(t *testing.T) {
	transfer := &domain.Transfer{ID: 1, Status: domain.StatusProcessed}
	svc := &mockTransferService{transferResult: transfer}
	r := testRouter(svc)
	rec := doRequest(r, http.MethodPost, "/transfers", validCreateBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCreateTransfer_FailedInsufficientBalance(t *testing.T) {
	reason := domain.ErrCodeInsufficientBalance
	transfer := &domain.Transfer{ID: 1, Status: domain.StatusFailed, FailureReason: &reason}
	svc := &mockTransferService{transferResult: transfer}
	r := testRouter(svc)
	rec := doRequest(r, http.MethodPost, "/transfers", validCreateBody())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestGetTransfer_InvalidID(t *testing.T) {
	r := testRouter(&mockTransferService{})
	rec := doRequest(r, http.MethodGet, "/transfers/not-a-number", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetTransfer_Found(t *testing.T) {
	transfer := &domain.Transfer{ID: 1, Status: domain.StatusFailed}
	reason := domain.ErrCodeInsufficientBalance
	transfer.FailureReason = &reason
	svc := &mockTransferService{getResult: transfer}
	r := testRouter(svc)

	rec := doRequest(r, http.MethodGet, "/transfers/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET must always return 200 for a found transfer regardless of its outcome, got %d", rec.Code)
	}
}

func TestGetTransfer_NotFound(t *testing.T) {
	svc := &mockTransferService{getResult: nil}
	r := testRouter(svc)

	rec := doRequest(r, http.MethodGet, "/transfers/1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetTransfer_UnexpectedError(t *testing.T) {
	svc := &mockTransferService{getErr: io.ErrUnexpectedEOF}
	r := testRouter(svc)

	rec := doRequest(r, http.MethodGet, "/transfers/1", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
