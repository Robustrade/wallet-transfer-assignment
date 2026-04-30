package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/middleware"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/response"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/handler"
)

// stubService is a no-op TransferServiceIface that returns a fixed response.
// Handler tests below exercise transport-level behavior, so the service
// either should not be reached or its response is irrelevant.
type stubService struct {
	called bool
	resp   response.TransferResp
	err    error
}

func (s *stubService) CreateTransfer(_ context.Context, _ request.CreateTransferReq) (response.TransferResp, error) {
	s.called = true
	return s.resp, s.err
}

const maxBody = 64 * 1024

// newTestRouter builds a router with the same middleware chain the production
// route registration uses, so size limits, recovery, and JSON error mapping
// are all exercised.
func newTestRouter(svc *stubService) *mux.Router {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewTransferHandler(svc)

	r := mux.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recovery(logger))
	r.Use(middleware.MaxBodyBytes(maxBody))
	r.NotFoundHandler = middleware.NotFoundHandler()
	r.MethodNotAllowedHandler = middleware.MethodNotAllowedHandler(nil)
	r.HandleFunc("/transfers", h.CreateTransfer).Methods(http.MethodPost)
	return r
}

// post issues a POST /transfers with the supplied raw body and returns the
// recorded response.
func post(t *testing.T, r *mux.Router, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var eb errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &eb))
	return eb
}

// TestHandler_BadJSON_Rejects400 — malformed JSON returns 400 BAD_JSON and
// the service is never invoked.
func TestHandler_BadJSON_Rejects400(t *testing.T) {
	svc := &stubService{}
	r := newTestRouter(svc)

	rec := post(t, r, []byte("{not valid json"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "BAD_JSON", decodeError(t, rec).Code)
	assert.False(t, svc.called, "service must not be reached on malformed JSON")
}

// TestHandler_UnknownField_Rejects400 — DisallowUnknownFields surfaces
// extra body fields as a 400 BAD_JSON, not a silent-strip + replay.
func TestHandler_UnknownField_Rejects400(t *testing.T) {
	svc := &stubService{}
	r := newTestRouter(svc)

	body := []byte(`{
		"idempotencyKey":"k1",
		"fromWalletId":"a",
		"toWalletId":"b",
		"amount":1,
		"extra":"surprise"
	}`)
	rec := post(t, r, body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "BAD_JSON", decodeError(t, rec).Code)
	assert.False(t, svc.called)
}

// TestHandler_PayloadTooLarge_Rejects413 — the MaxBodyBytes middleware caps
// the body at 64 KiB and the handler maps the resulting error to 413.
func TestHandler_PayloadTooLarge_Rejects413(t *testing.T) {
	svc := &stubService{}
	r := newTestRouter(svc)

	// 64 KiB + 1 byte of padding inside a JSON string.
	pad := strings.Repeat("x", maxBody+1)
	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":1,"_pad":"` + pad + `"}`)

	rec := post(t, r, body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "PAYLOAD_TOO_LARGE", decodeError(t, rec).Code)
	assert.False(t, svc.called)
}

// TestHandler_MethodNotAllowed_Rejects405 — verify the JSON 405 path.
func TestHandler_MethodNotAllowed_Rejects405(t *testing.T) {
	r := newTestRouter(&stubService{})

	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "METHOD_NOT_ALLOWED", decodeError(t, rec).Code)
}

// TestHandler_NotFound_Returns404 — verify the JSON 404 path.
func TestHandler_NotFound_Returns404(t *testing.T) {
	r := newTestRouter(&stubService{})

	req := httptest.NewRequest(http.MethodGet, "/no-such-route", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "NOT_FOUND", decodeError(t, rec).Code)
}

// TestHandler_FailedTransfer_Maps422 — the service returns a FAILED
// outcome (no error); the handler maps to 422 Unprocessable.
func TestHandler_FailedTransfer_Maps422(t *testing.T) {
	reason := "INSUFFICIENT_FUNDS"
	svc := &stubService{
		resp: response.TransferResp{
			Status:        "FAILED",
			FailureReason: &reason,
			Replayed:      false,
		},
	}
	r := newTestRouter(svc)

	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":1}`)
	rec := post(t, r, body)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.True(t, svc.called)
}

// TestHandler_Replay_Maps200 — replay flag steers a successful response to
// 200 OK regardless of the underlying status.
func TestHandler_Replay_Maps200(t *testing.T) {
	svc := &stubService{
		resp: response.TransferResp{
			Status:   "PROCESSED",
			Replayed: true,
		},
	}
	r := newTestRouter(svc)

	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":1}`)
	rec := post(t, r, body)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandler_FreshSuccess_Maps201 — the default success path.
func TestHandler_FreshSuccess_Maps201(t *testing.T) {
	svc := &stubService{
		resp: response.TransferResp{
			Status:   "PROCESSED",
			Replayed: false,
		},
	}
	r := newTestRouter(svc)

	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":1}`)
	rec := post(t, r, body)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

// TestHandler_GenericError_Maps500 — a plain error from the service (not an
// *apperr.AppError) is mapped to a generic 500 INTERNAL_ERROR; the wrapped
// message is NOT exposed to the client.
func TestHandler_GenericError_Maps500(t *testing.T) {
	svc := &stubService{err: errors.New("boom")}
	r := newTestRouter(svc)

	body := []byte(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":1}`)
	rec := post(t, r, body)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	eb := decodeError(t, rec)
	assert.Equal(t, "INTERNAL_ERROR", eb.Code)
	assert.NotContains(t, eb.Message, "boom",
		"wrapped error string must not leak to the client")
}

// TestHandler_AppError_PreservesStatusAndCode — when the service returns a
// typed *apperr.AppError, the handler must honor that error's HTTPStatus and
// emit its stable Code, not a generic 500.
func TestHandler_AppError_PreservesStatusAndCode(t *testing.T) {
	cases := []struct {
		name     string
		err      *apperr.AppError
		want     int
		wantCode string
	}{
		{"wallet not found", apperr.ErrWalletNotFound, http.StatusNotFound, "WALLET_NOT_FOUND"},
		{"insufficient funds", apperr.ErrInsufficientFunds, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS"},
		{"idempotency conflict", apperr.ErrIdempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT"},
		{"same wallet", apperr.ErrSameWallet, http.StatusBadRequest, "SAME_WALLET"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubService{err: tc.err}
			r := newTestRouter(svc)

			body := []byte(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":1}`)
			rec := post(t, r, body)

			assert.Equal(t, tc.want, rec.Code)
			assert.Equal(t, tc.wantCode, decodeError(t, rec).Code)
		})
	}
}
