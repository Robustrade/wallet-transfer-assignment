package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

const maxRequestBodyBytes = 1 << 20 // 1 MB

// writeJSON writes a JSON response with the given status code and payload.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a standard JSON error response.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

// decodeJSON safely decodes a JSON request body with size limit and unknown field rejection.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("request body cannot be empty")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxError):
			return fmt.Errorf("malformed JSON at position %d", syntaxError.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("malformed JSON")
		case errors.As(err, &unmarshalTypeError):
			return fmt.Errorf("invalid value for field %q", unmarshalTypeError.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("unknown field %s", fieldName)
		case errors.Is(err, io.EOF):
			return errors.New("request body cannot be empty")
		default:
			return err
		}
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must only contain a single JSON object")
	}

	return nil
}

// mapWalletError maps wallet service/domain errors to HTTP responses.
func mapWalletError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidWalletID), errors.Is(err, service.ErrNegativeWalletBalance):
		writeJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, domain.ErrWalletAlreadyExists):
		writeJSONError(w, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "wallet not found")
	default:
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

// mapTransferError maps transfer service/domain errors to HTTP responses.
func mapTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidTransferID),
		errors.Is(err, domain.ErrEmptyIdempotencyKey),
		errors.Is(err, domain.ErrEmptyWalletID),
		errors.Is(err, domain.ErrSameWalletTransfer),
		errors.Is(err, domain.ErrInvalidAmount):
		writeJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, domain.ErrTransferNotFound):
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeJSONError(w, http.StatusUnprocessableEntity, "WALLET_NOT_FOUND", err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeJSONError(w, http.StatusUnprocessableEntity, domain.ErrReasonInsufficientFunds, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeJSONError(w, http.StatusConflict, domain.ErrReasonIdempotencyConflict, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

// mapWalletTransferHistoryError maps errors when querying transfer history for a wallet.
func mapWalletTransferHistoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidWalletID), errors.Is(err, domain.ErrEmptyWalletID):
		writeJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "wallet not found")
	default:
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
