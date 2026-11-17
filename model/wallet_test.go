package model_test

import (
	"math"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
)

func TestNewWallet_Valid(t *testing.T) {
	t.Parallel()
	w, err := model.NewWallet("  wallet_1  ", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != "wallet_1" {
		t.Fatalf("id=%q", w.ID)
	}
	if w.OpeningBalance != 1000 || w.Balance != 1000 {
		t.Fatalf("balances opening=%d balance=%d", w.OpeningBalance, w.Balance)
	}
}

func TestNewWallet_RejectsEmptyAndNegative(t *testing.T) {
	t.Parallel()
	if _, err := model.NewWallet("   ", 0); err != model.ErrInvalidWalletID {
		t.Fatalf("empty id: %v", err)
	}
	if _, err := model.NewWallet("w", -5); err != model.ErrNegativeBalance {
		t.Fatalf("negative: %v", err)
	}
}

func TestWallet_CanDebit(t *testing.T) {
	t.Parallel()
	w, _ := model.NewWallet("w", 50)
	amt, _ := model.NewAmount(50)
	if !w.CanDebit(amt) {
		t.Fatal("expected can debit exact balance")
	}
	over, _ := model.NewAmount(51)
	if w.CanDebit(over) {
		t.Fatal("expected cannot overdraw")
	}
}

func TestWallet_CanCredit(t *testing.T) {
	t.Parallel()
	w, _ := model.NewWallet("w", 50)
	ok, _ := model.NewAmount(10)
	if !w.CanCredit(ok) {
		t.Fatal("expected can credit within headroom")
	}
	if w.CanCredit(0) || w.CanCredit(-1) {
		t.Fatal("non-positive amount must not credit")
	}

	maxed := &model.Wallet{Balance: helpers.Money(math.MaxInt64)}
	if maxed.CanCredit(helpers.Money(1)) {
		t.Fatal("max balance + 1 must not credit")
	}
	nearMax := &model.Wallet{Balance: helpers.Money(math.MaxInt64 - 1)}
	if !nearMax.CanCredit(helpers.Money(1)) {
		t.Fatal("MaxInt64-1 + 1 must fit")
	}
	if nearMax.CanCredit(helpers.Money(2)) {
		t.Fatal("MaxInt64-1 + 2 must not credit")
	}
	empty := &model.Wallet{Balance: 0}
	if !empty.CanCredit(helpers.Money(math.MaxInt64)) {
		t.Fatal("0 + MaxInt64 must fit")
	}
}
