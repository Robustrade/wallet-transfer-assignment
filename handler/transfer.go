package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(
	service *service.TransferService,
) *TransferHandler {
	return &TransferHandler{
		service: service,
	}
}

func (h *TransferHandler) CreateTransfer(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "request body is required")
		return
	}

	defer func() {
		_ = r.Body.Close()
	}()

	var req model.TransferRequest

	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Reject multiple JSON objects in one request.
	var extra interface{}
	if err := decoder.Decode(&extra); err == nil {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}

	transfer, err := h.service.CreateTransfer(
		r.Context(),
		req,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, err.Error())

		case errors.Is(err, service.ErrWalletNotFound):
			writeError(w, http.StatusNotFound, err.Error())

		case errors.Is(err, service.ErrIdempotencyConflict):
			writeError(w, http.StatusConflict, err.Error())

		case errors.Is(err, service.ErrInsufficientFunds):
			writeError(w, http.StatusUnprocessableEntity, err.Error())

		default:
			writeError(w, http.StatusInternalServerError, "internal server error")
		}

		return
	}

	response := model.TransferResponse{
		ID:             transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         json.Number(service.FormatAmount(transfer.Amount)),
		Status:         transfer.Status,
	}

	// We use 201 for a newly created transfer.
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
