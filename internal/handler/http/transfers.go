package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

// TransferService is the handler's only application dependency.
type TransferService interface {
	Transfer(context.Context, service.Request) (repository.Result, error)
}

const maxRequestBytes = 1 << 20

type transferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func transfers(svc TransferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use POST for transfers")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		// Validate bytes before encoding/json can replace malformed UTF-8.
		body, err := io.ReadAll(r.Body)
		if err != nil || !utf8.Valid(body) || !validUnicodeEscapes(body) {
			writeError(w, 400, "INVALID_REQUEST", "Body must be valid UTF-8 JSON within the size limit")
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var input *transferRequest
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Body must be a valid transfer JSON object")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Body must contain one JSON object")
			return
		}
		if input == nil || strings.TrimSpace(input.FromWalletID) == "" || strings.TrimSpace(input.ToWalletID) == "" || input.Amount <= 0 || input.FromWalletID == input.ToWalletID {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Provide distinct wallet IDs and a positive integer amount")
			return
		}
		result, err := svc.Transfer(r.Context(), service.Request{IdempotencyKey: input.IdempotencyKey, FromWalletID: input.FromWalletID, ToWalletID: input.ToWalletID, Amount: input.Amount})
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidRequest):
				writeError(w, 400, "INVALID_REQUEST", "Invalid transfer request")
			case errors.Is(err, service.ErrWalletNotFound):
				writeError(w, 404, "WALLET_NOT_FOUND", "Wallet not found")
			case errors.Is(err, service.ErrIdempotencyConflict):
				writeError(w, 409, "IDEMPOTENCY_CONFLICT", "Idempotency key was used for a different request")
			default:
				writeError(w, 500, "INTERNAL_ERROR", "Unable to complete transfer")
			}
			return
		}
		// The service owns terminal result creation and durable replay. Preserve its
		// status/body, including transferId and FAILED, rather than rebuilding it.
		if (result.StatusCode != http.StatusCreated && result.StatusCode != http.StatusUnprocessableEntity) || !json.Valid([]byte(result.Body)) {
			writeError(w, 500, "INTERNAL_ERROR", "Unable to complete transfer")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.StatusCode)
		_, _ = w.Write([]byte(result.Body))
	}
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: errorDetail{Code: code, Message: message}})
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject them before
// decoding so distinct wire identifiers cannot silently become the same key/ID.
// JSON syntax itself is still validated by the standard decoder.
func validUnicodeEscapes(body []byte) bool {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if i >= len(body) {
			return false
		}
		if body[i] != 'u' {
			continue
		} // Includes escaped backslashes, not Unicode escapes.
		if i+4 >= len(body) {
			return false
		}
		code, err := strconv.ParseUint(string(body[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(body[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
