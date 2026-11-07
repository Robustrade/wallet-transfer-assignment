// Package handler is the HTTP transport: it decodes requests, calls the service
// and maps results and errors to HTTP responses. It holds no business rules.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/service"
)

const maxBodyBytes = 1 << 20 // 1 MiB; a transfer request is a few hundred bytes

// TransferCreator is the service operation the handler depends on.
type TransferCreator interface {
	CreateTransfer(ctx context.Context, req domain.TransferRequest) (service.Result, error)
}

type TransferHandler struct {
	svc    TransferCreator
	logger *slog.Logger
}

func NewTransferHandler(svc TransferCreator, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{svc: svc, logger: logger}
}

// Register adds the transfer routes to mux.
func (h *TransferHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.createTransfer)
}

type createTransferRequest struct {
	// A pointer distinguishes an absent key (no deduplication) from an explicit
	// empty string, which is almost certainly a client bug and is rejected.
	IdempotencyKey *string `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
}

type transferResponse struct {
	ID             string  `json:"id"`
	IdempotencyKey *string `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Status         string  `json:"status"`
	FailureReason  *string `json:"failureReason"`
	CreatedAt      string  `json:"createdAt"`
}

func (h *TransferHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	req, err := decodeCreateTransfer(w, r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	res, err := h.svc.CreateTransfer(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	status := http.StatusCreated
	if res.Transfer.Status == domain.StatusFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, toResponse(res.Transfer))
}

func decodeCreateTransfer(w http.ResponseWriter, r *http.Request) (domain.TransferRequest, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var body createTransferRequest
	if err := dec.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return domain.TransferRequest{}, err
		}
		return domain.TransferRequest{}, fmt.Errorf("invalid JSON body: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return domain.TransferRequest{}, errors.New("invalid JSON body: unexpected data after the JSON object")
	}

	req := domain.TransferRequest{
		FromWalletID: body.FromWalletID,
		ToWalletID:   body.ToWalletID,
		Amount:       body.Amount,
	}
	if body.IdempotencyKey != nil {
		if *body.IdempotencyKey == "" {
			return domain.TransferRequest{}, errors.New("idempotencyKey: must not be empty; omit it to send without one")
		}
		req.IdempotencyKey = *body.IdempotencyKey
	}
	return req, nil
}

func (h *TransferHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var vErr *domain.ValidationError
	switch {
	case errors.As(err, &vErr):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", vErr.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED",
			"idempotencyKey was already used with a different request")
	case errors.Is(err, domain.ErrTemporarilyUnavailable):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "RETRYABLE",
			"temporarily unavailable; retry with the same idempotencyKey")
	default:
		// Details stay in the logs; the client gets a generic message.
		h.logger.ErrorContext(r.Context(), "create transfer failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func toResponse(t domain.Transfer) transferResponse {
	resp := transferResponse{
		ID:           t.ID.String(),
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount,
		Status:       string(t.Status),
		CreatedAt:    t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if t.IdempotencyKey != "" {
		resp.IdempotencyKey = &t.IdempotencyKey
	}
	if t.FailureReason != "" {
		reason := string(t.FailureReason)
		resp.FailureReason = &reason
	}
	return resp
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code, body.Error.Message = code, message
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent; an encode error here means the client went away.
	_ = json.NewEncoder(w).Encode(v)
}
