package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
	"github.com/synchone/wallet-transfer-assignment/internal/handler"
	"github.com/synchone/wallet-transfer-assignment/internal/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type noopRepo struct{}

func (noopRepo) BeginTx(context.Context) (pgx.Tx, error) { panic("not reached") }
func (noopRepo) FindTransferForUpdate(context.Context, pgx.Tx, string) (*domain.Transfer, error) {
	return nil, domain.ErrTransferNotFound
}
func (noopRepo) CreateTransfer(context.Context, pgx.Tx, domain.CreateTransferInput) (*domain.Transfer, error) {
	panic("not reached")
}
func (noopRepo) LockWallets(context.Context, pgx.Tx, ...string) (map[string]domain.Wallet, error) {
	panic("not reached")
}
func (noopRepo) UpdateWalletBalance(context.Context, pgx.Tx, string, int64) error {
	panic("not reached")
}
func (noopRepo) InsertLedgerEntry(context.Context, pgx.Tx, uuid.UUID, string, domain.LedgerEntryType, int64) (int64, error) {
	panic("not reached")
}
func (noopRepo) ListLedgerEntries(context.Context, pgx.Tx, uuid.UUID) ([]domain.LedgerEntry, error) {
	panic("not reached")
}
func (noopRepo) UpdateTransferStatus(context.Context, pgx.Tx, uuid.UUID, domain.TransferStatus, *string) error {
	panic("not reached")
}
func (noopRepo) StoreResponseSnapshot(context.Context, pgx.Tx, uuid.UUID, json.RawMessage) error {
	panic("not reached")
}

func TestCreateTransferMissingIdempotencyKey(t *testing.T) {
	router := handler.NewRouter(handler.Dependencies{
		TransferService: service.NewTransferService(noopRepo{}, slog.Default()),
		HealthPinger:    healthyPinger{},
		Logger:          slog.Default(),
	})

	body := bytes.NewBufferString(`{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	req := httptest.NewRequest(http.MethodPost, "/transfers", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHealthEndpoint(t *testing.T) {
	router := handler.NewRouter(handler.Dependencies{
		TransferService: service.NewTransferService(noopRepo{}, slog.Default()),
		HealthPinger:    healthyPinger{},
		Logger:          slog.Default(),
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
}

type healthyPinger struct{}

func (healthyPinger) Ping(_ *gin.Context) error { return nil }
