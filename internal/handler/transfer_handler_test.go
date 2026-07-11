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
	initErr        error
	transferResult *domain.Transfer
	transferErr    error
	getResult      *domain.Transfer
	getErr         error
}

func (m *mockTransferService) InitTransfer() (int64, error) { return m.initResult, m.initErr }

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
	return `{"idempotencyKey":"123","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":10.00}`
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

func TestInitTransferHandler_IDGenError(t *testing.T) {
	r := testRouter(&mockTransferService{initErr: domain.ErrTransferInitFailed})

	rec := doRequest(r, http.MethodPost, "/transfers/init", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != string(domain.ErrCodeTransferInitFailed) {
		t.Fatalf("expected error code %s, got %+v", domain.ErrCodeTransferInitFailed, resp)
	}
	if resp["message"] != domain.ErrTransferInitFailed.Message {
		t.Fatalf("expected client-facing message %q, got %+v", domain.ErrTransferInitFailed.Message, resp)
	}
}

func TestInitTransferHandler_UnexpectedIDGenError(t *testing.T) {
	// A non-domain error (e.g. an infra failure the service forgot to
	// wrap) must still 500 with a generic message, never leak internals.
	r := testRouter(&mockTransferService{initErr: io.ErrUnexpectedEOF})

	rec := doRequest(r, http.MethodPost, "/transfers/init", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
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
	body := `{"idempotencyKey":"not-a-number","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":10.00}`
	rec := doRequest(r, http.MethodPost, "/transfers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != string(domain.ErrCodeInvalidIdempotencyKey) {
		t.Fatalf("expected error code %s, got %+v", domain.ErrCodeInvalidIdempotencyKey, resp)
	}
}

func TestCreateTransfer_InvalidWalletID(t *testing.T) {
	r := testRouter(&mockTransferService{})
	body := `{"idempotencyKey":"123","fromWalletId":"not-a-uuid","toWalletId":"` + validWalletB + `","amount":10.00}`
	rec := doRequest(r, http.MethodPost, "/transfers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != string(domain.ErrCodeInvalidWalletID) {
		t.Fatalf("expected error code %s, got %+v", domain.ErrCodeInvalidWalletID, resp)
	}
}

func TestCreateTransfer_MalformedAmountJSON(t *testing.T) {
	// "abc" isn't valid JSON syntax as a bare token, so it 400s at the
	// bind step, before validateAmount (and its typed error) ever runs.
	body := `{"idempotencyKey":"123","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":abc}`
	r := testRouter(&mockTransferService{})
	rec := doRequest(r, http.MethodPost, "/transfers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTransfer_InvalidAmountShape(t *testing.T) {
	// Each of these is syntactically a valid bare JSON number, so they
	// reach validateAmount's shape regex and fail there, not at bind time.
	cases := []string{"-5", "1.234", "0.001"}
	for _, amount := range cases {
		body := `{"idempotencyKey":"123","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":` + amount + `}`
		r := testRouter(&mockTransferService{})
		rec := doRequest(r, http.MethodPost, "/transfers", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("amount %q: expected 400, got %d", amount, rec.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("amount %q: decode response: %v", amount, err)
		}
		if resp["error"] != string(domain.ErrCodeInvalidAmountFormat) {
			t.Fatalf("amount %q: expected error code %s, got %+v", amount, domain.ErrCodeInvalidAmountFormat, resp)
		}
	}
}

func TestCreateTransfer_AmountOverflow(t *testing.T) {
	// Far more digits than an int64 can hold once shifted into minor
	// units -- distinct from a merely negative or too-many-decimals
	// amount (TestCreateTransfer_InvalidAmountShape), which are shape
	// violations caught by the regex before this ever parses. This
	// amount passes the regex (all digits, 2 decimal places) but
	// overflows on conversion -- both cases share the same typed error.
	body := `{"idempotencyKey":"123","fromWalletId":"` + validWalletA + `","toWalletId":"` + validWalletB + `","amount":999999999999999999999.00}`
	r := testRouter(&mockTransferService{})
	rec := doRequest(r, http.MethodPost, "/transfers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != string(domain.ErrCodeInvalidAmountFormat) {
		t.Fatalf("expected error code %s, got %+v", domain.ErrCodeInvalidAmountFormat, resp)
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
		// The service itself never returns these codes (they're caught in
		// the handler before the service is ever called) -- included here
		// to pin domainErrorHTTPStatus's mapping directly, independent of
		// which path reaches it.
		{domain.ErrCodeInvalidAmountFormat, http.StatusBadRequest},
		{domain.ErrCodeInvalidWalletID, http.StatusBadRequest},
		{domain.ErrCodeInvalidIdempotencyKey, http.StatusBadRequest},
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
