package domain

import (
	"errors"
	"math"
	"testing"
)

func TestTransitions(t *testing.T) {
	tests := []struct {
		from, to TransferStatus
		allowed  bool
	}{
		{StatusPending, StatusProcessed, true},
		{StatusPending, StatusFailed, true},
		{StatusPending, StatusPending, false},
		{StatusProcessed, StatusFailed, false},
		{StatusProcessed, StatusPending, false},
		{StatusProcessed, StatusProcessed, false},
		{StatusFailed, StatusProcessed, false},
		{StatusFailed, StatusPending, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if got := CanTransition(tc.from, tc.to); got != tc.allowed {
				t.Fatalf("CanTransition = %v, want %v", got, tc.allowed)
			}
			err := AssertTransition(tc.from, tc.to)
			if tc.allowed && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.allowed && !errors.Is(err, ErrInvalidStateTransition) {
				t.Fatalf("want ErrInvalidStateTransition, got %v", err)
			}
		})
	}
}

func TestIsFinal(t *testing.T) {
	if StatusPending.IsFinal() {
		t.Fatal("PENDING must not be final")
	}
	if !StatusProcessed.IsFinal() || !StatusFailed.IsFinal() {
		t.Fatal("PROCESSED and FAILED must be final")
	}
}

func TestTransferCommandValidate(t *testing.T) {
	valid := TransferCommand{IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}

	tests := map[string]func(c *TransferCommand){
		"empty key":       func(c *TransferCommand) { c.IdempotencyKey = "  " },
		"empty from":      func(c *TransferCommand) { c.FromWalletID = "" },
		"empty to":        func(c *TransferCommand) { c.ToWalletID = "" },
		"same wallet":     func(c *TransferCommand) { c.ToWalletID = "w1" },
		"zero amount":     func(c *TransferCommand) { c.Amount = 0 },
		"negative amount": func(c *TransferCommand) { c.Amount = -5 },
		"huge amount":     func(c *TransferCommand) { c.Amount = math.MaxInt64 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cmd := valid
			mutate(&cmd)
			if err := cmd.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}
