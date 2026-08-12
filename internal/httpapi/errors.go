package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

// domainErrorMap is the single table-driven status/code mapper described in
// docs/api.md §6.4. The handler layer never inspects a driver-level error —
// only these domain sentinels — which is what keeps Postgres-specific
// knowledge out of this package.
var domainErrorMap = []struct {
	err    error
	status int
	code   string
}{
	{domain.ErrWalletNotFound, http.StatusNotFound, "WALLET_NOT_FOUND"},
	{domain.ErrTransferNotFound, http.StatusNotFound, "TRANSFER_NOT_FOUND"},
	{domain.ErrWalletAlreadyExists, http.StatusConflict, "WALLET_ALREADY_EXISTS"},
	{domain.ErrIdempotencyKeyReuse, http.StatusConflict, "IDEMPOTENCY_KEY_REUSE"},
	{domain.ErrCurrencyMismatch, http.StatusConflict, "CURRENCY_MISMATCH"},
	{domain.ErrTransferInProgress, http.StatusConflict, "TRANSFER_IN_PROGRESS"},
	{domain.ErrResourceBusy, http.StatusServiceUnavailable, "RESOURCE_BUSY"},
}

// writeDomainError maps err to its HTTP status/code and writes the error
// envelope. Any error not recognized here — including anything that wraps a
// driver-level error — becomes a 500 with no internal detail leaked to the
// client; the raw error is logged server-side with the request id instead.
func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		writeValidationError(w, ve.Field, ve.Message)
		return
	}

	for _, m := range domainErrorMap {
		if errors.Is(err, m.err) {
			if m.status == http.StatusServiceUnavailable {
				w.Header().Set("Retry-After", "1")
			}
			writeJSON(w, m.status, errorResponse{Error: errorBody{Code: m.code, Message: err.Error()}})
			return
		}
	}

	loggerFromContext(r.Context()).Error("unmapped error", "error", err)
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{Code: "INTERNAL_ERROR", Message: "an internal error occurred"}})
}

func writeValidationError(w http.ResponseWriter, field, message string) {
	writeJSON(w, http.StatusBadRequest, errorResponse{Error: errorBody{Code: "VALIDATION_ERROR", Message: message, Field: field}})
}

func writeMalformed(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, errorResponse{Error: errorBody{Code: "MALFORMED_REQUEST", Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
