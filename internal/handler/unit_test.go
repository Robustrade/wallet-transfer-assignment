package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"wallet-transfer/internal/domain"
)

func TestStatusForError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{domain.ErrEmptyIdempotencyKey, http.StatusBadRequest},
		{domain.ErrInvalidAmount, http.StatusBadRequest},
		{domain.ErrSameWallet, http.StatusBadRequest},
		{domain.ErrWalletNotFound, http.StatusUnprocessableEntity},
		{domain.ErrWalletInactive, http.StatusUnprocessableEntity},
		{domain.ErrInsufficientFunds, http.StatusUnprocessableEntity},
		{domain.ErrTransferNotFound, http.StatusNotFound},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		if got := statusForError(c.err); got != c.want {
			t.Fatalf("statusForError(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestToTransferResponse(t *testing.T) {
	reason := "insufficient funds"
	tr := domain.Transfer{
		ID:             uuid.New(),
		IdempotencyKey: "abc",
		FromWalletID:   uuid.New(),
		ToWalletID:     uuid.New(),
		Amount:         decimal.RequireFromString("12.5"),
		State:          domain.StateFailed,
		FailureReason:  &reason,
	}
	resp := toTransferResponse(tr)
	if resp.ID != tr.ID.String() {
		t.Fatalf("ID = %s", resp.ID)
	}
	if resp.State != "FAILED" {
		t.Fatalf("State = %s", resp.State)
	}
	if resp.FailureReason == nil || *resp.FailureReason != reason {
		t.Fatalf("FailureReason = %v", resp.FailureReason)
	}
	if !resp.Amount.Equal(tr.Amount) {
		t.Fatalf("Amount = %s", resp.Amount)
	}
}

func TestToTransferResponse_ProcessingShownAsPending(t *testing.T) {
	tr := domain.Transfer{ID: uuid.New(), FromWalletID: uuid.New(), ToWalletID: uuid.New(),
		Amount: decimal.RequireFromString("1"), State: domain.StateProcessing}
	if got := toTransferResponse(tr).State; got != "PENDING" {
		t.Fatalf("PROCESSING should be shown as PENDING, got %s", got)
	}
}
