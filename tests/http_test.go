package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/malem-mayeng/wallet-transfer-assignment/internal/domain"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/handler"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/repository"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/service"
)

func TestCreateTransferHTTP_Success(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	body := `{
		"idempotencyKey": "http-success-1",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	var response struct {
		TransferID int64  `json:"transferId"`
		Status     string `json:"status"`
		Amount     int64  `json:"amount"`
	}

	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.TransferID != 1 {
		t.Fatalf(
			"transfer ID = %d, want 1",
			response.TransferID,
		)
	}

	if response.Status != string(domain.TransferProcessed) {
		t.Fatalf(
			"status = %q, want %q",
			response.Status,
			domain.TransferProcessed,
		)
	}

	if response.Amount != 100 {
		t.Fatalf(
			"amount = %d, want 100",
			response.Amount,
		)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)
}

func TestCreateTransferHTTP_InvalidAmount(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	body := `{
		"idempotencyKey": "http-invalid-amount",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 0
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}

func TestCreateTransferHTTP_IdempotencyConflict(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	firstBody := `{
		"idempotencyKey": "http-conflict-1",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100
	}`

	firstReq := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(firstBody),
	)
	firstReq.Header.Set("Content-Type", "application/json")

	firstRecorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf(
			"first status = %d, want %d",
			firstRecorder.Code,
			http.StatusOK,
		)
	}

	secondBody := `{
		"idempotencyKey": "http-conflict-1",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 200
	}`

	secondReq := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(secondBody),
	)
	secondReq.Header.Set("Content-Type", "application/json")

	secondRecorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(secondRecorder, secondReq)

	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf(
			"second status = %d, want %d",
			secondRecorder.Code,
			http.StatusConflict,
		)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 1 {
		t.Fatalf(
			"transfers = %d, want 1",
			transferCount,
		)
	}
}

func TestCreateTransferHTTP_InvalidJSON(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(`{"amount":`),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}

func TestCreateTransferHTTP_WalletNotFound(t *testing.T) {
	ctx := context.Background()

	pool := newTestPool(t)
	resetDatabase(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	body := `{
		"idempotencyKey": "http-wallet-not-found",
		"fromWalletId": "wallet_missing",
		"toWalletId": "wallet_2",
		"amount": 100
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(body),
	)
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}
}

func TestCreateTransferHTTP_MissingIdempotencyKey(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	body := `{
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}

func TestCreateTransferHTTP_TrailingJSONIsRejected(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	body := `{
		"idempotencyKey": "http-trailing-data",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100
	} garbage`

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}

func TestCreateTransferHTTP_MissingWalletID(t *testing.T) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	body := `{
		"idempotencyKey": "missing-wallet-id",
		"fromWalletId": "",
		"toWalletId": "wallet_2",
		"amount": 100
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}

func TestCreateTransferHTTP_IdempotencyConflictBeforeValidation(
	t *testing.T,
) {
	pool := newTestPool(t)
	resetDatabase(t, pool)
	seedWallets(t, pool)

	repo := repository.NewPostgresRepository(pool)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	firstBody := `{
		"idempotencyKey": "validation-conflict-1",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 100
	}`

	firstReq := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(firstBody),
	)
	firstReq.Header.Set("Content-Type", "application/json")

	firstRecorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf(
			"first status = %d, want %d",
			firstRecorder.Code,
			http.StatusOK,
		)
	}

	secondBody := `{
		"idempotencyKey": "validation-conflict-1",
		"fromWalletId": "wallet_1",
		"toWalletId": "wallet_2",
		"amount": 0
	}`

	secondReq := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(secondBody),
	)
	secondReq.Header.Set("Content-Type", "application/json")

	secondRecorder := httptest.NewRecorder()

	transferHandler.CreateTransfer(secondRecorder, secondReq)

	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf(
			"second status = %d, want %d",
			secondRecorder.Code,
			http.StatusConflict,
		)
	}

	requireBalance(t, pool, "wallet_1", 900)
	requireBalance(t, pool, "wallet_2", 600)

	transferCount := countRows(
		t,
		pool,
		"SELECT COUNT(*) FROM transfers",
	)

	if transferCount != 1 {
		t.Fatalf(
			"transfers = %d, want 1",
			transferCount,
		)
	}
}
