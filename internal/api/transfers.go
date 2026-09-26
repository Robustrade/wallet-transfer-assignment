package api

import (
	"net/http"

	"wallet/internal/dto"
)

// postTransfer handles canonical POST /v1/transfers and the legacy
// POST /transfers alias. The service decides everything: the handler only
// decodes the body and writes back the outcome the service recorded (201, 404,
// or 422) or reports the error's mapping.
func (h *handler) postTransfer(w http.ResponseWriter, r *http.Request) {
	req, err := decodeTransferRequest(w, r)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	result, err := h.svc.Execute(r.Context(), req)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	writeRaw(w, result.Status, result.Body)
}

// getTransfer handles GET /transfers/{id}.
func (h *handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, err := h.svc.GetTransfer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.NewTransferResponse(transfer))
}
