package domain

import "testing"

func TestCreateTransferRequest_Validate(t *testing.T) {
	base := CreateTransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	tests := []struct {
		name    string
		mutate  func(r CreateTransferRequest) CreateTransferRequest
		wantErr error
	}{
		{
			name:    "valid request",
			mutate:  func(r CreateTransferRequest) CreateTransferRequest { return r },
			wantErr: nil,
		},
		{
			name: "missing idempotency key",
			mutate: func(r CreateTransferRequest) CreateTransferRequest {
				r.IdempotencyKey = ""
				return r
			},
			wantErr: ErrEmptyIdempotencyKey,
		},
		{
			name: "missing from wallet",
			mutate: func(r CreateTransferRequest) CreateTransferRequest {
				r.FromWalletID = ""
				return r
			},
			wantErr: ErrEmptyWalletID,
		},
		{
			name: "missing to wallet",
			mutate: func(r CreateTransferRequest) CreateTransferRequest {
				r.ToWalletID = ""
				return r
			},
			wantErr: ErrEmptyWalletID,
		},
		{
			name: "same wallet",
			mutate: func(r CreateTransferRequest) CreateTransferRequest {
				r.ToWalletID = r.FromWalletID
				return r
			},
			wantErr: ErrSameWallet,
		},
		{
			name: "zero amount",
			mutate: func(r CreateTransferRequest) CreateTransferRequest {
				r.Amount = 0
				return r
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "negative amount",
			mutate: func(r CreateTransferRequest) CreateTransferRequest {
				r.Amount = -50
				return r
			},
			wantErr: ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mutate(base).Validate()
			if err != tt.wantErr {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}
