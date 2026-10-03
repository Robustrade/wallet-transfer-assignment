package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/domain"
	"github.com/pravinkanna/wallet-transfer-assignment/internal/service"
)

// maxBodyBytes is the largest request body accepted (spec §2).
const maxBodyBytes = 64 << 10

// requestTimeout bounds each request, including any wait for a wallet lock
// (design §9).
const requestTimeout = 5 * time.Second

// Errors for spec §6 steps 1–2, which only the handler can check.
var (
	errRequestTooLarge = errors.New("body must be at most 64 KiB")
	errInvalidJSON     = errors.New("body must be a single JSON object")
	errUnknownField    = errors.New("unknown field")
)

// errorCodes maps each client error to its status and code from spec §4.
var errorCodes = []struct {
	err    error
	status int
	code   string
}{
	{errRequestTooLarge, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE"},
	{errInvalidJSON, http.StatusBadRequest, "INVALID_JSON"},
	{errUnknownField, http.StatusBadRequest, "UNKNOWN_FIELD"},
	{domain.ErrInvalidField, http.StatusBadRequest, "INVALID_FIELD"},
	{domain.ErrInvalidAmount, http.StatusBadRequest, "INVALID_AMOUNT"},
	{domain.ErrSameWallet, http.StatusBadRequest, "SAME_WALLET"},
	{domain.ErrWalletNotFound, http.StatusBadRequest, "WALLET_NOT_FOUND"},
	{domain.ErrIdempotencyKeyReused, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED"},
}

// knownFields are the request fields from spec §2.
var knownFields = map[string]bool{
	"idempotencyKey": true,
	"fromWalletId":   true,
	"toWalletId":     true,
	"amount":         true,
}

// New returns the HTTP handler for the API (spec §1).
func New(svc *service.TransferService, logger *slog.Logger) http.Handler {
	h := &transferHandler{svc: svc, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	return mux
}

type transferHandler struct {
	svc    *service.TransferService
	logger *slog.Logger
}

// transferResponse is the transfer body from spec §3.
type transferResponse struct {
	TransferID string               `json:"transferId"`
	State      domain.TransferState `json:"state"`
}

// errorResponse is the error body from spec §4.
type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *transferHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	req, transfer, err := h.run(ctx, http.MaxBytesReader(w, r.Body, maxBodyBytes))
	status, body := response(transfer, err)
	h.logRequest(ctx, req.IdempotencyKey, transfer, status, err)
	writeJSON(w, status, body)
}

// run decodes the request and, if it is valid, runs the transfer.
func (h *transferHandler) run(ctx context.Context, body io.Reader) (domain.TransferRequest, domain.Transfer, error) {
	req, err := decodeRequest(body)
	if err != nil {
		return domain.TransferRequest{}, domain.Transfer{}, err
	}
	transfer, err := h.svc.Transfer(ctx, req)
	return req, transfer, err
}

// response returns the status and body for a transfer (spec §3) or an error
// (spec §4).
func response(transfer domain.Transfer, err error) (int, any) {
	if err != nil {
		return errorBody(err)
	}
	body := transferResponse{TransferID: transfer.ID, State: transfer.State}
	if transfer.State == domain.StateFailed {
		return http.StatusUnprocessableEntity, body
	}
	return http.StatusCreated, body
}

// logRequest writes the request's one log line (design §10). Validation
// failures before the service have no idempotencyKey; responses without a
// transfer have no transferId or state.
func (h *transferHandler) logRequest(ctx context.Context, key string, transfer domain.Transfer, status int, err error) {
	var attrs []slog.Attr
	if key != "" {
		attrs = append(attrs, slog.String("idempotencyKey", key))
	}
	if transfer.ID != "" {
		attrs = append(attrs, slog.String("transferId", transfer.ID), slog.String("state", string(transfer.State)))
	}
	attrs = append(attrs, slog.Int("status", status))
	level := slog.LevelInfo
	if status == http.StatusInternalServerError {
		level = slog.LevelError
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	h.logger.LogAttrs(ctx, level, "transfer request", attrs...)
}

// decodeRequest turns a request body into a validated domain request
// (design §5).
func decodeRequest(body io.Reader) (domain.TransferRequest, error) {
	// Read the whole body first, so the size check comes before the JSON
	// check (spec §6 step 1).
	data, err := io.ReadAll(body)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return domain.TransferRequest{}, errRequestTooLarge
	}
	if err != nil {
		return domain.TransferRequest{}, errInvalidJSON
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil || fields == nil {
		return domain.TransferRequest{}, errInvalidJSON
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return domain.TransferRequest{}, errInvalidJSON
	}
	for name := range fields {
		if !knownFields[name] {
			return domain.TransferRequest{}, fmt.Errorf("%w: %s", errUnknownField, name)
		}
	}

	key, err := stringField(fields, "idempotencyKey")
	if err != nil {
		return domain.TransferRequest{}, err
	}
	from, err := stringField(fields, "fromWalletId")
	if err != nil {
		return domain.TransferRequest{}, err
	}
	to, err := stringField(fields, "toWalletId")
	if err != nil {
		return domain.TransferRequest{}, err
	}
	amount := string(fields["amount"])
	if amount == "null" {
		amount = ""
	}
	return domain.NewTransferRequest(key, from, to, amount)
}

// stringField unquotes a JSON string field. A missing field or null gives "".
func stringField(fields map[string]json.RawMessage, name string) (string, error) {
	var value string
	if raw, ok := fields[name]; ok {
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", fmt.Errorf("%w: %s must be a string", domain.ErrInvalidField, name)
		}
	}
	return value, nil
}

// errorBody returns the spec §4 status and body for err. Any error that is
// not a client error is a server error, and its details stay out of the
// response.
func errorBody(err error) (int, errorResponse) {
	for _, e := range errorCodes {
		if errors.Is(err, e.err) {
			return e.status, errorResponse{Error: errorDetail{Code: e.code, Message: err.Error()}}
		}
	}
	return http.StatusInternalServerError, errorResponse{Error: errorDetail{
		Code:    "INTERNAL_ERROR",
		Message: "internal server error",
	}}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
