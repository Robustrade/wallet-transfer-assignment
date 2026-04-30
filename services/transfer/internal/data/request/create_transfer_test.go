package request_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
)

func TestCreateTransferReq_Validate_OK(t *testing.T) {
	req := request.CreateTransferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	require.NoError(t, req.Validate())
}

func TestCreateTransferReq_Validate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*request.CreateTransferReq)
		wantErr *apperr.AppError
	}{
		{
			name:    "missing idempotency key",
			mutate:  func(r *request.CreateTransferReq) { r.IdempotencyKey = "" },
			wantErr: apperr.ErrInvalidIdempotencyKey,
		},
		{
			name:    "blank idempotency key",
			mutate:  func(r *request.CreateTransferReq) { r.IdempotencyKey = "   " },
			wantErr: apperr.ErrInvalidIdempotencyKey,
		},
		{
			name:    "missing from wallet id",
			mutate:  func(r *request.CreateTransferReq) { r.FromWalletID = "" },
			wantErr: apperr.ErrInvalidWalletID,
		},
		{
			name:    "missing to wallet id",
			mutate:  func(r *request.CreateTransferReq) { r.ToWalletID = "" },
			wantErr: apperr.ErrInvalidWalletID,
		},
		{
			name:    "from wallet id with leading whitespace",
			mutate:  func(r *request.CreateTransferReq) { r.FromWalletID = " wallet_a" },
			wantErr: apperr.ErrInvalidWalletID,
		},
		{
			name:    "to wallet id with trailing whitespace",
			mutate:  func(r *request.CreateTransferReq) { r.ToWalletID = "wallet_b\t" },
			wantErr: apperr.ErrInvalidWalletID,
		},
		{
			name:    "same wallet",
			mutate:  func(r *request.CreateTransferReq) { r.ToWalletID = r.FromWalletID },
			wantErr: apperr.ErrSameWallet,
		},
		{
			name:    "zero amount",
			mutate:  func(r *request.CreateTransferReq) { r.Amount = 0 },
			wantErr: apperr.ErrInvalidAmount,
		},
		{
			name:    "negative amount",
			mutate:  func(r *request.CreateTransferReq) { r.Amount = -1 },
			wantErr: apperr.ErrInvalidAmount,
		},
		{
			name: "idempotency key too long",
			mutate: func(r *request.CreateTransferReq) {
				r.IdempotencyKey = strings.Repeat("k", request.MaxIdempotencyKeyLen+1)
			},
			wantErr: apperr.ErrInvalidIdempotencyKey,
		},
		{
			name: "from wallet id too long",
			mutate: func(r *request.CreateTransferReq) {
				r.FromWalletID = strings.Repeat("a", request.MaxWalletIDLen+1)
			},
			wantErr: apperr.ErrInvalidWalletID,
		},
		{
			name: "to wallet id too long",
			mutate: func(r *request.CreateTransferReq) {
				r.ToWalletID = strings.Repeat("b", request.MaxWalletIDLen+1)
			},
			wantErr: apperr.ErrInvalidWalletID,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := request.CreateTransferReq{
				IdempotencyKey: "k1",
				FromWalletID:   "wallet_a",
				ToWalletID:     "wallet_b",
				Amount:         100,
			}
			tc.mutate(&req)
			err := req.Validate()
			ae, ok := apperr.As(err)
			require.True(t, ok, "expected AppError, got %T: %v", err, err)
			assert.Equal(t, tc.wantErr.Code, ae.Code)
		})
	}
}
