package domain

import (
	"errors"
	"strings"
	"testing"
)

func validRequest() TransferRequest {
	return TransferRequest{
		IdempotencyKey: "request-001",
		FromWalletID:   "wallet-A",
		ToWalletID:     "wallet-B",
		Amount:         100,
	}
}

func TestTransferRequestStructure(t *testing.T) {
	tests := []struct {
		name string
		edit func(*TransferRequest)
		want error
	}{
		{"valid", func(*TransferRequest) {}, nil},
		{"empty key", func(r *TransferRequest) { r.IdempotencyKey = "" }, ErrInvalidIdempotencyKey},
		{"whitespace key", func(r *TransferRequest) { r.IdempotencyKey = "  " }, ErrInvalidIdempotencyKey},
		{"key padded with spaces", func(r *TransferRequest) { r.IdempotencyKey = " key " }, ErrInvalidIdempotencyKey},
		{"key containing NUL", func(r *TransferRequest) { r.IdempotencyKey = "bad\x00key" }, ErrInvalidIdempotencyKey},
		{"wallet ID containing NUL", func(r *TransferRequest) { r.ToWalletID = "bad\x00wallet" }, ErrInvalidWalletID},
		{"key too long", func(r *TransferRequest) { r.IdempotencyKey = strings.Repeat("a", 129) }, ErrInvalidIdempotencyKey},
		{"invalid UTF-8 key", func(r *TransferRequest) { r.IdempotencyKey = string([]byte{0xff}) }, ErrInvalidIdempotencyKey},
		{"invalid UTF-8 wallet ID", func(r *TransferRequest) { r.ToWalletID = string([]byte{0xff}) }, ErrInvalidWalletID},
		{"missing sender", func(r *TransferRequest) { r.FromWalletID = "" }, ErrInvalidWalletID},
		{"missing recipient", func(r *TransferRequest) { r.ToWalletID = "" }, ErrInvalidWalletID},
		{"same wallet", func(r *TransferRequest) { r.ToWalletID = r.FromWalletID }, ErrSameWallet},
		{"zero amount", func(r *TransferRequest) { r.Amount = 0 }, ErrInvalidAmount},
		{"negative amount", func(r *TransferRequest) { r.Amount = -5 }, ErrInvalidAmount},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			tc.edit(&r)
			if err := r.ValidateStructure(); !errors.Is(err, tc.want) {
				if tc.want == nil && err == nil {
					return
				}
				t.Fatalf("ValidateStructure() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSameOperation(t *testing.T) {
	a := validRequest()
	if !a.SameOperation(a) {
		t.Fatal("identical requests must match")
	}
	for _, change := range []func(*TransferRequest){
		func(r *TransferRequest) { r.IdempotencyKey = "different" },
		func(r *TransferRequest) { r.FromWalletID = "wallet-C" },
		func(r *TransferRequest) { r.ToWalletID = "nonexistent-wallet" },
		func(r *TransferRequest) { r.Amount++ },
	} {
		other := a
		change(&other)
		if a.SameOperation(other) {
			t.Fatalf("changed payload was incorrectly treated as identical: %+v", other)
		}
	}
}
