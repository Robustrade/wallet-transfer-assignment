package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
	"wallet-service/internal/service"
)

type transferService interface {
	Transfer(context.Context, domain.Request) (repository.Result, error)
}

func createTransfer(transfers transferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started, requestID := time.Now(), rand.Text()
		ctx := service.WithRequestID(r.Context(), requestID)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", requestID)
		status, code := http.StatusInternalServerError, ""
		var deliveryErr error
		defer func() {
			slog.InfoContext(ctx, "http_request", "request_id", requestID, "http_status", status,
				"error_code", code, "elapsed_ms", time.Since(started).Milliseconds(), "delivery_failed", deliveryErr != nil)
		}()
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			status, code = http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE"
			deliveryErr = writeError(w, status, code)
			return
		}
		body := http.MaxBytesReader(w, r.Body, 4<<10)
		defer body.Close()
		req, err := decodeRequest(body)
		if err != nil {
			status, code = http.StatusBadRequest, "INVALID_REQUEST"
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				status, code = http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE"
			} else if errors.Is(err, domain.ErrSelfTransfer) {
				code = "SELF_TRANSFER"
			}
			deliveryErr = writeError(w, status, code)
			return
		}
		result, err := transfers.Transfer(ctx, req)
		if err != nil {
			status, code = http.StatusInternalServerError, "INTERNAL_ERROR"
			switch {
			case errors.Is(err, service.ErrUnavailable), errors.Is(err, service.ErrCommitUnknown):
				status, code = http.StatusServiceUnavailable, "TEMPORARILY_UNAVAILABLE"
			case errors.Is(err, domain.ErrInvalidRequest):
				status, code = http.StatusBadRequest, "INVALID_REQUEST"
			case errors.Is(err, domain.ErrSelfTransfer):
				status, code = http.StatusBadRequest, "SELF_TRANSFER"
			case errors.Is(err, domain.ErrIdempotencyConflict):
				status, code = http.StatusConflict, "IDEMPOTENCY_CONFLICT"
			}
			deliveryErr = writeError(w, status, code)
			return
		}
		status = result.HTTPStatus
		w.WriteHeader(status)
		_, deliveryErr = io.WriteString(w, result.Body)
	}
}

func decodeRequest(body io.Reader) (domain.Request, error) {
	var req domain.Request
	data, err := io.ReadAll(body)
	if err != nil {
		return req, err
	}
	if !utf8.Valid(data) {
		return req, domain.ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return req, domain.ErrInvalidRequest
	}
	seen := make(map[string]bool, 4)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return req, domain.ErrInvalidRequest
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return req, domain.ErrInvalidRequest
		}
		switch name {
		case "idempotencyKey":
			err = json.Unmarshal(value, &req.IdempotencyKey)
		case "fromWalletId":
			err = json.Unmarshal(value, &req.FromWalletID)
		case "toWalletId":
			err = json.Unmarshal(value, &req.ToWalletID)
		case "amount":
			err = json.Unmarshal(value, &req.Amount)
		default:
			return req, domain.ErrInvalidRequest
		}
		if err != nil {
			return req, domain.ErrInvalidRequest
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') || len(seen) != 4 {
		return req, domain.ErrInvalidRequest
	}
	if decoder.Decode(new(any)) != io.EOF {
		return req, domain.ErrInvalidRequest
	}
	return req, domain.ValidateRequest(req)
}

func writeError(w http.ResponseWriter, status int, code string) error {
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
}
