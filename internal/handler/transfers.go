// Package handler is the HTTP transport. It parses requests, maps outcomes to
// status codes, and writes JSON. It holds no business logic: every decision
// about what a transfer does belongs to the service.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/service"
)

// maxRequestBody bounds what will be read from a client. A transfer request is
// a few hundred bytes; anything larger is a mistake or an attack.
const maxRequestBody = 8 << 10 // 8 KiB

// replayHeader marks a response that returned an existing transfer rather than
// creating one, so clients and log analysis can see retries without parsing the
// body.
const replayHeader = "Idempotent-Replay"

// Transfers serves the transfer endpoints.
type Transfers struct {
	service *service.Transfers
	logger  *slog.Logger
}

// NewTransfers returns a handler backed by svc. A nil logger disables logging.
func NewTransfers(svc *service.Transfers, logger *slog.Logger) *Transfers {
	return &Transfers{service: svc, logger: logger}
}

// Routes returns the handler's routes. The patterns are method-aware, so a
// request with the wrong method gets 405 from the mux rather than reaching any
// handler.
func (h *Transfers) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.create)
	return mux
}

// createTransferRequest is the wire format. Amount is int64 rather than a
// float, so a fractional amount is rejected at decode instead of being
// silently truncated.
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// transferResponse is returned for every outcome that produced a transfer,
// success or failure, so a client parses one shape. State is the authoritative
// answer: a client never has to read the status code to learn what happened to
// its money, which matters because the status code is the part most likely to
// be rewritten by a proxy.
type transferResponse struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	State          string    `json:"state"`
	FailureReason  *string   `json:"failureReason"`
	CreatedAt      time.Time `json:"createdAt"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Field names the offending request field, for validation failures only.
	Field string `json:"field,omitempty"`
}

func (h *Transfers) create(w http.ResponseWriter, r *http.Request) {
	request, err := decodeRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), "")
		return
	}

	result, err := h.service.Create(r.Context(), request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	if result.Replayed {
		w.Header().Set(replayHeader, "true")
	}

	writeJSON(w, statusFor(result), newTransferResponse(result.Transfer))
}

// decodeRequest reads and validates the shape of the body. Rules about what a
// transfer may contain belong to the domain; this only enforces that the bytes
// are a well-formed request object.
func decodeRequest(w http.ResponseWriter, r *http.Request) (domain.TransferRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	decoder := json.NewDecoder(r.Body)

	// A misspelled field would otherwise be ignored, and "ammount" would send a
	// transfer of zero.
	decoder.DisallowUnknownFields()

	var body createTransferRequest
	if err := decoder.Decode(&body); err != nil {
		return domain.TransferRequest{}, err
	}

	// Reject trailing content, so a body of two concatenated objects cannot
	// have its second half silently dropped.
	if err := decoder.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return domain.TransferRequest{}, errors.New("body must contain a single JSON object")
	}

	return domain.TransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	}, nil
}

// statusFor maps a completed transfer to its status code.
//
// A failure is 422 whether it happened now or on an earlier request, because
// the failure is the answer and reporting it as 200 would invite a client to
// read a failed transfer as a successful one. A success replayed is 200 rather
// than 201 because nothing was created on this call.
func statusFor(result service.Result) int {
	switch {
	case result.Transfer.State == domain.StateFailed:
		return http.StatusUnprocessableEntity
	case result.Replayed:
		return http.StatusOK
	default:
		return http.StatusCreated
	}
}

func (h *Transfers) writeServiceError(w http.ResponseWriter, err error) {
	var notFound *domain.WalletNotFoundError
	var invalid *domain.ValidationError

	switch {
	case errors.As(err, &notFound):
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", notFound.Error(), "")
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", invalid.Error(), invalid.Field)
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", err.Error(), "")
	case errors.Is(err, domain.ErrWalletBusy):
		// The request never started, so retrying it unchanged is safe.
		writeError(w, http.StatusServiceUnavailable, "WALLET_BUSY", err.Error(), "")
	default:
		// Nothing anticipated. Log the detail and tell the client nothing about
		// the internals.
		if h.logger != nil {
			h.logger.Error("unhandled transfer failure", slog.String("error", err.Error()))
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", "")
	}
}

func newTransferResponse(transfer domain.Transfer) transferResponse {
	response := transferResponse{
		ID:             transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		State:          string(transfer.State),
		CreatedAt:      transfer.CreatedAt,
	}

	// null rather than "" when there is no failure, so the field's absence of a
	// value is explicit.
	if transfer.FailureReason != "" {
		reason := transfer.FailureReason
		response.FailureReason = &reason
	}

	return response
}

func writeError(w http.ResponseWriter, status int, code, message, field string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message, Field: field}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// The status line is already sent, so a failure here cannot be reported to
	// the client; the connection simply ends short.
	_ = json.NewEncoder(w).Encode(body)
}
