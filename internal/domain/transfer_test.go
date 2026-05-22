package domain

import "testing"

func TestTransferStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		name string
		from TransferStatus
		to   TransferStatus
		want bool
	}{
		{"pending to processed", StatusPending, StatusProcessed, true},
		{"pending to failed", StatusPending, StatusFailed, true},
		{"pending to pending", StatusPending, StatusPending, false},
		// Terminal states have no outgoing transitions: this is what makes the
		// state machine safe under retries and duplicate delivery.
		{"processed is terminal", StatusProcessed, StatusFailed, false},
		{"processed cannot reprocess", StatusProcessed, StatusProcessed, false},
		{"failed is terminal", StatusFailed, StatusProcessed, false},
		{"failed cannot refail", StatusFailed, StatusFailed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
				t.Errorf("%s -> %s: got %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}
