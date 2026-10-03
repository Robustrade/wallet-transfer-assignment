package domain_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/domain"
)

func TestNewTransferRequestAcceptsValidInput(t *testing.T) {
	longA := strings.Repeat("a", 255)
	longB := strings.Repeat("b", 255)
	longMultibyteA := strings.Repeat("é", 255) // 255 characters, 510 bytes
	longMultibyteB := strings.Repeat("ü", 255)

	tests := []struct {
		name                  string
		key, from, to, amount string
		wantAmount            int64
	}{
		{"typical request", "key-1", "wallet_1", "wallet_2", "100", 100},
		{"smallest amount", "key-1", "wallet_1", "wallet_2", "1", 1},
		{"largest amount", "key-1", "wallet_1", "wallet_2", "9223372036854775807", math.MaxInt64},
		{"255-character fields", longA, longA, longB, "100", 100},
		{"255 multibyte characters", longMultibyteA, longMultibyteA, longMultibyteB, "100", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NewTransferRequest(tt.key, tt.from, tt.to, tt.amount)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := domain.TransferRequest{
				IdempotencyKey: tt.key,
				FromWalletID:   tt.from,
				ToWalletID:     tt.to,
				Amount:         tt.wantAmount,
			}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestNewTransferRequestRejectsInvalidInput(t *testing.T) {
	tooLong := strings.Repeat("a", 256)
	tooLongMultibyte := strings.Repeat("é", 256)

	tests := []struct {
		name                  string
		key, from, to, amount string
		wantErr               error
	}{
		// Spec §6 step 3: every field is present; strings are 1–255 characters.
		{"missing idempotencyKey", "", "wallet_1", "wallet_2", "100", domain.ErrInvalidField},
		{"missing fromWalletId", "key-1", "", "wallet_2", "100", domain.ErrInvalidField},
		{"missing toWalletId", "key-1", "wallet_1", "", "100", domain.ErrInvalidField},
		{"missing amount", "key-1", "wallet_1", "wallet_2", "", domain.ErrInvalidField},
		{"idempotencyKey too long", tooLong, "wallet_1", "wallet_2", "100", domain.ErrInvalidField},
		{"fromWalletId too long", "key-1", tooLong, "wallet_2", "100", domain.ErrInvalidField},
		{"toWalletId too long", "key-1", "wallet_1", tooLong, "100", domain.ErrInvalidField},
		{"256 multibyte characters", tooLongMultibyte, "wallet_1", "wallet_2", "100", domain.ErrInvalidField},
		{"NUL in idempotencyKey", "key-\x00", "wallet_1", "wallet_2", "100", domain.ErrInvalidField},
		{"NUL in fromWalletId", "key-1", "wallet\x00_1", "wallet_2", "100", domain.ErrInvalidField},
		{"NUL in toWalletId", "key-1", "wallet_1", "wallet\x00_2", "100", domain.ErrInvalidField},

		// Step 4: amount is a JSON integer from 1 to int64 max.
		{"amount as a string", "key-1", "wallet_1", "wallet_2", `"100"`, domain.ErrInvalidAmount},
		{"fractional amount", "key-1", "wallet_1", "wallet_2", "100.5", domain.ErrInvalidAmount},
		{"amount with a decimal point", "key-1", "wallet_1", "wallet_2", "100.0", domain.ErrInvalidAmount},
		{"amount in exponent form", "key-1", "wallet_1", "wallet_2", "1e2", domain.ErrInvalidAmount},
		{"zero amount", "key-1", "wallet_1", "wallet_2", "0", domain.ErrInvalidAmount},
		{"negative amount", "key-1", "wallet_1", "wallet_2", "-1", domain.ErrInvalidAmount},
		{"amount above int64 max", "key-1", "wallet_1", "wallet_2", "9223372036854775808", domain.ErrInvalidAmount},

		// Step 5: the wallets differ.
		{"same wallet", "key-1", "wallet_1", "wallet_1", "100", domain.ErrSameWallet},

		// The earliest failing step decides the error.
		{"missing field before invalid amount", "", "wallet_1", "wallet_2", "1e2", domain.ErrInvalidField},
		{"missing field before same wallet", "key-1", "wallet_1", "wallet_1", "", domain.ErrInvalidField},
		{"invalid amount before same wallet", "key-1", "wallet_1", "wallet_1", "0", domain.ErrInvalidAmount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.NewTransferRequest(tt.key, tt.from, tt.to, tt.amount)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
