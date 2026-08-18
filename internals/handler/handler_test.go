package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/service"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type fakeTransferCreator struct {
	result    *service.TransferResult
	err       error
	lastInput service.CreateTransferInput
	called    bool
}

func (f *fakeTransferCreator) CreateTransfer(ctx context.Context, in service.CreateTransferInput) (*service.TransferResult, error) {
	f.called = true
	f.lastInput = in
	return f.result, f.err
}

func doRequest(t *testing.T, fake *fakeTransferCreator, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := NewTransferHandler(fake)
	err := h.CreateTransfer(c)
	require.NoError(t, err, "echo handlers should write the response, not return transport errors")
	return rec
}

func TestCreateTransfer_InvalidJSON(t *testing.T) {
	fake := &fakeTransferCreator{}
	rec := doRequest(t, fake, `{"amount": "not a number"`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, fake.called)
}

func TestCreateTransfer_MissingIdempotencyKey(t *testing.T) {
	fake := &fakeTransferCreator{}
	body := `{"fromWalletId":"` + uuid.New().String() + `","toWalletId":"` + uuid.New().String() + `","amount":100}`
	rec := doRequest(t, fake, body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, fake.called)
}

func TestCreateTransfer_InvalidFromWalletID(t *testing.T) {
	fake := &fakeTransferCreator{}
	body := `{"idempotencyKey":"k1","fromWalletId":"not-a-uuid","toWalletId":"` + uuid.New().String() + `","amount":100}`
	rec := doRequest(t, fake, body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, fake.called)
}

func TestCreateTransfer_InvalidToWalletID(t *testing.T) {
	fake := &fakeTransferCreator{}
	body := `{"idempotencyKey":"k1","fromWalletId":"` + uuid.New().String() + `","toWalletId":"not-a-uuid","amount":100}`
	rec := doRequest(t, fake, body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, fake.called)
}

func TestCreateTransfer_NonPositiveAmount(t *testing.T) {
	fake := &fakeTransferCreator{}
	body := `{"idempotencyKey":"k1","fromWalletId":"` + uuid.New().String() + `","toWalletId":"` + uuid.New().String() + `","amount":0}`
	rec := doRequest(t, fake, body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, fake.called)
}

func TestCreateTransfer_Processed_Returns201(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	transferID := uuid.New()
	fake := &fakeTransferCreator{result: &service.TransferResult{
		TransferID:   transferID,
		State:        models.TransferProcessed,
		FromWalletID: from,
		ToWalletID:   to,
		Amount:       100,
	}}
	body := `{"idempotencyKey":"k1","fromWalletId":"` + from.String() + `","toWalletId":"` + to.String() + `","amount":100}`
	rec := doRequest(t, fake, body)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.True(t, fake.called)
	require.Equal(t, "k1", fake.lastInput.IdempotencyKey)
	require.Equal(t, from, fake.lastInput.FromWalletID)
	require.Equal(t, to, fake.lastInput.ToWalletID)

	var got service.TransferResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, transferID, got.TransferID)
}

func TestCreateTransfer_Failed_Returns422(t *testing.T) {
	fake := &fakeTransferCreator{result: &service.TransferResult{
		State:         models.TransferFailed,
		FailureReason: "insufficient balance",
	}}
	body := `{"idempotencyKey":"k1","fromWalletId":"` + uuid.New().String() + `","toWalletId":"` + uuid.New().String() + `","amount":100}`
	rec := doRequest(t, fake, body)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestCreateTransfer_ServiceErrors_MapToStatusCodes(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"idempotency conflict", service.ErrIdempotencyKeyConflict, http.StatusConflict},
		{"same wallet", models.ErrSameWallet, http.StatusBadRequest},
		{"non-positive amount", models.ErrNonPositiveAmount, http.StatusBadRequest},
		{"wallet not found", repository.ErrWalletNotFound, http.StatusNotFound},
		{"insufficient balance", models.ErrInsufficientBalance, http.StatusUnprocessableEntity},
		{"unknown error", errUnexpected, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTransferCreator{err: tc.err}
			body := `{"idempotencyKey":"k1","fromWalletId":"` + uuid.New().String() + `","toWalletId":"` + uuid.New().String() + `","amount":100}`
			rec := doRequest(t, fake, body)
			require.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

var errUnexpected = &customError{"something went wrong"}

type customError struct{ msg string }

func (e *customError) Error() string { return e.msg }
