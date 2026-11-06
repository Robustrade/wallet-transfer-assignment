package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func TestGetWallet(t *testing.T) {
	h := newServer(&fakeTransfers{}, nil)

	rec := do(t, h, http.MethodGet, "/wallets/wallet_1", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"id":"wallet_1","balance":900,"updatedAt":"2026-11-04T10:00:00Z"}`, rec.Body.String())

	rec = do(t, h, http.MethodGet, "/wallets/unknown", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "WALLET_NOT_FOUND", errorCode(t, rec))
}

func TestGetTransfer(t *testing.T) {
	tr := transfer(domain.StatusProcessed, "")
	h := newServer(&fakeTransfers{result: service.CreateTransferResult{Transfer: tr}}, nil)

	rec := do(t, h, http.MethodGet, "/transfers/"+tr.ID.String(), "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, tr.ID.String(), decode(t, rec)["id"])
	require.Equal(t, "PROCESSED", decode(t, rec)["status"])

	rec = do(t, h, http.MethodGet, "/transfers/00000000-0000-4000-8000-000000000000", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "TRANSFER_NOT_FOUND", errorCode(t, rec))

	rec = do(t, h, http.MethodGet, "/transfers/not-a-uuid", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "VALIDATION_ERROR", errorCode(t, rec))
}
