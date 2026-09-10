package domain

import (
	"testing"
	"time"
)

func TestNewTransferRejectsInvalidInput(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   CreateTransferInput
		want error
	}{
		{
			name: "missing key",
			in:   CreateTransferInput{ID: "t1", FromWalletID: "a", ToWalletID: "b", Amount: 1, Now: now},
			want: ErrMissingIdempotencyKey,
		},
		{
			name: "same wallet",
			in:   CreateTransferInput{ID: "t1", IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "a", Amount: 1, Now: now},
			want: ErrSameWallet,
		},
		{
			name: "non-positive amount",
			in:   CreateTransferInput{ID: "t1", IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 0, Now: now},
			want: ErrInvalidAmount,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTransfer(tc.in)
			if err != tc.want {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestTransferStateMachine(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	tr, err := NewTransfer(CreateTransferInput{
		ID: "t1", IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 10, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Status != StatusPending {
		t.Fatalf("new transfer status = %s", tr.Status)
	}

	processed, err := tr.MarkProcessed(now)
	if err != nil {
		t.Fatal(err)
	}
	if processed.Status != StatusProcessed {
		t.Fatalf("status = %s", processed.Status)
	}
	if _, err := processed.MarkFailed("nope", now); err != ErrIllegalTransition {
		t.Fatalf("terminal processed allowed fail: %v", err)
	}

	failed, err := tr.MarkFailed("insufficient_funds", now)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.FailureReason != "insufficient_funds" {
		t.Fatalf("failed = %+v", failed)
	}
	if _, err := failed.MarkProcessed(now); err != ErrIllegalTransition {
		t.Fatalf("terminal failed allowed process: %v", err)
	}
}

func TestLedgerPostingBalances(t *testing.T) {
	now := time.Now()
	debit, credit, err := NewPosting("t1", "a", "b", "d1", "c1", 50, now)
	if err != nil {
		t.Fatal(err)
	}
	if !debit.Balances(credit) {
		t.Fatalf("expected balanced posting: %+v %+v", debit, credit)
	}
	credit.Amount = 49
	if debit.Balances(credit) {
		t.Fatal("unbalanced posting accepted")
	}
}

func TestRequestHashStableAndDistinct(t *testing.T) {
	a := RequestHash("w1", "w2", 100)
	b := RequestHash("w1", "w2", 100)
	c := RequestHash("w1", "w2", 101)
	if a != b {
		t.Fatal("hash should be stable")
	}
	if a == c {
		t.Fatal("different payload must hash differently")
	}
}
