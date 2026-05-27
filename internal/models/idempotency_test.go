package models

import (
	"testing"
)

func TestGenerateRequestHash(t *testing.T) {
	tests := []struct {
		name         string
		fromWalletID string
		toWalletID   string
		amount       float64
	}{
		{"test 1", "wallet_1", "wallet_2", 100.0},
		{"test 2", "wallet_1", "wallet_2", 100.0}, // Same as test 1
		{"test 3", "wallet_1", "wallet_2", 200.0}, // Different amount
		{"test 4", "wallet_2", "wallet_1", 100.0}, // Swapped wallets
	}

	hash1 := GenerateRequestHash(tests[0].fromWalletID, tests[0].toWalletID, tests[0].amount)
	hash2 := GenerateRequestHash(tests[1].fromWalletID, tests[1].toWalletID, tests[1].amount)
	hash3 := GenerateRequestHash(tests[2].fromWalletID, tests[2].toWalletID, tests[2].amount)
	hash4 := GenerateRequestHash(tests[3].fromWalletID, tests[3].toWalletID, tests[3].amount)

	// Same input should produce same hash
	if hash1 != hash2 {
		t.Errorf("Same inputs produced different hashes: %s != %s", hash1, hash2)
	}

	// Different amount should produce different hash
	if hash1 == hash3 {
		t.Error("Different amounts produced same hash")
	}

	// Swapped wallets should produce different hash
	if hash1 == hash4 {
		t.Error("Swapped wallets produced same hash")
	}

	// Hash should be 64 characters (SHA256 hex)
	if len(hash1) != 64 {
		t.Errorf("Hash length = %d, want 64", len(hash1))
	}
}

func TestIdempotencyRecord_ValidateRequestHash(t *testing.T) {
	correctHash := GenerateRequestHash("wallet_1", "wallet_2", 100.0)
	wrongHash := GenerateRequestHash("wallet_1", "wallet_2", 200.0)

	record := &IdempotencyRecord{
		IdempotencyKey: "key_1",
		RequestHash:    correctHash,
		Status:         IdempotencyStatusProcessing,
	}

	t.Run("matching hash", func(t *testing.T) {
		err := record.ValidateRequestHash(correctHash)
		if err != nil {
			t.Errorf("ValidateRequestHash() error = %v, want nil", err)
		}
	})

	t.Run("mismatched hash", func(t *testing.T) {
		err := record.ValidateRequestHash(wrongHash)
		if err == nil {
			t.Error("ValidateRequestHash() expected error, got nil")
		}
	})
}

func TestIdempotencyRecord_StatusMethods(t *testing.T) {
	tests := []struct {
		name         string
		status       IdempotencyStatus
		isProcessing bool
		isCompleted  bool
		isFailed     bool
	}{
		{"processing", IdempotencyStatusProcessing, true, false, false},
		{"completed", IdempotencyStatusCompleted, false, true, false},
		{"failed", IdempotencyStatusFailed, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &IdempotencyRecord{Status: tt.status}

			if record.IsProcessing() != tt.isProcessing {
				t.Errorf("IsProcessing() = %v, want %v", record.IsProcessing(), tt.isProcessing)
			}
			if record.IsCompleted() != tt.isCompleted {
				t.Errorf("IsCompleted() = %v, want %v", record.IsCompleted(), tt.isCompleted)
			}
			if record.IsFailed() != tt.isFailed {
				t.Errorf("IsFailed() = %v, want %v", record.IsFailed(), tt.isFailed)
			}
		})
	}
}
