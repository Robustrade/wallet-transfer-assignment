// Package handler holds the HTTP handlers for the transfer service.
//
// Handlers are intentionally thin: they decode the request, call the service,
// and translate the service result to an HTTP response. All business logic
// lives in the service layer.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/middleware"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
	svciface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/svc/ifaces"
)

// TransferHandler wires the transfer service to HTTP.
type TransferHandler struct {
	service svciface.TransferServiceIface
}

// NewTransferHandler constructs the handler.
func NewTransferHandler(svc svciface.TransferServiceIface) *TransferHandler {
	return &TransferHandler{service: svc}
}

// CreateTransfer handles POST /transfers.
//
// Status mapping:
//
//	201 Created            on a fresh PROCESSED transfer
//	200 OK                 on a successful idempotent replay (PROCESSED or FAILED)
//	422 Unprocessable      on a fresh FAILED transfer (e.g. INSUFFICIENT_FUNDS)
//	400 Bad Request        on syntactic / domain validation failure
//	409 Conflict           on idempotency-key reuse with a different request body
//	5xx                    on an internal error
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()

	// DisallowUnknownFields gives strict idempotency: the body that produced
	// `request_hash` must match exactly. A duplicate request with a key seen
	// before but with extra fields is a client bug we want to surface, not
	// silently treat as a replay.
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req request.CreateTransferReq
	if err := dec.Decode(&req); err != nil {
		// http.MaxBytesReader exposes its size violation as *http.MaxBytesError.
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			middleware.WriteError(w, r, apperr.ErrPayloadTooLarge.WithWrap(err))
			return
		}
		middleware.WriteError(w, r, apperr.ErrBadJSON.WithWrap(err))
		return
	}

	resp, err := h.service.CreateTransfer(r.Context(), req)
	if err != nil {
		middleware.WriteError(w, r, err)
		return
	}

	status := http.StatusCreated
	switch {
	case resp.Replayed:
		status = http.StatusOK
	case resp.IsFailed():
		status = http.StatusUnprocessableEntity
	}
	middleware.WriteJSON(w, status, resp)
}
