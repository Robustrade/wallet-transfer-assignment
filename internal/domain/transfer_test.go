package domain

import (
	"errors"
	"strings"
	"testing"
	appErrors "wallet-transfer-assignment/internal/appErrors"
)

func TestTransferRequestValidate(t *testing.T) {
	valid := TransferRequest{IdempotencyKey: "key:1.v2-test", FromWalletID: "wallet_1", ToWalletID: "wallet-2", Amount: 1000}
	if err := valid.Validate(1000); err != nil {
		t.Fatalf("valid request at amount limit rejected: %v", err)
	}

	tests := map[string]func(*TransferRequest){
		"empty key":       func(r *TransferRequest) { r.IdempotencyKey = "" },
		"long key":        func(r *TransferRequest) { r.IdempotencyKey = strings.Repeat("a", 129) },
		"blank key":       func(r *TransferRequest) { r.IdempotencyKey = "  " },
		"empty wallet":    func(r *TransferRequest) { r.FromWalletID = "" },
		"long wallet":     func(r *TransferRequest) { r.ToWalletID = strings.Repeat("a", 129) },
		"same wallets":    func(r *TransferRequest) { r.ToWalletID = r.FromWalletID },
		"zero amount":     func(r *TransferRequest) { r.Amount = 0 },
		"negative amount": func(r *TransferRequest) { r.Amount = -1 },
		"above limit":     func(r *TransferRequest) { r.Amount = 1001 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if err := request.Validate(1000); !errors.Is(err, appErrors.ErrInvalidTransfer) {
				t.Fatalf("want appErrors.ErrInvalidTransfer, got %v", err)
			}
		})
	}
}

func TestIdentifiersHaveNoCharacterWhitelist(t *testing.T) {
	request := TransferRequest{
		IdempotencyKey: "550e8400-e29b-41d4-a716-446655440000",
		FromWalletID:   "wallet.1",
		ToWalletID:     strings.Repeat("é", 128),
		Amount:         100,
	}
	if err := request.Validate(1000); err != nil {
		t.Fatalf("nonblank identifiers within database length rejected: %v", err)
	}
}

func TestRequestHash(t *testing.T) {
	original := TransferRequest{IdempotencyKey: "first", FromWalletID: "wallet_a", ToWalletID: "wallet_b", Amount: 100}
	tests := []struct {
		name   string
		mutate func(*TransferRequest)
		same   bool
	}{
		{"different key", func(r *TransferRequest) { r.IdempotencyKey = "second" }, true},
		{"different amount", func(r *TransferRequest) { r.Amount++ }, false},
		{"different source", func(r *TransferRequest) { r.FromWalletID = "wallet_c" }, false},
		{"different destination", func(r *TransferRequest) { r.ToWalletID = "wallet_c" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := original
			tc.mutate(&request)
			if same := request.Hash() == original.Hash(); same != tc.same {
				t.Fatalf("same hash = %v, want %v", same, tc.same)
			}
		})
	}
}

func TestTransferTransitions(t *testing.T) {
	tests := []struct {
		from, to TransferStatus
		want     error
	}{
		{Pending, Processed, nil},
		{Pending, Failed, nil},
		{Pending, Pending, appErrors.ErrInvalidTransition},
		{Processed, Pending, appErrors.ErrInvalidTransition},
		{Processed, Processed, appErrors.ErrInvalidTransition},
		{Processed, Failed, appErrors.ErrInvalidTransition},
		{Failed, Pending, appErrors.ErrInvalidTransition},
		{Failed, Processed, appErrors.ErrInvalidTransition},
		{Failed, Failed, appErrors.ErrInvalidTransition},
		{"UNKNOWN", Processed, appErrors.ErrInvalidTransition},
		{Pending, "UNKNOWN", appErrors.ErrInvalidTransition},
	}

	for _, tc := range tests {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			transfer := Transfer{Status: tc.from}
			if err := transfer.Transition(tc.to); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			wantStatus := tc.from
			if tc.want == nil {
				wantStatus = tc.to
			}
			if transfer.Status != wantStatus {
				t.Fatalf("status = %s, want %s", transfer.Status, wantStatus)
			}
		})
	}
}
