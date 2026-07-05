package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/handler"
)

func TestRouter_WrongMethod_Returns405(t *testing.T) {
	router := handler.NewRouter(handler.NewTransferHandler(&fakeTransferService{}))

	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestRouter_HealthCheck(t *testing.T) {
	router := handler.NewRouter(handler.NewTransferHandler(&fakeTransferService{}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}
