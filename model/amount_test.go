package model_test

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

func TestNewAmount_RejectsNonPositive(t *testing.T) {
	t.Parallel()
	for _, v := range []int64{0, -1, -100} {
		if _, err := model.NewAmount(v); err != model.ErrInvalidAmount {
			t.Fatalf("NewAmount(%d): got %v, want ErrInvalidAmount", v, err)
		}
	}
}

func TestNewAmount_AcceptsPositive(t *testing.T) {
	t.Parallel()
	m, err := model.NewAmount(1)
	if err != nil || m.Int64() != 1 {
		t.Fatalf("got %v %v", m, err)
	}
}

func TestNewBalance(t *testing.T) {
	t.Parallel()
	if _, err := model.NewBalance(0); err != nil {
		t.Fatal(err)
	}
	if _, err := model.NewBalance(-1); err != model.ErrNegativeBalance {
		t.Fatalf("got %v", err)
	}
}

func TestNormalizeIDAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	id, err := model.NormalizeID("  w1  ")
	if err != nil || id != "w1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if _, err := model.NormalizeID("  "); err != model.ErrInvalidWalletID {
		t.Fatalf("got %v", err)
	}
	key, err := model.NormalizeIdempotencyKey("  k  ")
	if err != nil || key != "k" {
		t.Fatalf("key=%q err=%v", key, err)
	}
	if _, err := model.NormalizeIdempotencyKey(""); err != model.ErrInvalidIdempotencyKey {
		t.Fatalf("got %v", err)
	}
}
