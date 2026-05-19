package domain_test

import (
	"testing"

	"github.com/hariharandr/wallet-transfer-assignment/internal/domain"
)

func TestAmountValidation(t *testing.T) {
	if err := domain.ValidateAmount(100); err != nil {
		t.Fatalf("100 should be valid, got %v", err)
	}
	for _, bad := range []int64{0, -1, -999} {
		if err := domain.ValidateAmount(bad); err == nil {
			t.Fatalf("amount %d should be invalid", bad)
		}
	}
}

func TestTransferStateString(t *testing.T) {
	// stringer should give us the db/api text, not the int.
	cases := map[domain.TransferState]string{
		domain.StatePending:   "PENDING",
		domain.StateProcessed: "PROCESSED",
		domain.StateFailed:    "FAILED",
	}
	for st, want := range cases {
		if st.String() != want {
			t.Fatalf("state %d String() = %q, want %q", st, st.String(), want)
		}
	}
}

func TestTransferStateTransitions(t *testing.T) {
	if !domain.StatePending.CanTransition(domain.StateProcessed) {
		t.Fatal("PENDING -> PROCESSED must be allowed")
	}
	if !domain.StatePending.CanTransition(domain.StateFailed) {
		t.Fatal("PENDING -> FAILED must be allowed")
	}
	// terminal states are dead ends, no going back.
	if domain.StateProcessed.CanTransition(domain.StatePending) {
		t.Fatal("PROCESSED -> PENDING must not be allowed")
	}
	if domain.StateProcessed.CanTransition(domain.StateFailed) {
		t.Fatal("PROCESSED -> FAILED must not be allowed")
	}
}

func TestEnumSQLRoundTrip(t *testing.T) {
	// value goes to db as text, scan reads it back to the same enum.
	v, err := domain.StateProcessed.Value()
	if err != nil {
		t.Fatalf("Value(): %v", err)
	}
	if v != "PROCESSED" {
		t.Fatalf("Value() = %v, want PROCESSED", v)
	}

	var got domain.TransferState
	if err := got.Scan("FAILED"); err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	if got != domain.StateFailed {
		t.Fatalf("Scan gave %v, want StateFailed", got)
	}

	if err := got.Scan("NONSENSE"); err == nil {
		t.Fatal("scanning an unknown status should error")
	}
}

func TestEntryTypeString(t *testing.T) {
	if domain.Debit.String() != "DEBIT" || domain.Credit.String() != "CREDIT" {
		t.Fatalf("entry type strings wrong: %q %q", domain.Debit, domain.Credit)
	}
}
