package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
)

// errorBody is the JSON shape returned for any error response. The optional
// `request_id` field carries the per-request id (from the RequestID
// middleware) so a client can quote the JSON body verbatim in a bug report
// and the operator can grep server logs by that id.
type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteError serializes the supplied error to a JSON response with the proper
// HTTP status. *apperr.AppError is mapped to its Code / HTTPStatus / Message;
// any other error is treated as an internal error.
//
// 5xx responses also emit a structured log line via slog.Default() carrying the
// underlying error (including any wrapped cause). 4xx responses are not
// logged here — the request log line emitted by the Logging middleware already
// records method/path/status, which is sufficient for client errors.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	ae, ok := apperr.As(err)
	if !ok {
		ae = apperr.ErrInternal.WithWrap(err)
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	requestID := RequestIDFromContext(ctx)
	if ae.HTTPStatus >= 500 {
		slog.Default().ErrorContext(ctx, "server error response",
			"error", err.Error(),
			"code", ae.Code,
			"status", ae.HTTPStatus,
			"request_id", requestID,
		)
	}
	WriteJSON(w, ae.HTTPStatus, errorBody{
		Code:      ae.Code,
		Message:   ae.Message,
		RequestID: requestID,
	})
}

// WriteJSON writes the supplied value as a JSON response with the given status.
//
// Encoding errors after WriteHeader cannot be recovered (the client has
// already received the status code), so the best we can do is surface them in
// logs for forensics.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("failed to encode JSON response", "error", err, "status", status)
	}
}

// NotFoundHandler returns 404 with a JSON body. Use it to override gorilla/mux's
// default plain-text 404.
func NotFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusNotFound, errorBody{
			Code:      "NOT_FOUND",
			Message:   "route not found",
			RequestID: RequestIDFromContext(r.Context()),
		})
	})
}

// MethodNotAllowedHandler returns 405 with a JSON body. If allowedMethods is
// non-nil, its result is used to populate the RFC 9110 §10.2.1 `Allow` header
// so clients can discover which methods the path does support.
func MethodNotAllowedHandler(allowedMethods func(*http.Request) []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowedMethods != nil {
			if methods := allowedMethods(r); len(methods) > 0 {
				w.Header().Set("Allow", strings.Join(methods, ", "))
			}
		}
		WriteJSON(w, http.StatusMethodNotAllowed, errorBody{
			Code:      "METHOD_NOT_ALLOWED",
			Message:   "method not allowed",
			RequestID: RequestIDFromContext(r.Context()),
		})
	})
}

// MaxBodyBytes wraps a handler to limit request body size. Bodies larger than
// the limit cause subsequent reads to fail; the handler can then return 413.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
