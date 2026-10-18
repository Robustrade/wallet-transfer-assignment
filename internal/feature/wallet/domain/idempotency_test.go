package domain_test

import (
	"errors"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
)

func TestTransferFingerprint(t *testing.T) {
	base, err := domain.TransferFingerprint(1, 2, 100)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("deterministic", func(t *testing.T) {
		again, _ := domain.TransferFingerprint(1, 2, 100)
		if again != base {
			t.Fatalf("same input produced different hashes: %s vs %s", base, again)
		}
	})

	t.Run("fits idempotency_records.request_hash VARCHAR(64)", func(t *testing.T) {
		if len(base) != 64 {
			t.Fatalf("hash length = %d, want 64", len(base))
		}
	})

	t.Run("each field changes the hash", func(t *testing.T) {
		for name, args := range map[string][3]int64{
			"amount":   {1, 2, 101},
			"sender":   {3, 2, 100},
			"receiver": {1, 3, 100},
			"swapped":  {2, 1, 100},
		} {
			got, _ := domain.TransferFingerprint(args[0], args[1], args[2])
			if got == base {
				t.Errorf("changing %s did not change the fingerprint", name)
			}
		}
	})
}

func TestIdempotencyRecordMatchesRequest(t *testing.T) {
	rec := domain.IdempotencyRecord{RequestHash: "abc"}

	if err := rec.MatchesRequest("abc"); err != nil {
		t.Fatalf("same hash rejected: %v", err)
	}
	if err := rec.MatchesRequest("different"); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("got %v, want ErrIdempotencyConflict", err)
	}
}
