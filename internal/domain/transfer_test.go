package domain

import (
	"errors"
	"testing"
)

func TestTransferRequest_Validate(t *testing.T) {
	base := TransferRequest{
		IdempotencyKey: "key1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	cases := []struct {
		name    string
		mutate  func(r TransferRequest) TransferRequest
		wantErr error
	}{
		{"valid", func(r TransferRequest) TransferRequest { return r }, nil},
		{"missing idempotency key", func(r TransferRequest) TransferRequest { r.IdempotencyKey = ""; return r }, ErrMissingField},
		{"missing from wallet", func(r TransferRequest) TransferRequest { r.FromWalletID = ""; return r }, ErrMissingField},
		{"missing to wallet", func(r TransferRequest) TransferRequest { r.ToWalletID = ""; return r }, ErrMissingField},
		{"same wallet", func(r TransferRequest) TransferRequest { r.ToWalletID = r.FromWalletID; return r }, ErrSameWallet},
		{"zero amount", func(r TransferRequest) TransferRequest { r.Amount = 0; return r }, ErrInvalidAmount},
		{"negative amount", func(r TransferRequest) TransferRequest { r.Amount = -50; return r }, ErrInvalidAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mutate(base).Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error wrapping %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestTransfer_StateTransitions(t *testing.T) {
	t.Run("pending can move to processed", func(t *testing.T) {
		tr := NewTransfer("wallet_1", "wallet_2", 100)
		if err := tr.MarkProcessed(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.Status != TransferProcessed {
			t.Fatalf("expected PROCESSED, got %s", tr.Status)
		}
	})

	t.Run("pending can move to failed with reason", func(t *testing.T) {
		tr := NewTransfer("wallet_1", "wallet_2", 100)
		if err := tr.MarkFailed("insufficient funds"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.Status != TransferFailed {
			t.Fatalf("expected FAILED, got %s", tr.Status)
		}
		if tr.FailureReason == nil || *tr.FailureReason != "insufficient funds" {
			t.Fatalf("expected failure reason to be set")
		}
	})

	t.Run("processed cannot transition again", func(t *testing.T) {
		tr := NewTransfer("wallet_1", "wallet_2", 100)
		if err := tr.MarkProcessed(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tr.MarkFailed("late failure"); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected ErrInvalidTransition, got %v", err)
		}
	})

	t.Run("failed cannot transition again", func(t *testing.T) {
		tr := NewTransfer("wallet_1", "wallet_2", 100)
		if err := tr.MarkFailed("first failure"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tr.MarkProcessed(); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected ErrInvalidTransition, got %v", err)
		}
	})
}

func TestLedgerEntriesForTransfer_Balances(t *testing.T) {
	tr := NewTransfer("wallet_1", "wallet_2", 250)
	entries := LedgerEntriesForTransfer(tr)

	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 entries, got %d", len(entries))
	}

	var debit, credit *LedgerEntry
	for i := range entries {
		switch entries[i].EntryType {
		case EntryDebit:
			debit = &entries[i]
		case EntryCredit:
			credit = &entries[i]
		}
	}

	if debit == nil || credit == nil {
		t.Fatalf("expected one DEBIT and one CREDIT entry")
	}
	if debit.WalletID != tr.FromWalletID || credit.WalletID != tr.ToWalletID {
		t.Fatalf("entries point at the wrong wallets")
	}
	if debit.Amount != credit.Amount || debit.Amount != tr.Amount {
		t.Fatalf("debit and credit amounts must both equal the transfer amount: debit=%d credit=%d amount=%d", debit.Amount, credit.Amount, tr.Amount)
	}
}
