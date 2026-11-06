package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// integerLiteral matches a plain JSON integer: no fraction, no exponent, not quoted.
var integerLiteral = regexp.MustCompile(`^-?[0-9]+$`)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	// Raw so that floats, exponents, strings and int64 overflow are rejected
	// explicitly instead of being silently coerced.
	Amount json.RawMessage `json:"amount"`
}

func (r createTransferRequest) toDomain() (domain.TransferRequest, error) {
	if len(r.Amount) == 0 {
		return domain.TransferRequest{}, badRequest("amount: is required")
	}
	raw := string(r.Amount)
	if !integerLiteral.MatchString(raw) {
		return domain.TransferRequest{}, badRequest("amount: must be an integer in minor units")
	}
	amount, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return domain.TransferRequest{}, badRequest("amount: is out of range")
	}
	return domain.TransferRequest{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         amount,
	}, nil
}

type transferResponse struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         string    `json:"status"`
	FailureReason  *string   `json:"failureReason"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func newTransferResponse(t domain.Transfer) transferResponse {
	resp := transferResponse{
		ID:             t.ID.String(),
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		CreatedAt:      t.CreatedAt.UTC(),
		UpdatedAt:      t.UpdatedAt.UTC(),
	}
	if t.FailureReason != "" {
		reason := string(t.FailureReason)
		resp.FailureReason = &reason
	}
	return resp
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error    errorBody         `json:"error"`
	Transfer *transferResponse `json:"transfer,omitempty"`
}

// decodeJSON strictly decodes a single JSON object from the request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &apiError{status: http.StatusRequestEntityTooLarge, code: "PAYLOAD_TOO_LARGE",
				message: fmt.Sprintf("request body must not exceed %d bytes", maxBodyBytes)}
		}
		return badRequest("request body must be a valid JSON object: " + err.Error())
	}
	if dec.More() {
		return badRequest("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // the client may have gone away; nothing useful to do
}
