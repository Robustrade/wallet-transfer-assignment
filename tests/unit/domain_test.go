package unit

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func TestValidateAmount(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		wantErr bool
	}{
		{"positive amount 100 paise", 100, false},
		{"positive amount 1 paise", 1, false},
		{"large valid amount", 100000000000, false},
		{"zero amount", 0, true},
		{"negative amount", -100, true},
		{"overflow risk amount", 1 << 62, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateAmount(tt.amount)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAmount(%d) error = %v, wantErr %v", tt.amount, err, tt.wantErr)
			}
		})
	}
}

func TestFormatPaise(t *testing.T) {
	tests := []struct {
		paise    int64
		expected string
	}{
		{100, "₹1.00"},
		{10050, "₹100.50"},
		{50, "₹0.50"},
		{0, "₹0.00"},
		{1000000, "₹10000.00"},
		{-10050, "-₹100.50"},
		{-1 << 63, "-₹92233720368547758.08"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := domain.FormatPaise(tt.paise)
			if got != tt.expected {
				t.Errorf("FormatPaise(%d) = %s; want %s", tt.paise, got, tt.expected)
			}
		})
	}
}

func TestComputeRequestHash(t *testing.T) {
	// Deterministic hashing verification
	hash1 := service.ComputeRequestHash("wallet_1", "wallet_2", 5000)
	hash2 := service.ComputeRequestHash("wallet_1", "wallet_2", 5000)
	if hash1 != hash2 {
		t.Errorf("ComputeRequestHash is not deterministic: %s != %s", hash1, hash2)
	}

	// Change amount -> different hash
	hash3 := service.ComputeRequestHash("wallet_1", "wallet_2", 5001)
	if hash1 == hash3 {
		t.Errorf("ComputeRequestHash produced duplicate hash for different amounts")
	}

	// Swap wallets -> different hash
	hash4 := service.ComputeRequestHash("wallet_2", "wallet_1", 5000)
	if hash1 == hash4 {
		t.Errorf("ComputeRequestHash produced duplicate hash for swapped wallets")
	}

	// Delimiter-containing IDs must not create ambiguous fingerprints.
	ambiguous1 := service.ComputeRequestHash("a:b", "c", 100)
	ambiguous2 := service.ComputeRequestHash("a", "b:c", 100)
	if ambiguous1 == ambiguous2 {
		t.Errorf("ComputeRequestHash collided for different wallet ID boundaries")
	}
}
