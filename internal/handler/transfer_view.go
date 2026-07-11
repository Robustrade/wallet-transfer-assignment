package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
)

// toTransferView builds the one shared response representation used by
// both POST /transfers and GET /transfers/{id} — a transfers row never
// changes after insert, so there's nothing for the two to diverge on.
func toTransferView(t *domain.Transfer) TransferView {
	view := TransferView{
		ID:           strconv.FormatInt(t.ID, 10),
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount.StringFixed(2),
		Status:       string(t.Status),
		CreatedAt:    t.CreatedAt.Format(time.RFC3339),
	}
	if t.FailureReason != nil {
		reason := string(*t.FailureReason)
		view.FailureReason = &reason
	}
	return view
}

// statusForTransfer derives POST /transfers' HTTP status from a
// transfer's outcome. GET never calls this — a found row is always 200
// there, since the URI addresses a specific resource and the body's
// status field is what carries the real outcome.
func statusForTransfer(t *domain.Transfer) int {
	if t.Status == domain.StatusProcessed {
		return http.StatusOK
	}
	if t.FailureReason != nil {
		return domainErrorHTTPStatus(*t.FailureReason)
	}
	return http.StatusInternalServerError
}

// domainErrorHTTPStatus maps a domain.ErrorCode to its HTTP status,
// shared by both live-rejection errors and persisted FailureReason
// values so the two paths can never disagree on what a code means.
func domainErrorHTTPStatus(code domain.ErrorCode) int {
	switch code {
	case domain.ErrCodeIdempotencyKeyReused:
		return http.StatusConflict
	case domain.ErrCodeAmountUnrepresentable:
		// Tier-1 structural failure (the client's amount doesn't fit in
		// an int64 at all), never a persisted FailureReason — this case
		// only fires via the live-rejection path.
		return http.StatusBadRequest
	case domain.ErrCodeTransferInitFailed:
		// Infrastructure failure (e.g. ID-generation clock regression),
		// not a client mistake or a persisted FailureReason.
		return http.StatusInternalServerError
	default:
		return http.StatusUnprocessableEntity
	}
}
