package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/Robustrade/wallet-transfer-assignment/handler"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/service"
	"github.com/google/uuid"
)

func TestCreateTransfer_Validation(t *testing.T) {
	t.Parallel()
	h := handler.New(nil)
	mux := http.NewServeMux()
	h.Routes(mux)

	cases := []struct {
		name string
		body string
	}{
		{"empty key", `{"idempotencyKey":"","fromWalletId":"a","toWalletId":"b","amount":1}`},
		{"same wallet", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"a","amount":1}`},
		{"zero amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":0}`},
		{"float amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1.5}`},
		{"missing amount", `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"invalid_request"`) {
				t.Fatalf("body=%s", rec.Body.String())
			}
		})
	}
}

func TestCreateTransfer_ServiceErrorMapping(t *testing.T) {
	t.Parallel()
	validBody := `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":10}`

	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"wallet not found", service.ErrWalletNotFound, http.StatusNotFound, "wallet_not_found"},
		{"idempotency mismatch", service.ErrIdempotencyMismatch, http.StatusConflict, "idempotency_key_mismatch"},
		{"balance overflow", model.ErrBalanceOverflow, http.StatusBadRequest, "invalid_request"},
		{"invariant broken", service.ErrInvariantBroken, http.StatusInternalServerError, "internal"},
		{"unexpected", errors.New("db unavailable"), http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := handler.New(&stubService{createTransferErr: tc.err})
			mux := http.NewServeMux()
			h.Routes(mux)

			req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(validBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
				t.Fatalf("body=%s, want error %q", rec.Body.String(), tc.code)
			}
		})
	}
}

func TestCreateTransfer_SuccessAndReplay(t *testing.T) {
	t.Parallel()

	view := dto.TransferView{
		TransferID:     uuid.New().String(),
		IdempotencyKey: "k1",
		FromWalletID:   "a",
		ToWalletID:     "b",
		Amount:         10,
		Status:         "PROCESSED",
	}
	h := handler.New(&stubService{
		createTransfer: dto.TransferOutcome{StatusCode: 201, Transfer: &view},
	})
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest(http.MethodPost, "/transfers",
		strings.NewReader(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":10}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	raw, _ := json.Marshal(view)
	h2 := handler.New(&stubService{
		createTransfer: dto.TransferOutcome{StatusCode: 201, ReplayBody: raw},
	})
	mux2 := http.NewServeMux()
	h2.Routes(mux2)
	req2 := httptest.NewRequest(http.MethodPost, "/transfers",
		strings.NewReader(`{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":10}`))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("replay status=%d", rec2.Code)
	}
}

func TestGetTransfer_Mapping(t *testing.T) {
	t.Parallel()
	id := uuid.New()

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		h := handler.New(nil)
		mux := http.NewServeMux()
		h.Routes(mux)
		req := httptest.NewRequest(http.MethodGet, "/transfers/not-a-uuid", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		h := handler.New(&stubService{getTransferErr: service.ErrTransferNotFound})
		mux := http.NewServeMux()
		h.Routes(mux)
		req := httptest.NewRequest(http.MethodGet, "/transfers/"+id.String(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "transfer_not_found") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		want := dto.TransferView{TransferID: id.String(), Status: "PROCESSED", Amount: 5}
		h := handler.New(&stubService{getTransferView: want})
		mux := http.NewServeMux()
		h.Routes(mux)
		req := httptest.NewRequest(http.MethodGet, "/transfers/"+id.String(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("internal", func(t *testing.T) {
		t.Parallel()
		h := handler.New(&stubService{getTransferErr: errors.New("boom")})
		mux := http.NewServeMux()
		h.Routes(mux)
		req := httptest.NewRequest(http.MethodGet, "/transfers/"+id.String(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rec.Code)
		}
	})
}
