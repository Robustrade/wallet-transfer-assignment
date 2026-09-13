package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ankit729/wallet-transfer/internal/domain"
	"github.com/ankit729/wallet-transfer/internal/repository"
	"github.com/ankit729/wallet-transfer/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(service *service.Service) http.Handler {
	handler := &Handler{
		service: service,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("GET /readyz", handler.ready)

	mux.HandleFunc("POST /wallets", handler.createWallet)
	mux.HandleFunc("GET /wallets/{id}", handler.getWallet)

	mux.HandleFunc("POST /transfers", handler.createTransfer)
	mux.HandleFunc("GET /transfers/{id}", handler.getTransfer)

	return requestLogger(mux)
}

func (h *Handler) health(
	w http.ResponseWriter,
	_ *http.Request,
) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *Handler) ready(
	w http.ResponseWriter,
	r *http.Request,
) {
	if err := h.service.Ping(r.Context()); err != nil {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"database_unavailable",
			"database is not ready",
		)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

func (h *Handler) createWallet(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request domain.CreateWalletRequest

	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

func (h *Handler) getWallet(
	w http.ResponseWriter,
	r *http.Request,
) {
	walletID := r.PathValue("id")

	wallet, err := h.service.GetWallet(
		r.Context(),
		walletID,
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}

func (h *Handler) createTransfer(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request domain.TransferRequest

	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	headerKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))

	if request.IdempotencyKey == "" && headerKey != "" {
		request.IdempotencyKey = headerKey
	} else if request.IdempotencyKey != "" &&
		headerKey != "" &&
		request.IdempotencyKey != headerKey {
		writeError(
			w,
			http.StatusBadRequest,
			"idempotency_key_mismatch",
			"body idempotencyKey and Idempotency-Key header must match",
		)
		return
	}

	result, err := h.service.CreateTransfer(
		r.Context(),
		request,
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set(
		"X-Idempotent-Replay",
		strconv.FormatBool(result.Replayed),
	)

	writeJSONBytes(
		w,
		result.ResponseCode,
		result.ResponseBody,
	)
}

func (h *Handler) getTransfer(
	w http.ResponseWriter,
	r *http.Request,
) {
	transferID := r.PathValue("id")

	result, err := h.service.GetTransfer(
		r.Context(),
		transferID,
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	response := struct {
		TransferID     string     `json:"transferId"`
		IdempotencyKey string     `json:"idempotencyKey"`
		Status         string     `json:"status"`
		FromWalletID   string     `json:"fromWalletId"`
		ToWalletID     string     `json:"toWalletId"`
		Amount         int64      `json:"amount"`
		CreatedAt      time.Time  `json:"createdAt"`
		ProcessedAt    *time.Time `json:"processedAt,omitempty"`
		FailureCode    string     `json:"failureCode,omitempty"`
		FailureMessage string     `json:"failureMessage,omitempty"`
	}{
		TransferID:     result.ID,
		IdempotencyKey: result.IdempotencyKey,
		Status:         string(result.Status),
		FromWalletID:   result.FromWalletID,
		ToWalletID:     result.ToWalletID,
		Amount:         result.Amount,
		CreatedAt:      result.CreatedAt,
		ProcessedAt:    result.ProcessedAt,
		FailureCode:    result.FailureCode,
		FailureMessage: result.FailureMessage,
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) writeServiceError(
	w http.ResponseWriter,
	err error,
) {
	var validationError *domain.ValidationError

	switch {
	case errors.As(err, &validationError):
		writeError(
			w,
			http.StatusBadRequest,
			"validation_error",
			validationError.Message,
		)

	case errors.Is(err, repository.ErrIdempotencyConflict):
		writeError(
			w,
			http.StatusConflict,
			"idempotency_conflict",
			"the idempotency key was already used with a different request",
		)

	case errors.Is(err, repository.ErrWalletAlreadyExists):
		writeError(
			w,
			http.StatusConflict,
			"wallet_already_exists",
			"wallet already exists",
		)

	case errors.Is(err, repository.ErrWalletNotFound):
		writeError(
			w,
			http.StatusNotFound,
			"wallet_not_found",
			"wallet was not found",
		)

	case errors.Is(err, repository.ErrTransferNotFound):
		writeError(
			w,
			http.StatusNotFound,
			"transfer_not_found",
			"transfer was not found",
		)

	default:
		slog.Error("request failed", "error", err)

		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
	}
}

func decodeJSON(
	w http.ResponseWriter,
	r *http.Request,
	target any,
) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain exactly one JSON object")
	}

	return nil
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	body, err := json.Marshal(value)
	if err != nil {
		writeError(
			w,
			http.StatusInternalServerError,
			"serialization_error",
			"failed to serialize response",
		)
		return
	}

	writeJSONBytes(w, status, body)
}

func writeJSONBytes(
	w http.ResponseWriter,
	status int,
	body []byte,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	writeJSON(w, status, domain.ErrorResponse{
		Error: domain.APIError{
			Code:    code,
			Message: message,
		},
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		start := time.Now()

		next.ServeHTTP(w, r)

		slog.Info(
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start).String(),
		)
	})
}
