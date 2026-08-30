package domain

import "testing"

func TestCreateTransferInputValidate(t *testing.T) {
	err := CreateTransferInput{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "a", Amount: 1}.Validate()
	if err == nil {
		t.Fatal("expected error for same wallets")
	}
	err = CreateTransferInput{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 0}.Validate()
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
	err = CreateTransferInput{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 10}.Validate()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCanTransition(t *testing.T) {
	if err := CanTransition(StatusPending, StatusProcessed); err != nil {
		t.Fatal(err)
	}
	if err := CanTransition(StatusProcessed, StatusFailed); err == nil {
		t.Fatal("processed -> failed should be illegal")
	}
}

func TestRequestHashStable(t *testing.T) {
	a := CreateTransferInput{FromWalletID: "w1", ToWalletID: "w2", Amount: 100}.RequestHash()
	b := CreateTransferInput{FromWalletID: "w1", ToWalletID: "w2", Amount: 100}.RequestHash()
	c := CreateTransferInput{FromWalletID: "w1", ToWalletID: "w2", Amount: 101}.RequestHash()
	if a != b {
		t.Fatal("hash should be stable")
	}
	if a == c {
		t.Fatal("hash should change with amount")
	}
}
