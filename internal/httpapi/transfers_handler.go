package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

// TransferService is the subset of service.TransferService this handler
// needs. Defining it here (consumer-side) rather than depending on the
// concrete type lets tests inject a fake without touching a database.
type TransferService interface {
	Create(ctx context.Context, cmd domain.TransferCommand) (service.TransferResult, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
}

type transfersHandler struct {
	transfers TransferService
}

// Create handles POST /transfers. It only ever does: decode -> resolve the
// idempotency key -> call the service -> map the result. All business rules
// live in TransferService.Create.
func (h *transfersHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createTransferRequest
	if err := dec.Decode(&req); err != nil {
		writeMalformed(w, "request body is not a valid transfer request: "+err.Error())
		return
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeMalformed(w, "request body must contain a single JSON object")
		return
	}

	idempotencyKey, ok := resolveIdempotencyKey(w, r, req.IdempotencyKey)
	if !ok {
		return
	}

	cmd := domain.TransferCommand{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		AmountMinor:    int64(req.Amount),
		Currency:       req.Currency,
	}

	result, err := h.transfers.Create(r.Context(), cmd)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}

	status := http.StatusCreated
	if result.Transfer.Status == domain.TransferStatusFailed {
		status = http.StatusUnprocessableEntity
	}
	if result.Outcome == service.OutcomeReplayed {
		w.Header().Set("Idempotent-Replay", "true")
	}
	writeJSON(w, status, newTransferResponse(result.Transfer))
}

// resolveIdempotencyKey implements the body-field-or-header rule from
// docs/api.md §6.2: exactly one source must supply the key, or both must
// agree.
func resolveIdempotencyKey(w http.ResponseWriter, r *http.Request, bodyKey string) (string, bool) {
	headerKey := r.Header.Get("Idempotency-Key")
	switch {
	case bodyKey == "" && headerKey == "":
		writeValidationError(w, "idempotencyKey", "idempotencyKey is required (request body field or Idempotency-Key header)")
		return "", false
	case bodyKey == "":
		return headerKey, true
	case headerKey != "" && headerKey != bodyKey:
		writeValidationError(w, "idempotencyKey", "idempotencyKey in the request body and the Idempotency-Key header must match")
		return "", false
	default:
		return bodyKey, true
	}
}

// Get handles GET /transfers/{id}.
func (h *transfersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeDomainError(w, r, domain.ErrTransferNotFound)
		return
	}
	transfer, err := h.transfers.Get(r.Context(), id)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransferResponse(transfer))
}
