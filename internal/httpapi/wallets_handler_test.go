package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/httpapi"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

// testLogger discards output so tests stay quiet; logging behaviour itself
// isn't what these handler tests are checking.
func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeWalletServiceFull struct {
	createResult domain.Wallet
	createErr    error
	getResult    domain.Wallet
	getErr       error
	entriesPage  service.LedgerPage
	entriesErr   error
}

func (f *fakeWalletServiceFull) Create(context.Context, string, string) (domain.Wallet, error) {
	return f.createResult, f.createErr
}
func (f *fakeWalletServiceFull) Get(context.Context, string) (domain.Wallet, error) {
	return f.getResult, f.getErr
}
func (f *fakeWalletServiceFull) ListEntries(context.Context, string, int, *int64) (service.LedgerPage, error) {
	return f.entriesPage, f.entriesErr
}

type fakeTransferServiceUnused struct{}

func (fakeTransferServiceUnused) Create(context.Context, domain.TransferCommand) (service.TransferResult, error) {
	return service.TransferResult{}, nil
}
func (fakeTransferServiceUnused) Get(context.Context, uuid.UUID) (domain.Transfer, error) {
	return domain.Transfer{}, nil
}

func newWalletTestServer(wallets httpapi.WalletService) *httptest.Server {
	router := httpapi.NewRouter(fakeTransferServiceUnused{}, wallets, fakeHealthChecker{}, testLogger())
	return httptest.NewServer(router)
}

func TestWalletsHandler_Create(t *testing.T) {
	fake := &fakeWalletServiceFull{createResult: domain.Wallet{ID: "wallet_1", Kind: domain.WalletKindUser, Currency: "INR", CreatedAt: time.Now()}}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/wallets", `{"id":"wallet_1","currency":"INR"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := decodeBody(t, resp)
	require.Equal(t, "wallet_1", body["id"])
	require.Equal(t, "USER", body["kind"])
}

func TestWalletsHandler_Create_AlreadyExists(t *testing.T) {
	fake := &fakeWalletServiceFull{createErr: domain.ErrWalletAlreadyExists}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/wallets", `{"id":"wallet_1","currency":"INR"}`)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	body := decodeBody(t, resp)
	errBody := body["error"].(map[string]any)
	require.Equal(t, "WALLET_ALREADY_EXISTS", errBody["code"])
}

func TestWalletsHandler_Get(t *testing.T) {
	fake := &fakeWalletServiceFull{getResult: domain.Wallet{ID: "wallet_1", Kind: domain.WalletKindUser, Currency: "INR", BalanceMinor: 500, CreatedAt: time.Now()}}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/wallets/wallet_1")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decodeBody(t, resp)
	require.Equal(t, float64(500), body["balance"])
}

func TestWalletsHandler_Get_NotFound(t *testing.T) {
	fake := &fakeWalletServiceFull{getErr: domain.ErrWalletNotFound}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/wallets/missing")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestWalletsHandler_ListEntries(t *testing.T) {
	nextID := int64(41)
	fake := &fakeWalletServiceFull{entriesPage: service.LedgerPage{
		Entries: []domain.LedgerEntry{
			{ID: 42, TransferID: uuid.Must(uuid.NewV7()), WalletID: "wallet_1", EntryType: domain.LedgerEntryDebit, AmountMinor: 100, Currency: "INR", BalanceAfter: 900, CreatedAt: time.Now()},
		},
		NextBeforeID: &nextID,
	}}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/wallets/wallet_1/entries?limit=1")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decodeBody(t, resp)
	entries := body["entries"].([]any)
	require.Len(t, entries, 1)
	require.Equal(t, float64(41), body["nextBeforeId"])
}

func TestWalletsHandler_ListEntries_InvalidLimit(t *testing.T) {
	fake := &fakeWalletServiceFull{}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/wallets/wallet_1/entries?limit=not-a-number")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHealthz(t *testing.T) {
	fake := &fakeWalletServiceFull{}
	srv := newWalletTestServer(fake)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadyz_Unavailable(t *testing.T) {
	router := httpapi.NewRouter(fakeTransferServiceUnused{}, &fakeWalletServiceFull{}, fakeHealthChecker{err: context.DeadlineExceeded}, testLogger())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
