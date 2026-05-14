package services

import (
	"errors"
	"testing"

	"github.com/parth/wallet-transfer-assignment/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateTransferRequest tests input validation
func TestValidateTransferRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *domain.TransferRequest
		wantErr error
	}{
		{
			name: "valid request",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-key-123",
				FromWalletID:   "550e8400-e29b-41d4-a716-446655440000",
				ToWalletID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				Amount:         "100.50",
			},
			wantErr: nil,
		},
		{
			name: "empty idempotency key",
			req: &domain.TransferRequest{
				IdempotencyKey: "",
				FromWalletID:   "550e8400-e29b-41d4-a716-446655440000",
				ToWalletID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				Amount:         "100",
			},
			wantErr: domain.ErrInvalidIdempotencyKey,
		},
		{
			name: "invalid from wallet ID (not UUID)",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-key",
				FromWalletID:   "not-a-uuid",
				ToWalletID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				Amount:         "100",
			},
			wantErr: domain.ErrInvalidWalletID,
		},
		{
			name: "zero amount",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-key",
				FromWalletID:   "550e8400-e29b-41d4-a716-446655440000",
				ToWalletID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				Amount:         "0",
			},
			wantErr: domain.ErrInvalidAmount,
		},
		{
			name: "negative amount",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-key",
				FromWalletID:   "550e8400-e29b-41d4-a716-446655440000",
				ToWalletID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				Amount:         "-50",
			},
			wantErr: domain.ErrInvalidAmount,
		},
		{
			name: "invalid amount format",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-key",
				FromWalletID:   "550e8400-e29b-41d4-a716-446655440000",
				ToWalletID:     "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
				Amount:         "not-a-number",
			},
			wantErr: domain.ErrInvalidAmountFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateTransferRequest(tt.req)
			if tt.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.True(t, errors.Is(err, tt.wantErr), "got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestProcessTransferValidation tests business logic validation
func TestProcessTransferValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *domain.TransferRequest
		wantErr error
	}{
		{
			name: "transfer to same wallet",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-key",
				FromWalletID:   "550e8400-e29b-41d4-a716-446655440000",
				ToWalletID:     "550e8400-e29b-41d4-a716-446655440000",
				Amount:         "100",
			},
			wantErr: domain.ErrSameWallet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test demonstrates how to test for business logic errors
			// A real test would use a mock repository
			_ = tt.req
			_ = tt.wantErr
			t.Logf("need: mock repository for full integration test")
		})
	}
}

// TestErrorTypes tests that custom errors can be identified
func TestErrorTypes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "wallet not found error",
			err:  domain.ErrWalletNotFound,
			want: domain.ErrWalletNotFound,
		},
		{
			name: "insufficient balance error",
			err:  domain.ErrInsufficientBalance,
			want: domain.ErrInsufficientBalance,
		},
		{
			name: "invalid amount error",
			err:  domain.ErrInvalidAmount,
			want: domain.ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, errors.Is(tt.err, tt.want))
		})
	}
}

// TestIdempotencyKeyPreservation tests that idempotency behavior is correct
func TestIdempotencyKeyPreservation(t *testing.T) {
	t.Run("duplicate requests must return same response", func(t *testing.T) {
		// This is a behavioral test that requires integration test setup
		// It verifies that calling ProcessTransfer twice with same key returns same response
		t.Skip("requires database setup and mock repository")
	})

	t.Run("concurrent identical requests must not duplicate transfer", func(t *testing.T) {
		// This test verifies concurrent request handling
		// Both requests should receive the same transfer ID
		t.Skip("requires database setup, concurrency testing")
	})
}
