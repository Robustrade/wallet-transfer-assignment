package domain_test

import (
	"errors"
	"testing"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

func validRequest() domain.TransferRequest {
	return domain.TransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
}

func TestValidateAcceptsAWellFormedRequest(t *testing.T) {
	if err := validRequest().Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestValidateRejectsMalformedRequests(t *testing.T) {
	tests := map[string]struct {
		mutate func(*domain.TransferRequest)
		want   error
	}{
		"missing idempotency key": {
			func(r *domain.TransferRequest) { r.IdempotencyKey = "" },
			domain.ErrMissingIdempotencyKey,
		},
		"oversized idempotency key": {
			func(r *domain.TransferRequest) {
				r.IdempotencyKey = string(make([]byte, domain.MaxIdempotencyKeyLength+1))
			},
			domain.ErrIdempotencyKeyTooLong,
		},
		"missing source wallet": {
			func(r *domain.TransferRequest) { r.FromWalletID = "" },
			domain.ErrMissingFromWallet,
		},
		"missing destination wallet": {
			func(r *domain.TransferRequest) { r.ToWalletID = "" },
			domain.ErrMissingToWallet,
		},
		"transfer to self": {
			func(r *domain.TransferRequest) { r.ToWalletID = r.FromWalletID },
			domain.ErrSameWallet,
		},
		"zero amount": {
			func(r *domain.TransferRequest) { r.Amount = 0 },
			domain.ErrNonPositiveAmount,
		},
		"negative amount": {
			func(r *domain.TransferRequest) { r.Amount = -1 },
			domain.ErrNonPositiveAmount,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := validRequest()
			tc.mutate(&req)

			if err := req.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestKeyAtMaximumLengthIsAccepted(t *testing.T) {
	req := validRequest()
	req.IdempotencyKey = string(make([]byte, domain.MaxIdempotencyKeyLength))

	if err := req.Validate(); err != nil {
		t.Fatalf("key of exactly the maximum length rejected: %v", err)
	}
}

// The hash is what tells a genuine retry apart from a key reused for a
// different transfer, so it must be stable across everything that does not
// change the transfer, and must move for everything that does.
// Guards against anything time-based or random leaking into the fingerprint: a
// hash that varied between calls would turn every legitimate retry into a 409.
func TestHashIsStableForTheSameTransfer(t *testing.T) {
	first := validRequest()
	second := validRequest()

	if first.Hash() != second.Hash() {
		t.Fatal("two identical requests hashed to different values")
	}
}

func TestHashIgnoresTheIdempotencyKey(t *testing.T) {
	other := validRequest()
	other.IdempotencyKey = "a-completely-different-key"

	if validRequest().Hash() != other.Hash() {
		t.Fatal("hash changed with the idempotency key, which it should exclude")
	}
}

func TestHashChangesWithEveryMeaningfulField(t *testing.T) {
	tests := map[string]func(*domain.TransferRequest){
		"source wallet":      func(r *domain.TransferRequest) { r.FromWalletID = "wallet_9" },
		"destination wallet": func(r *domain.TransferRequest) { r.ToWalletID = "wallet_9" },
		"amount":             func(r *domain.TransferRequest) { r.Amount = 101 },
	}

	base := validRequest().Hash()
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			req := validRequest()
			mutate(&req)

			if req.Hash() == base {
				t.Fatalf("hash did not change when %s changed", name)
			}
		})
	}
}

// Wallet ids are opaque TEXT and may contain any character, newlines included.
// Separating fields without also length-prefixing them would encode these two
// distinct transfers identically, and the second caller would be handed the
// first caller's transfer instead of a 409.
func TestHashDistinguishesFieldBoundaries(t *testing.T) {
	first := validRequest()
	first.FromWalletID, first.ToWalletID = "a\nbc", "x"

	second := validRequest()
	second.FromWalletID, second.ToWalletID = "a", "bc\nx"

	if first.Hash() == second.Hash() {
		t.Fatal("different wallet pairs produced the same hash")
	}
}

func TestPendingTransferCanBeProcessed(t *testing.T) {
	transfer := &domain.Transfer{State: domain.StatePending}

	if err := transfer.MarkProcessed(); err != nil {
		t.Fatalf("marking a pending transfer processed: %v", err)
	}
	if transfer.State != domain.StateProcessed {
		t.Fatalf("state is %s, want PROCESSED", transfer.State)
	}
	if transfer.FailureReason != "" {
		t.Fatalf("processed transfer carries a failure reason: %q", transfer.FailureReason)
	}
}

func TestPendingTransferCanBeFailedWithAReason(t *testing.T) {
	transfer := &domain.Transfer{State: domain.StatePending}

	if err := transfer.MarkFailed("insufficient funds"); err != nil {
		t.Fatalf("marking a pending transfer failed: %v", err)
	}
	if transfer.State != domain.StateFailed {
		t.Fatalf("state is %s, want FAILED", transfer.State)
	}
	if transfer.FailureReason != "insufficient funds" {
		t.Fatalf("failure reason is %q, want %q", transfer.FailureReason, "insufficient funds")
	}
}

// The schema enforces that a transfer is FAILED if and only if it carries a
// reason; the domain refuses to build a row that would violate it.
func TestFailingWithoutAReasonIsRejected(t *testing.T) {
	for name, reason := range map[string]string{"empty": "", "blank": "   "} {
		t.Run(name, func(t *testing.T) {
			transfer := &domain.Transfer{State: domain.StatePending}

			if err := transfer.MarkFailed(reason); !errors.Is(err, domain.ErrInvalidStateTransition) {
				t.Fatalf("got %v, want ErrInvalidStateTransition", err)
			}
			if transfer.State != domain.StatePending {
				t.Fatalf("state changed to %s on a rejected transition", transfer.State)
			}
		})
	}
}

// A transfer settles exactly once. Re-running a transition is how a duplicate
// or a retry would try to move money twice.
func TestTerminalTransfersDoNotTransitionAgain(t *testing.T) {
	tests := map[string]domain.State{
		"processed": domain.StateProcessed,
		"failed":    domain.StateFailed,
	}

	for name, state := range tests {
		t.Run(name+" cannot be processed", func(t *testing.T) {
			transfer := &domain.Transfer{State: state, FailureReason: "reason"}

			if err := transfer.MarkProcessed(); !errors.Is(err, domain.ErrInvalidStateTransition) {
				t.Fatalf("got %v, want ErrInvalidStateTransition", err)
			}
			if transfer.State != state {
				t.Fatalf("state changed from %s to %s", state, transfer.State)
			}
		})

		t.Run(name+" cannot be failed", func(t *testing.T) {
			transfer := &domain.Transfer{State: state, FailureReason: "reason"}

			if err := transfer.MarkFailed("another reason"); !errors.Is(err, domain.ErrInvalidStateTransition) {
				t.Fatalf("got %v, want ErrInvalidStateTransition", err)
			}
			if transfer.State != state {
				t.Fatalf("state changed from %s to %s", state, transfer.State)
			}
		})
	}
}

func TestTerminalStates(t *testing.T) {
	tests := map[domain.State]bool{
		domain.StatePending:   false,
		domain.StateProcessed: true,
		domain.StateFailed:    true,
	}

	for state, want := range tests {
		if got := state.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", state, got, want)
		}
	}
}
