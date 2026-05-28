package domain

import (
	"math"
	"testing"
	"time"
)

// TestWallet_ZeroValueIsValid documents that the zero-valued Wallet
// struct is meaningful: empty id, zero balance, zero timestamps.
// Callers (typically the repository scan) populate it field by
// field; this test exists so a future refactor cannot accidentally
// break that pattern.
func TestWallet_ZeroValueIsValid(t *testing.T) {
	var w Wallet
	if w.ID != "" {
		t.Fatalf("zero ID: %q", w.ID)
	}
	if w.Balance != 0 {
		t.Fatalf("zero Balance: %d", w.Balance)
	}
	if !w.CreatedAt.IsZero() {
		t.Fatalf("zero CreatedAt: %v", w.CreatedAt)
	}
}

// TestWallet_AcceptsMaxInt64Balance proves we model balance as
// int64 and have headroom to the platform max. A future refactor
// that switched to int32 (lossy) would fail this test.
func TestWallet_AcceptsMaxInt64Balance(t *testing.T) {
	w := Wallet{ID: "rich", Balance: math.MaxInt64}
	if w.ID != "rich" {
		t.Fatalf("id lost: %q", w.ID)
	}
	if w.Balance != math.MaxInt64 {
		t.Fatalf("balance lost precision: %d", w.Balance)
	}
}

// TestWallet_AllowsLargeAndUnicodeIDs documents that wallet IDs are
// caller-supplied strings — no length check, no character
// restriction. The DB enforces a TEXT max via PG's default, but the
// domain type itself imposes nothing.
func TestWallet_AllowsLargeAndUnicodeIDs(t *testing.T) {
	cases := []string{
		"短いID",
		"emoji-🪙-id",
		"with spaces",
		"--hyphens--",
		stringOfLen(256, 'x'),
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			w := Wallet{ID: id, Balance: 1}
			if w.ID != id {
				t.Fatalf("id mutated: got %q want %q", w.ID, id)
			}
			if w.Balance != 1 {
				t.Fatalf("balance mutated: got %d", w.Balance)
			}
		})
	}
}

// TestWallet_TimestampsArePreserved confirms the struct does no
// time zone conversion or rounding. Tests rely on this when
// comparing scanned-from-DB timestamps with seeded ones.
func TestWallet_TimestampsArePreserved(t *testing.T) {
	now := time.Date(2024, 1, 2, 3, 4, 5, 678901234, time.UTC)
	w := Wallet{ID: "w", Balance: 42, CreatedAt: now, UpdatedAt: now}
	if w.ID != "w" {
		t.Fatalf("ID lost: %q", w.ID)
	}
	if w.Balance != 42 {
		t.Fatalf("Balance lost: %d", w.Balance)
	}
	if !w.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt drift: %v", w.CreatedAt)
	}
	if !w.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt drift: %v", w.UpdatedAt)
	}
	if w.CreatedAt.Nanosecond() != 678901234 {
		t.Fatalf("nanos lost: %d", w.CreatedAt.Nanosecond())
	}
}

func stringOfLen(n int, b byte) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}
