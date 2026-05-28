package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
)

// TransferHandler binds the POST /transfers route to the
// TransferService. Handlers are deliberately thin: no business logic,
// no SQL, no transactions — everything substantive lives in the
// service layer.
type TransferHandler struct {
	svc *service.TransferService
}

// NewTransferHandler wires the handler to its service dependency.
// svc must be non-nil.
func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// createTransferRequest is the wire shape of POST /transfers. JSON
// tags follow camelCase to match the assignment's example payload.
//
// We DisallowUnknownFields when decoding so a typo (e.g.
// "frmWalletId") is rejected with 400 rather than silently dropped.
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// transferResponse is the wire shape returned by POST /transfers (and
// would be returned by GET /transfers/{id} if/when we add it).
//
// FailureReason is `omitempty` so successful PROCESSED responses don't
// carry a noisy empty-string field.
type transferResponse struct {
	ID            string `json:"id"`
	FromWalletID  string `json:"fromWalletId"`
	ToWalletID    string `json:"toWalletId"`
	Amount        int64  `json:"amount"`
	State         string `json:"state"`
	FailureReason string `json:"failureReason,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// toTransferResponse renders the domain Transfer for the HTTP wire.
// Timestamps are forced to UTC with a millisecond-precision RFC 3339
// suffix so clients see a deterministic format independent of the
// server's local time zone.
func toTransferResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		ID:            t.ID.String(),
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		State:         string(t.State),
		FailureReason: t.FailureReason,
		CreatedAt:     t.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt:     t.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}

// Get handles GET /transfers/{id}. Returns the persisted transfer
// in the same shape as Create's response. Returns 400 on a
// malformed UUID, 404 when no row matches.
func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid transfer id"})
		return
	}
	t, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTransferResponse(t))
}

// transferListResponse is the wire shape for the per-wallet
// transfer history. The cursor is omitted (via `omitempty`) when
// the result is short of the page size — that's the "no more
// pages" signal to clients.
type transferListResponse struct {
	Transfers  []transferResponse `json:"transfers"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

// parseListQuery extracts and validates the optional ?limit and
// ?before query parameters. Defaults and clamping are deliberately
// done in the service layer (DefaultTransferListLimit /
// MaxTransferListLimit) so that any non-HTTP caller follows the
// same rules.
func parseListQuery(r *http.Request) (limit int, before *time.Time, err error) {
	if s := r.URL.Query().Get("limit"); s != "" {
		n, perr := strconv.Atoi(s)
		if perr != nil || n < 0 {
			return 0, nil, errBadQuery("invalid limit")
		}
		limit = n
	}
	if s := r.URL.Query().Get("before"); s != "" {
		t, perr := time.Parse(time.RFC3339Nano, s)
		if perr != nil {
			return 0, nil, errBadQuery("invalid before timestamp; expected RFC3339")
		}
		before = &t
	}
	return limit, before, nil
}

type badQueryError struct{ msg string }

func (e badQueryError) Error() string { return e.msg }
func errBadQuery(m string) error      { return badQueryError{msg: m} }

// Create handles POST /transfers.
//
// Flow:
//  1. Decode the JSON body with DisallowUnknownFields. A malformed
//     body or unknown field is rejected as 400 with the decoder's
//     error message (safe to expose; it's about the request, not
//     server internals).
//  2. Hand the request to the service. The service does all the
//     real work — locking, idempotency, double-entry, state machine.
//  3. Map any returned error through writeError (which routes
//     domain/repository sentinels to the right HTTP status and
//     hides DB internals from the client).
//  4. On success, emit the transfer as JSON with status 200.
//
// 200 OK is returned for both newly-created and replayed transfers.
// This matches the assignment requirement that duplicate requests
// "return the original result" without any client-visible difference.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid json: " + err.Error()})
		return
	}
	t, err := h.svc.Create(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTransferResponse(t))
}
