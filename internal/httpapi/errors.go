package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	TransferID string `json:"transferId,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "INTERNAL_ERROR"
	message := "internal error"

	switch {
	case errors.Is(err, domain.ErrMissingField), errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		status, code, message = http.StatusBadRequest, "INVALID_REQUEST", err.Error()
	case errors.Is(err, domain.ErrWalletNotFound):
		status, code, message = http.StatusNotFound, "WALLET_NOT_FOUND", err.Error()
	case errors.Is(err, domain.ErrTransferNotFound):
		status, code, message = http.StatusNotFound, "TRANSFER_NOT_FOUND", err.Error()
	case errors.Is(err, domain.ErrWalletExists):
		status, code, message = http.StatusConflict, "WALLET_ALREADY_EXISTS", err.Error()
	case errors.Is(err, domain.ErrIdempotencyConflict):
		status, code, message = http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", err.Error()
	case errors.Is(err, domain.ErrDuplicateKey):

		status, code, message = http.StatusConflict, "CONCURRENT_REQUEST",
			"this idempotency key could not be resolved; retry the request"
	default:
		s.logger.ErrorContext(r.Context(), "request failed",
			slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Any("error", err))
	}

	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func decodeJSON(r *http.Request, dst any) error {
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil {
		return nil
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return errors.New("field " + typeErr.Field + " has the wrong type")
	}
	return errors.New("request body is not valid JSON")
}
