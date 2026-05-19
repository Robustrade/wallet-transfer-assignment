package service

import (
	"testing"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

func TestHashCreateTransferInput_deterministic(t *testing.T) {
	input := CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	h1 := hashCreateTransferInput(input)
	h2 := hashCreateTransferInput(input)
	if h1 != h2 {
		t.Fatalf("hash mismatch: %q vs %q", h1, h2)
	}

	input.Amount = 200
	h3 := hashCreateTransferInput(input)
	if h1 == h3 {
		t.Fatal("expected different hash for different amount")
	}
}

func TestEncodeDecodeTransferResult_roundTrip(t *testing.T) {
	want := &CreateTransferResult{
		TransferID: "t-1",
		State:      domain.TransferStateProcessed,
		Amount:     100,
		Currency:   "USD",
	}

	body, err := encodeTransferResult(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, err := decodeTransferResult(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TransferID != want.TransferID || got.Amount != want.Amount ||
		got.Currency != want.Currency || got.State != want.State {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestValidateWalletsForTransfer(t *testing.T) {
	source := &domain.Wallet{ID: "w1", Balance: 100, Currency: "USD"}
	target := &domain.Wallet{ID: "w2", Balance: 0, Currency: "USD"}

	if err := validateWalletsForTransfer(source, target, 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := validateWalletsForTransfer(source, target, 150); err != domain.ErrInsufficientFunds {
		t.Fatalf("got %v, want ErrInsufficientFunds", err)
	}

	target.Currency = "EUR"
	if err := validateWalletsForTransfer(source, target, 10); err != domain.ErrInvalidTransfer {
		t.Fatalf("got %v, want ErrInvalidTransfer", err)
	}
}
