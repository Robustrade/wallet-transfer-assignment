package mapper

import (
	"testing"
	"time"

	db "wallet-transfer-assigment/internal/db/generated"
	"wallet-transfer-assigment/models"
)

func TestMappToWalletResponse(t *testing.T) {
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	mapper := Mapp{}
	input := db.Wallet{
		WalletID:  "wallet_1",
		Balance:   1250,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	got := mapper.ToWalletResponse(input)
	want := models.Wallet{
		WalletID:  input.WalletID,
		Balance:   input.Balance,
		CreatedAt: input.CreatedAt,
		UpdatedAt: input.UpdatedAt,
	}

	if got != want {
		t.Fatalf("wallet mapping mismatch: got %#v, want %#v", got, want)
	}
}

func TestMappToWalletsResponse(t *testing.T) {
	mapper := Mapp{}
	input := []db.Wallet{
		{WalletID: "wallet_1", Balance: 100},
		{WalletID: "wallet_2", Balance: 200},
	}

	got := mapper.ToWalletsResponse(input)
	if len(got) != len(input) {
		t.Fatalf("mapped wallet count: got %d, want %d", len(got), len(input))
	}
	for index, wallet := range input {
		if got[index].WalletID != wallet.WalletID || got[index].Balance != wallet.Balance {
			t.Errorf("wallet %d mapping mismatch: got %#v, want wallet ID %q and balance %d", index, got[index], wallet.WalletID, wallet.Balance)
		}
	}

	empty := mapper.ToWalletsResponse(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty wallet mapping should return a non-nil empty slice, got %#v", empty)
	}
}

func TestMappToCreateTransferParams(t *testing.T) {
	mapper := Mapp{}
	input := models.TransferRequest{
		IdempotencyKey: "request_1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         250,
	}

	got := mapper.ToCreateTransferParams(input)
	want := db.CreateTransferParams{
		FromWalletID: input.FromWalletID,
		ToWalletID:   input.ToWalletID,
		Amount:       input.Amount,
		Status:       "PENDING",
	}

	if got != want {
		t.Fatalf("transfer parameter mapping mismatch: got %#v, want %#v", got, want)
	}

	if got.TransactionID != "" {
		t.Errorf("mapper should leave transaction ID for the persistence layer, got %q", got.TransactionID)
	}
}

func TestNewMapper(t *testing.T) {
	if NewMapper(nil) == nil {
		t.Fatal("new mapper returned nil")
	}
}
