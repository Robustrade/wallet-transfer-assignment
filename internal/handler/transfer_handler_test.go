package handler_test

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rohitkumar27/wallet-transfer-assignment/internal/handler"
	"github.com/rohitkumar27/wallet-transfer-assignment/internal/service"
)

func newHandler(t *testing.T) *handler.TransferHandler {
	t.Helper()
	// No real DB needed for these cases: validation fails before any
	// query is issued, so a closed *sql.DB is enough to construct the
	// service without a live connection.
	db, err := sql.Open("pgx", "postgres://invalid/invalid")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return handler.NewTransferHandler(service.NewTransferService(db, logger))
}

func TestCreateTransfer_InvalidJSON(t *testing.T) {
	h := newHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateTransfer_MissingFields(t *testing.T) {
	h := newHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"amount": 100}`))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateTransfer_SameWallet(t *testing.T) {
	h := newHandler(t)
	body := `{"idempotencyKey":"k1","fromWalletId":"w1","toWalletId":"w1","amount":100}`
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateTransfer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}
