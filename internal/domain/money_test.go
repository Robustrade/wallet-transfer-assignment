package domain_test

import (
	"testing"

	"wallet-transfer/internal/domain"
)

func TestCentsToNumeric(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{99, "0.99"},
		{100, "1.00"},
		{10000, "100.00"},
		{100000, "1000.00"},
		{9999, "99.99"},
	}
	for _, tc := range cases {
		got := domain.CentsToNumeric(tc.cents)
		if got != tc.want {
			t.Errorf("CentsToNumeric(%d) = %q, want %q", tc.cents, got, tc.want)
		}
	}
}

func TestNumericToCents(t *testing.T) {
	cases := []struct {
		input string
		want  int64
		isErr bool
	}{
		{"0", 0, false},
		{"0.00", 0, false},
		{"1.00", 100, false},
		{"100", 10000, false},
		{"100.00", 10000, false},
		{"100.50", 10050, false},
		{"99.99", 9999, false},
		{"1000.00", 100000, false},
		{"0.01", 1, false},
		{"0.1", 10, false}, // one decimal digit is fine
		{"1.001", 0, true}, // three decimal digits → error
		{"", 0, true},      // empty
		{"abc", 0, true},   // non-numeric
	}
	for _, tc := range cases {
		got, err := domain.NumericToCents(tc.input)
		if tc.isErr {
			if err == nil {
				t.Errorf("NumericToCents(%q) expected error, got %d", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NumericToCents(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NumericToCents(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	// Any value should survive CentsToNumeric → NumericToCents unchanged.
	for _, cents := range []int64{0, 1, 50, 99, 100, 9999, 10000, 100000, 1234567} {
		numeric := domain.CentsToNumeric(cents)
		back, err := domain.NumericToCents(numeric)
		if err != nil {
			t.Errorf("round-trip(%d): NumericToCents(%q) error: %v", cents, numeric, err)
			continue
		}
		if back != cents {
			t.Errorf("round-trip(%d): got %d via %q", cents, back, numeric)
		}
	}
}

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from domain.TransferStatus
		to   domain.TransferStatus
		want bool
	}{
		{domain.StatusPending, domain.StatusProcessed, true},
		{domain.StatusPending, domain.StatusFailed, true},
		{domain.StatusProcessed, domain.StatusFailed, false},
		{domain.StatusProcessed, domain.StatusPending, false},
		{domain.StatusFailed, domain.StatusProcessed, false},
		{domain.StatusFailed, domain.StatusPending, false},
	}
	for _, tc := range cases {
		got := domain.CanTransition(tc.from, tc.to)
		if got != tc.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
