package domain_test

import (
	"testing"

	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestComputeRequestHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		fromWalletID string
		toWalletID   string
		amount       int64
		wantMatch    bool
	}{
		{
			name:         "identical parameters yield identical hash",
			fromWalletID: "w1",
			toWalletID:   "w2",
			amount:       1000,
			wantMatch:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			hash1 := domain.ComputeRequestHash(tt.fromWalletID, tt.toWalletID, tt.amount)
			hash2 := domain.ComputeRequestHash(tt.fromWalletID, tt.toWalletID, tt.amount)

			is.NotEmpty(hash1)
			is.Equal(hash1, hash2)

			diffHash := domain.ComputeRequestHash(tt.fromWalletID, tt.toWalletID, tt.amount+1)
			is.NotEqual(hash1, diffHash)
		})
	}
}
