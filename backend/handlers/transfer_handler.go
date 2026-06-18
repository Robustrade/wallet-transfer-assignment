package handler

import (
	"encoding/json"
	"net/http"
	"wallet-transfer/service"
)

type TransferHandler struct {
	transferService *service.TransferService
}

func NewTransferHandler(
	transferService *service.TransferService,
) *TransferHandler {
	return &TransferHandler{
		transferService: transferService,
	}
}

func (h *TransferHandler) CreateTransfer(
	w http.ResponseWriter,
	r *http.Request,
) {

	var req service.CreateTransferRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {

		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)

		return
	}

	transfer, err := h.transferService.CreateTransfer(
		r.Context(),
		req,
	)

	if err != nil {

		switch err {

		case service.ErrInvalidAmount,
			service.ErrSameWallet,
			service.ErrMissingIdempotency:

			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

			return

		case service.ErrWalletNotFound:

			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

			return

		case service.ErrInsufficientFunds:

			http.Error(
				w,
				err.Error(),
				http.StatusConflict,
			)

			return

		default:

			http.Error(
				w,
				"internal server error",
				http.StatusInternalServerError,
			)

			return
		}
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(transfer)
}
