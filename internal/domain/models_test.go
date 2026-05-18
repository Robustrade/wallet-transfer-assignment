package domain

import "testing"

func TestTransferRequestValidate(t *testing.T) {
	valid := &TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request: unexpected error %v", err)
	}

	tests := []struct {
		name    string
		req     *TransferRequest
		wantErr error
	}{
		{
			name: "missing idempotency key",
			req: &TransferRequest{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "missing from wallet",
			req: &TransferRequest{
				IdempotencyKey: "key-2",
				ToWalletID:     "wallet_2",
				Amount:         100,
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "missing to wallet",
			req: &TransferRequest{
				IdempotencyKey: "key-3",
				FromWalletID:   "wallet_1",
				Amount:         100,
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "zero amount",
			req: &TransferRequest{
				IdempotencyKey: "key-4",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         0,
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "negative amount",
			req: &TransferRequest{
				IdempotencyKey: "key-5",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         -50,
			},
			wantErr: ErrInvalidAmount,
		},
		{
			name: "same wallet",
			req: &TransferRequest{
				IdempotencyKey: "key-6",
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_1",
				Amount:         100,
			},
			wantErr: ErrSameWallet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Errorf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
