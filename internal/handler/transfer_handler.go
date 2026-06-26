package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/service"
)

// maxBodyBytes bounds the size of a request body to protect the decoder from
// unbounded input.
const maxBodyBytes = 1 << 20 // 1 MiB

// transferHandler holds the service dependency for the transfer/wallet routes.
type transferHandler struct {
	svc service.TransferService
}

// create handles POST /transfers. It decodes a bounded, strict JSON body, runs
// the transfer through the service, and surfaces exactly-once semantics:
//
//	fresh PROCESSED success      -> 201 Created
//	replayed (non-failed)        -> 200 OK + Idempotent-Replayed: true
//	insufficient funds (422)     -> FAILED transfer body + error envelope
//	other domain errors          -> mapped via writeError
func (h *transferHandler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req transferRequest
	if err := dec.Decode(&req); err != nil {
		writeBadRequest(w, "malformed request body")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeBadRequest(w, "request body must contain a single JSON object")
		return
	}

	result, err := h.svc.Transfer(r.Context(), req.toInput())

	if errors.Is(err, domain.ErrInsufficientFunds) {
		// Business failure: the service returns the durable FAILED transfer
		// alongside the error. Surface both as 422.
		if result != nil && result.Replayed {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		status, code, message := statusForError(err)
		writeJSON(w, status, failedTransferBody(result, code, message))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if result == nil || result.Transfer == nil {
		writeError(w, r, errors.New("nil transfer result"))
		return
	}

	body := newTransferResponse(result.Transfer)
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
		writeJSON(w, http.StatusOK, body)
		return
	}
	writeJSON(w, http.StatusCreated, body)
}

// get handles GET /transfers/{id}.
func (h *transferHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransferResponse(t))
}

// balance handles GET /wallets/{id}/balance.
func (h *transferHandler) balance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	b, err := h.svc.GetWalletBalance(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newWalletBalanceResponse(b))
}

// failedTransferResponse is the 422 body: the FAILED transfer alongside the
// error envelope, so clients see both the durable outcome and the reason.
type failedTransferResponse struct {
	Transfer *transferResponse `json:"transfer,omitempty"`
	Error    errorDetail       `json:"error"`
}

func failedTransferBody(result *service.TransferResult, code, message string) failedTransferResponse {
	body := failedTransferResponse{Error: errorDetail{Code: code, Message: message}}
	if result != nil && result.Transfer != nil {
		tr := newTransferResponse(result.Transfer)
		body.Transfer = &tr
	}
	return body
}
