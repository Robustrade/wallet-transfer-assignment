// Package handler maps HTTP requests to service calls and service results to
// HTTP responses. It holds no business logic.
package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// timeLayout renders timestamps as RFC 3339 in UTC with microseconds, the same
// precision the database stores.
const timeLayout = "2006-01-02T15:04:05.000000Z"

// ---- request --------------------------------------------------------------

// createTransferRequest mirrors the JSON body. Pointers tell "missing or
// null" (nil) apart from "empty string". Amount is kept raw, so we can insist
// on a JSON integer: encoding/json would otherwise accept 1e2, and a
// json.Number field would even accept the string "100".
type createTransferRequest struct {
	IdempotencyKey *string         `json:"idempotencyKey"`
	FromWalletID   *string         `json:"fromWalletId"`
	ToWalletID     *string         `json:"toWalletId"`
	Amount         json.RawMessage `json:"amount"`
}

// jsonInteger is the JSON grammar for an integer: no fraction, no exponent,
// no leading zeros.
var jsonInteger = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// errPayloadTooLarge is returned when the body exceeds MAX_BODY_BYTES while
// being read.
var errPayloadTooLarge = errors.New("payload too large")

// decodeCreateTransfer turns the HTTP request into a validated command. Every
// failure is a *service.ValidationError (400), except errPayloadTooLarge (413).
func decodeCreateTransfer(r *http.Request) (service.CreateTransferCommand, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return service.CreateTransferCommand{}, invalid("Content-Type", "must be application/json")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return service.CreateTransferCommand{}, errPayloadTooLarge
		}
		return service.CreateTransferCommand{}, invalid("body", "could not be read")
	}

	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return service.CreateTransferCommand{}, invalid("body", "is required")
	}
	// Rejects arrays, null, strings and numbers up front with a clear message.
	if body[0] != '{' {
		return service.CreateTransferCommand{}, invalid("body", "must be a JSON object")
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var req createTransferRequest
	if err := dec.Decode(&req); err != nil {
		return service.CreateTransferCommand{}, decodeError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return service.CreateTransferCommand{}, invalid("body", "must contain a single JSON object")
	}

	// Required fields. Their content is checked by cmd.Validate below.
	switch {
	case req.IdempotencyKey == nil:
		return service.CreateTransferCommand{}, invalid("idempotencyKey", "is required")
	case req.FromWalletID == nil:
		return service.CreateTransferCommand{}, invalid("fromWalletId", "is required")
	case req.ToWalletID == nil:
		return service.CreateTransferCommand{}, invalid("toWalletId", "is required")
	}
	amount, err := parseAmount(req.Amount)
	if err != nil {
		return service.CreateTransferCommand{}, err
	}

	cmd := service.CreateTransferCommand{
		IdempotencyKey: *req.IdempotencyKey,
		FromWalletID:   *req.FromWalletID,
		ToWalletID:     *req.ToWalletID,
		Amount:         amount,
	}
	// The same rules the service applies. Checking here avoids a
	// transaction for requests that can never succeed.
	if err := cmd.Validate(); err != nil {
		return service.CreateTransferCommand{}, err
	}
	return cmd, nil
}

func parseAmount(raw json.RawMessage) (int64, error) {
	s := string(bytes.TrimSpace(raw))
	if s == "" || s == "null" {
		return 0, invalid("amount", "is required")
	}
	if !jsonInteger.MatchString(s) {
		return 0, invalid("amount", "must be an integer number of minor units")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil { // out of int64 range
		return 0, invalid("amount", fmt.Sprintf("must be at most %d", service.MaxAmount))
	}
	return n, nil
}

// decodeError converts encoding/json errors into client-facing messages.
func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		return invalid(typeErr.Field, "must be a "+typeErr.Type.String())
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return invalid("body", "is not valid JSON")
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		// encoding/json has no typed error for this case.
		return invalid("body", "contains "+strings.TrimPrefix(err.Error(), "json: "))
	default:
		return invalid("body", "is not valid JSON")
	}
}

func invalid(field, message string) error {
	return &service.ValidationError{Field: field, Message: message}
}

// ---- responses ------------------------------------------------------------

// transferResponse is the body for every transfer outcome, including replays.
type transferResponse struct {
	ID             string  `json:"id"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Status         string  `json:"status"`
	FailureReason  *string `json:"failureReason"` // null unless FAILED
	CreatedAt      string  `json:"createdAt"`
}

func toTransferResponse(t domain.Transfer) transferResponse {
	resp := transferResponse{
		ID:             t.ID.String(),
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		CreatedAt:      formatTime(t.CreatedAt),
	}
	if t.FailureReason != "" {
		reason := string(t.FailureReason)
		resp.FailureReason = &reason
	}
	return resp
}

type walletResponse struct {
	ID        string `json:"id"`
	Balance   int64  `json:"balance"`
	Currency  string `json:"currency"`
	UpdatedAt string `json:"updatedAt"`
}

func toWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{ID: w.ID, Balance: w.Balance, Currency: w.Currency, UpdatedAt: formatTime(w.UpdatedAt)}
}

type transferListResponse struct {
	Transfers []transferResponse `json:"transfers"`
}

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }
