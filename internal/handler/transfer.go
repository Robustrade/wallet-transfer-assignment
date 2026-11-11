package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"wallet-transfer-assignment/internal/appErrors"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

const MaxTransferBodyBytes int64 = 4096

// TransferService is the workflow contract consumed by the HTTP handler.
type TransferService interface {
	CreateTransfer(context.Context, domain.TransferRequest) (service.TransferResult, error)
}

type TransferHandler struct {
	service   TransferService
	maxAmount int64
}

func NewTransferHandler(service TransferService, maxAmount int64) *TransferHandler {
	return &TransferHandler{service: service, maxAmount: maxAmount}
}

func NewRouter(transfers TransferService, maxAmount int64) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /transfers", NewTransferHandler(transfers, maxAmount))
	return mux
}

func (h *TransferHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	request, err := decodeTransferRequest(w, r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := request.Validate(h.maxAmount); err != nil {
		writeError(w, r, err)
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), request)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeTransferResult(w, r, result)
}

func decodeTransferRequest(w http.ResponseWriter, r *http.Request) (domain.TransferRequest, error) {
	body := http.MaxBytesReader(w, r.Body, MaxTransferBodyBytes)
	defer func() { _ = body.Close() }()

	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	var request domain.TransferRequest
	if err := decoder.Decode(&request); err != nil {
		return domain.TransferRequest{}, appErrors.ErrInvalidTransfer
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.TransferRequest{}, appErrors.ErrInvalidTransfer
	}
	return request, nil
}

func writeTransferResult(w http.ResponseWriter, r *http.Request, result service.TransferResult) {
	switch result.StatusCode {
	case http.StatusCreated, http.StatusUnprocessableEntity:
		if result.Replayed {
			w.Header().Set("Idempotent-Replayed", "true")
		}
	case http.StatusAccepted:
		w.Header().Set("Retry-After", "1")
	default:
		writeError(w, r, appErrors.ErrInvalidTransition)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.StatusCode)

	if _, err := w.Write(result.Body); err != nil {
		slog.ErrorContext(r.Context(), "write transfer response failed", "error", err)
	}
}
