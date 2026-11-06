package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type transferHandler struct {
	svc    TransferCreator
	logger *slog.Logger
}

// create handles POST /transfers.
//
//	201 PROCESSED · 422 INSUFFICIENT_FUNDS (FAILED transfer in body) · replays carry
//	Idempotent-Replayed: true with the original status code and body.
func (h *transferHandler) create(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	req, err := body.toDomain()
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	res, err := h.svc.CreateTransfer(r.Context(), req)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	w.Header().Set("Location", "/transfers/"+res.Transfer.ID.String())

	transfer := newTransferResponse(res.Transfer)
	if res.Transfer.Status == domain.StatusFailed {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
			Error:    errorBody{Code: string(res.Transfer.FailureReason), Message: failureMessage(res.Transfer.FailureReason)},
			Transfer: &transfer,
		})
		return
	}
	writeJSON(w, http.StatusCreated, transfer)
}

func failureMessage(reason domain.FailureReason) string {
	switch reason {
	case domain.FailureInsufficientFunds:
		return "source wallet has insufficient funds"
	default:
		return "transfer failed"
	}
}
