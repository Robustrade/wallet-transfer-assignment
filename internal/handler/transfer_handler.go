package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/sonalijais/wallet-transfer-assignment/internal/domain"
	"github.com/sonalijais/wallet-transfer-assignment/internal/service"
	"github.com/sonalijais/wallet-transfer-assignment/internal/utility"
)

type Handler struct {
	Service *service.Service
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTransferRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utility.WriteJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request")
		return
	}

	if err := req.Validate(); err != nil {
		utility.WriteJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	requestID := uuid.New().String()
	log.Printf("[START] request_id=%s idempotency_key=%s from=%s to=%s amount=%f",
		requestID, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount,
	)

	type contextKey string

	const RequestIDKey contextKey = "request_id"

	ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
	resp, err := h.Service.CreateTransfer(ctx, req)
	if err != nil {
		log.Printf("[REQUEST_FAILED] request_id=%s error=%v", requestID, err)
		var vErr *domain.ValidationError

		switch {

		case errors.As(err, &vErr):
			utility.WriteJSONError(w, http.StatusBadRequest, vErr.Code, vErr.Message)
			return
		case errors.Is(err, utility.ErrIdempotencyConflict):
			utility.WriteJSONError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error()) // 409
			return
		case errors.Is(err, utility.ErrRetryLater):
			utility.WriteJSONError(w, http.StatusConflict, "RETRY_LATER", err.Error()) // 409
			return
		default:
			utility.WriteJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			return
		}
	}

	log.Printf("[REQUEST_SUCCESS] request_id=%s transfer_id=%s", requestID, resp.TransferID)
	utility.WriteJSONWithData(w, http.StatusOK, resp)
}
