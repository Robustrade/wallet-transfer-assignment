package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/transfer"
)

type TransferService interface {
	Create(context.Context, transfer.CreateRequest) (transfer.Outcome, error)
}

type TransferHandler struct {
	service TransferService
}

func NewTransferHandler(service TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

func (h *TransferHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.create)
}

func (h *TransferHandler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request transfer.CreateRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, &transfer.Error{StatusCode: 400, Code: "invalid_json", Message: "request body must be a valid transfer JSON object"})
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, &transfer.Error{StatusCode: 400, Code: "invalid_json", Message: "request body must contain exactly one JSON object"})
		return
	}

	outcome, err := h.service.Create(r.Context(), request)
	if err != nil {
		var apiError *transfer.Error
		if errors.As(err, &apiError) {
			writeError(w, apiError)
			return
		}
		slog.ErrorContext(r.Context(), "create transfer failed", "error", err)
		writeError(w, &transfer.Error{StatusCode: 500, Code: "internal_error", Message: "transfer could not be completed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(outcome.StatusCode)
	if _, err := w.Write(outcome.Body); err != nil {
		slog.ErrorContext(r.Context(), "write transfer response", "error", err)
	}
	w.Write([]byte("\n"))
}

func writeError(w http.ResponseWriter, apiError *transfer.Error) {
	body, err := json.Marshal(transfer.APIError{Code: apiError.Code, Message: apiError.Message})
	if err != nil {
		http.Error(w, `{"code":"internal_error","message":"transfer could not be completed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiError.StatusCode)
	_, _ = w.Write(append(body, '\n'))
}
