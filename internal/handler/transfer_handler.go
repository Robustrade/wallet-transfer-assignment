package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{
		service: s,
	}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {

	var req service.TransferRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid request body",
		)
		return
	}

	resp, err := h.service.CreateTransfer(
		r.Context(),
		req,
	)

	if err != nil {

		switch {

		case errors.Is(err, service.ErrInvalidAmount):
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return

		case errors.Is(err, service.ErrSameWallet):
			writeError(
				w,
				http.StatusBadRequest,
				err.Error(),
			)
			return

		case errors.Is(err, service.ErrInsufficientBalance):
			writeError(
				w,
				http.StatusConflict,
				err.Error(),
			)
			return

		default:
			writeError(
				w,
				http.StatusInternalServerError,
				"internal server error",
			)
			return
		}
	}

	writeJSON(
		w,
		http.StatusCreated,
		resp,
	)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	v any,
) {

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(v)
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {

	writeJSON(
		w,
		status,
		map[string]string{
			"error": message,
		},
	)
}
