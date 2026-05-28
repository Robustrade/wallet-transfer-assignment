package domain

import "testing"

// TestTransferState_CanTransitionTo locks down the state machine.
//
// The valid edges are:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// Every other combination — including any move FROM a terminal state
// (PROCESSED, FAILED) and self-loops — must be rejected. The table
// enumerates every combination so a future change to the state set is
// forced to either pass this test or update it deliberately.
func TestTransferState_CanTransitionTo(t *testing.T) {
	cases := []struct {
		name string
		from TransferState
		to   TransferState
		want bool
	}{
		// The two legal forward edges.
		{"pending->processed", StatePending, StateProcessed, true},
		{"pending->failed", StatePending, StateFailed, true},
		// Pending must not loop on itself.
		{"pending->pending", StatePending, StatePending, false},
		// PROCESSED is terminal — no outgoing edges.
		{"processed->failed", StateProcessed, StateFailed, false},
		{"processed->pending", StateProcessed, StatePending, false},
		// FAILED is terminal — no outgoing edges.
		{"failed->processed", StateFailed, StateProcessed, false},
		{"failed->pending", StateFailed, StatePending, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.from.CanTransitionTo(c.to); got != c.want {
				t.Fatalf("(%s -> %s) got %v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

// TestTransferState_Valid checks that the three known states are
// valid and that an arbitrary other string is not. This is a
// defence-in-depth check for code paths that round-trip a state value
// through external input — service code never trusts a string from
// outside the package as a TransferState without first running it
// through Valid().
func TestTransferState_Valid(t *testing.T) {
	for _, s := range []TransferState{StatePending, StateProcessed, StateFailed} {
		if !s.Valid() {
			t.Fatalf("expected %s to be valid", s)
		}
	}
	if TransferState("BOGUS").Valid() {
		t.Fatal("BOGUS should not be valid")
	}
}
