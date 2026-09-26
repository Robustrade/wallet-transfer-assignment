package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"wallet/internal/constants"
	"wallet/internal/domain"
	"wallet/internal/dto"
)

// rawTransferRequest mirrors dto.TransferRequest with pointer fields so the
// decoder can tell "field absent" (or null) from "field present". Value-level
// rules (empty string, non-positive amount, ...) live in the service; this
// type only answers "did the client send every field with the right JSON
// type".
type rawTransferRequest struct {
	IdempotencyKey *string `json:"idempotencyKey"`
	FromWalletID   *string `json:"fromWalletId"`
	ToWalletID     *string `json:"toWalletId"`
	Amount         *int64  `json:"amount"`
}

// decodeTransferRequest reads a POST /transfers body. Every failure is a
// *domain.ValidationError so clients see one error format for shape and value
// problems alike.
func decodeTransferRequest(w http.ResponseWriter, r *http.Request) (dto.TransferRequest, error) {
	var raw rawTransferRequest

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, constants.MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return dto.TransferRequest{}, decodeError(err)
	}
	// Reject trailing content: exactly one JSON object per request.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return dto.TransferRequest{}, domain.NewValidationError("body must contain exactly one JSON object")
	}

	missing := make([]string, 0, 4)
	if raw.IdempotencyKey == nil {
		missing = append(missing, "idempotencyKey")
	}
	if raw.FromWalletID == nil {
		missing = append(missing, "fromWalletId")
	}
	if raw.ToWalletID == nil {
		missing = append(missing, "toWalletId")
	}
	if raw.Amount == nil {
		missing = append(missing, "amount")
	}
	if len(missing) > 0 {
		return dto.TransferRequest{}, domain.NewValidationError("missing required field(s): %s", strings.Join(missing, ", "))
	}

	return dto.TransferRequest{
		IdempotencyKey: *raw.IdempotencyKey,
		FromWalletID:   *raw.FromWalletID,
		ToWalletID:     *raw.ToWalletID,
		Amount:         *raw.Amount,
	}, nil
}

// decodeError turns json decoding failures into client-facing validation
// errors with messages that name the offending field.
func decodeError(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return domain.NewValidationError("request body must be at most %d bytes", maxBytesErr.Limit)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return domain.NewValidationError("invalid value for %s: expected %s", typeErr.Field, expectedJSONType(typeErr.Type))
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return domain.NewValidationError("request body must be a JSON object")
	}

	// DisallowUnknownFields has no dedicated error type; its message starts
	// with "json: unknown field".
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		return domain.NewValidationError("%s", err.Error())
	}

	return domain.NewValidationError("malformed JSON body: %s", err.Error())
}

func expectedJSONType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Int64:
		return "an integer"
	default:
		return "a number"
	}
}
