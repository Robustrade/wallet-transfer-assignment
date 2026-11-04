package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/repository"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/service"
)

const maxTransferRequestBytes = 1 << 20

// TransferExecutor is the service capability required by the HTTP handler.
type TransferExecutor interface {
	ExecuteIdempotent(ctx context.Context, key string, request service.TransferRequest) (domain.Transfer, error)
}

// NewTransferHandler creates an HTTP handler for POST /transfers.
// Register it on the /transfers route in the application mux.
func NewTransferHandler(transfers TransferExecutor) http.Handler {
	return &transferHandler{transfers: transfers}
}

type transferHandler struct {
	transfers TransferExecutor
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	ID           string                `json:"id"`
	FromWalletID string                `json:"fromWalletId"`
	ToWalletID   string                `json:"toWalletId"`
	Amount       domain.Amount         `json:"amount"`
	Status       domain.TransferStatus `json:"status"`
	FailureCode  string                `json:"failureCode,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type transferAPIResponse struct {
	Transfer *transferResponse `json:"transfer,omitempty"`
	Error    *apiError         `json:"error,omitempty"`
}

func (h *transferHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, transferAPIResponse{
			Error: &apiError{Code: "method_not_allowed", Message: "method not allowed"},
		})
		return
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, transferAPIResponse{
				Error: &apiError{Code: "unsupported_media_type", Message: "content type must be application/json"},
			})
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxTransferRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var input createTransferRequest
	if err := decoder.Decode(&input); err != nil {
		writeDecodeError(w, err)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, transferAPIResponse{
			Error: &apiError{Code: "invalid_json", Message: "request body must contain exactly one JSON object"},
		})
		return
	}

	headerKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	bodyKey := strings.TrimSpace(input.IdempotencyKey)
	if headerKey != "" && bodyKey != "" && headerKey != bodyKey {
		writeJSON(w, http.StatusBadRequest, transferAPIResponse{
			Error: &apiError{Code: "idempotency_key_mismatch", Message: "Idempotency-Key header and idempotencyKey body field must match"},
		})
		return
	}
	key := headerKey
	if key == "" {
		key = bodyKey
	}

	transfer, err := h.transfers.ExecuteIdempotent(r.Context(), key, service.TransferRequest{
		FromWalletID: input.FromWalletID,
		ToWalletID:   input.ToWalletID,
		Amount:       domain.Amount(input.Amount),
	})
	if err != nil {
		status, responseError := mapTransferError(err)
		response := transferAPIResponse{Error: responseError}
		if transfer.ID != "" {
			response.Transfer = toTransferResponse(transfer)
		}
		writeJSON(w, status, response)
		return
	}

	writeJSON(w, http.StatusCreated, transferAPIResponse{Transfer: toTransferResponse(transfer)})
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeJSON(w, http.StatusRequestEntityTooLarge, transferAPIResponse{
			Error: &apiError{Code: "request_too_large", Message: "request body exceeds the maximum allowed size"},
		})
		return
	}
	writeJSON(w, http.StatusBadRequest, transferAPIResponse{
		Error: &apiError{Code: "invalid_json", Message: "request body must be valid JSON with the expected fields"},
	})
}

func mapTransferError(err error) (int, *apiError) {
	switch {
	case errors.Is(err, service.ErrInvalidIdempotencyKey):
		return http.StatusBadRequest, &apiError{Code: "invalid_idempotency_key", Message: "a non-empty idempotency key of at most 255 characters is required"}
	case errors.Is(err, service.ErrIdempotencyConflict):
		return http.StatusConflict, &apiError{Code: "idempotency_conflict", Message: "idempotency key was already used with a different request"}
	case errors.Is(err, domain.ErrInvalidWalletID):
		return http.StatusBadRequest, &apiError{Code: "invalid_wallet_id", Message: domain.ErrInvalidWalletID.Error()}
	case errors.Is(err, domain.ErrSameWallet):
		return http.StatusBadRequest, &apiError{Code: "same_wallet", Message: domain.ErrSameWallet.Error()}
	case errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest, &apiError{Code: "invalid_amount", Message: domain.ErrInvalidAmount.Error()}
	case errors.Is(err, repository.ErrWalletNotFound):
		return http.StatusNotFound, &apiError{Code: "wallet_not_found", Message: repository.ErrWalletNotFound.Error()}
	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, &apiError{Code: "insufficient_funds", Message: domain.ErrInsufficientFunds.Error()}
	case errors.Is(err, domain.ErrCurrencyMismatch):
		return http.StatusUnprocessableEntity, &apiError{Code: "currency_mismatch", Message: domain.ErrCurrencyMismatch.Error()}
	case errors.Is(err, domain.ErrBalanceOverflow):
		return http.StatusUnprocessableEntity, &apiError{Code: "balance_overflow", Message: domain.ErrBalanceOverflow.Error()}
	default:
		return http.StatusInternalServerError, &apiError{Code: "internal_error", Message: "an internal error occurred"}
	}
}

func toTransferResponse(transfer domain.Transfer) *transferResponse {
	return &transferResponse{
		ID:           transfer.ID,
		FromWalletID: transfer.FromWalletID,
		ToWalletID:   transfer.ToWalletID,
		Amount:       transfer.Amount,
		Status:       transfer.Status,
		FailureCode:  transfer.FailureCode,
	}
}

func writeJSON(w http.ResponseWriter, status int, response transferAPIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
