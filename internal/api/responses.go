package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"wallet/internal/constants"
	"wallet/internal/domain"
	"wallet/internal/dto"
)

// writeJSON renders v as the response body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Marshaling our own response types cannot realistically fail; if it
		// somehow does, the client gets a fixed internal error.
		body = []byte(`{"error":{"code":"INTERNAL_ERROR","message":"internal error"}}`)
		status = http.StatusInternalServerError
	}
	writeRaw(w, status, body)
}

// writeRaw sends pre-rendered bytes. Recorded outcomes are written this way so
// replays are byte-identical to the original response.
func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError maps a service/domain error to an HTTP status and error body, and
// logs it. Error mapping in one place keeps handlers thin and consistent.
func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, resp := errorResponse(err)

	if status >= http.StatusInternalServerError {
		logger.ErrorContext(r.Context(), "request failed",
			"method", r.Method, "path", r.URL.Path, "status", status, "error", err)
	} else {
		logger.WarnContext(r.Context(), "request rejected",
			"method", r.Method, "path", r.URL.Path, "status", status, "error", err)
	}

	writeJSON(w, status, resp)
}

// errorResponse is the single error-mapping table of the service.
func errorResponse(err error) (int, dto.ErrorResponse) {
	var validationErr *domain.ValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest, dto.NewErrorResponse(constants.CodeValidation, validationErr.Message)
	}

	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound, dto.NewErrorResponse(constants.CodeWalletNotFound, err.Error())

	case errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound, dto.NewErrorResponse(constants.CodeTransferNotFound, err.Error())

	case errors.Is(err, domain.ErrKeyReused):
		// Same idempotencyKey, different request payload: refuse loudly
		// instead of silently returning the first request's result.
		return http.StatusConflict, dto.NewErrorResponse(constants.CodeKeyReused,
			"idempotencyKey was already used with a different request")

	case errors.Is(err, domain.ErrInsufficientFunds):
		// Normally a recorded 422 outcome, never an error; defensive mapping
		// keeps the status correct if it ever escapes as one.
		return http.StatusUnprocessableEntity, dto.NewErrorResponse(constants.CodeInsufficientFunds, err.Error())

	case errors.Is(err, domain.ErrLockTimeout):
		return http.StatusServiceUnavailable, dto.NewErrorResponse(constants.CodeLockTimeout,
			"the request could not acquire its locks in time; retry with the same idempotencyKey")

	case errors.Is(err, domain.ErrStatementTimeout):
		return http.StatusServiceUnavailable, dto.NewErrorResponse(constants.CodeStatementTimeout,
			"the request exceeded the statement time limit; retry with the same idempotencyKey")

	case errors.Is(err, domain.ErrDatabaseUnavailable):
		// Transport-level failure (dial error, broken connection, saturated
		// pool): capacity, not correctness. Nothing was committed; retrying
		// the identical request is safe.
		return http.StatusServiceUnavailable, dto.NewErrorResponse(constants.CodeDatabaseUnavailable,
			"the database is temporarily unavailable; retry with the same idempotencyKey")

	case errors.Is(err, domain.ErrCommitOutcomeUnknown):
		// The commit's outcome could not be confirmed even by reading the
		// record back. Retrying the identical request with the same key is
		// safe: the retry either replays the recorded outcome or claims the
		// key fresh. The serious signal is the Error log in
		// resolveUnknownOutcome, not this response's log level.
		return http.StatusServiceUnavailable, dto.NewErrorResponse(constants.CodeOutcomeUnknown,
			"the request's outcome could not be confirmed; retry the same request with the same idempotencyKey")

	case errors.Is(err, context.Canceled):
		// The client disconnected (or its deadline passed) before the request
		// completed: not a server fault, so not a 500. Checked after the
		// domain cases so an already-classified failure keeps its own status.
		return constants.ClientClosedRequest, dto.NewErrorResponse(constants.CodeClientClosed,
			"the client closed the request before it completed")

	default:
		// Never leak internal error details to the client; the full error is
		// in the log line written by writeError.
		return http.StatusInternalServerError, dto.NewErrorResponse(constants.CodeInternal, "internal error")
	}
}
