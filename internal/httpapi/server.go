// Package httpapi maps HTTP requests onto the wallet service.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

const maxBodyBytes = 1 << 20

// API is the service surface the HTTP handlers call.
type API interface {
	CreateWallet(ctx context.Context, id string, initialBalance int64) (service.WalletView, error)
	GetWallet(ctx context.Context, id string) (service.WalletView, error)
	CreateTransfer(ctx context.Context, req service.Request) (service.Result, error)
	GetTransfer(ctx context.Context, id string) (service.TransferDetails, error)
}

// New returns the HTTP handler for the wallet service.
func New(api API) http.Handler {
	h := &handler{api: api}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /wallets", h.createWallet)
	mux.HandleFunc("GET /wallets/{id}", h.getWallet)
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /transfers/{id}", h.getTransfer)
	return logRequests(mux)
}

type handler struct {
	api API
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []byte(`{"status":"ok"}`))
}

func (h *handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID             string `json:"id"`
		InitialBalance *int64 `json:"initialBalance"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", inputMessage(err))
		return
	}
	if body.InitialBalance == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "initialBalance is required")
		return
	}
	view, err := h.api.CreateWallet(r.Context(), body.ID, *body.InitialBalance)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	payload, err := json.Marshal(view)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, payload)
}

func (h *handler) getWallet(w http.ResponseWriter, r *http.Request) {
	view, err := h.api.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	payload, err := json.Marshal(view)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	req, err := readTransfer(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", inputMessage(err))
		return
	}
	result, err := h.api.CreateTransfer(r.Context(), req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, result.Status, result.Body)
}

func (h *handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	details, err := h.api.GetTransfer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, err)
		return
	}
	payload, err := json.Marshal(details)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func readTransfer(r *http.Request) (service.Request, error) {
	var body struct {
		IdempotencyKey string `json:"idempotencyKey"`
		FromWalletID   string `json:"fromWalletId"`
		ToWalletID     string `json:"toWalletId"`
		Amount         *int64 `json:"amount"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return service.Request{}, err
	}
	if body.Amount == nil {
		return service.Request{}, &domain.InputError{Message: "amount is required"}
	}
	return service.Request{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         *body.Amount,
	}, nil
}

func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return jsonInputError(err)
	}
	return nil
}

func jsonInputError(err error) error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return &domain.InputError{Message: "request body is too large"}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Type != nil && typeErr.Type.String() == "int64" {
		return &domain.InputError{Message: "amount fields must be integers"}
	}
	if strings.Contains(err.Error(), "unknown field") {
		return &domain.InputError{Message: "request body contains an unknown field"}
	}
	if errors.Is(err, io.EOF) {
		return &domain.InputError{Message: "request body is required"}
	}
	return &domain.InputError{Message: "request body must be valid JSON"}
}

func writeAPIError(w http.ResponseWriter, err error) {
	var input *domain.InputError
	switch {
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, "invalid_request", input.Message)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", "wallet already exists")
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used with a different request")
	case errors.Is(err, domain.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "idempotency_busy", "retry the same idempotency key")
	default:
		slog.Error("request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

func inputMessage(err error) string {
	var input *domain.InputError
	if errors.As(err, &input) {
		return input.Message
	}
	return "request body must be valid JSON"
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	payload, err := json.Marshal(errorBody{Error: code, Message: message})
	if err != nil {
		payload = []byte(`{"error":"internal_error","message":"internal error"}`)
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, payload)
}

func writeJSON(w http.ResponseWriter, status int, payload []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
