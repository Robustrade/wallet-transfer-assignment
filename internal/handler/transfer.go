package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
	"github.com/benjamin-benny/wallet-transfer/internal/service"
)

// createTransferRequest is the JSON request body for POST /transfers.
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         string `json:"amount"`
}

// TransferHandler handles transfer-related HTTP endpoints.
type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// Create handles POST /transfers.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Parse and validate fields into domain types.
	domainReq, err := parseRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.svc.Create(r.Context(), domainReq)
	if err != nil {
		status, msg := httpStatusForError(err)
		writeError(w, status, msg)
		return
	}

	// Idempotent replay of a success: downgrade 201 → 200.
	httpStatus := result.HTTPStatus
	if result.IsReplay && httpStatus == http.StatusCreated {
		httpStatus = http.StatusOK
	}

	writeJSON(w, httpStatus, result.Response)
}

func parseRequest(req createTransferRequest) (domain.CreateTransferRequest, error) {
	if req.IdempotencyKey == "" {
		return domain.CreateTransferRequest{}, domain.ErrMissingIdempotencyKey
	}

	fromID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		return domain.CreateTransferRequest{}, errInvalidField("fromWalletId", "must be a valid UUID")
	}

	toID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		return domain.CreateTransferRequest{}, errInvalidField("toWalletId", "must be a valid UUID")
	}

	if req.Amount == "" {
		return domain.CreateTransferRequest{}, errInvalidField("amount", "is required")
	}
	amount, err := domain.ParseAmount(req.Amount)
	if err != nil {
		return domain.CreateTransferRequest{}, errInvalidField("amount", err.Error())
	}

	return domain.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         amount,
	}, nil
}

// errInvalidField returns a descriptive validation error for a specific field.
type fieldError struct {
	field string
	msg   string
}

func (e fieldError) Error() string {
	return e.field + ": " + e.msg
}

func errInvalidField(field, msg string) error {
	return fieldError{field: field, msg: msg}
}
