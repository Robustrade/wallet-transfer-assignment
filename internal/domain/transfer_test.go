package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

func request(key, from, to string, amount int64) domain.TransferRequest {
	return domain.TransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount}
}

func TestTransferRequestValidate(t *testing.T) {
	cases := []struct {
		name string
		req  domain.TransferRequest
		want error
	}{
		{"valid", request("k", "a", "b", 1), nil},
		{"missing key", request("", "a", "b", 1), domain.ErrMissingField},
		{"missing source", request("k", "", "b", 1), domain.ErrMissingField},
		{"missing destination", request("k", "a", "", 1), domain.ErrMissingField},
		{"zero amount", request("k", "a", "b", 0), domain.ErrInvalidAmount},
		{"negative amount", request("k", "a", "b", -1), domain.ErrInvalidAmount},
		{"same wallet", request("k", "a", "a", 1), domain.ErrSameWallet},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTransferRequestHash(t *testing.T) {
	base := request("k", "a", "b", 100)

	if base.Hash() != request("different-key", "a", "b", 100).Hash() {
		t.Error("hash changed with the idempotency key, which it must ignore")
	}

	for name, other := range map[string]domain.TransferRequest{
		"amount":         request("k", "a", "b", 101),
		"source":         request("k", "c", "b", 100),
		"destination":    request("k", "a", "c", 100),
		"field boundary": request("k", "a|b", "", 100),
	} {
		if base.Hash() == other.Hash() {
			t.Errorf("hash ignores a change of %s", name)
		}
	}
}

func TestTransferStatusIsTerminal(t *testing.T) {
	cases := map[domain.TransferStatus]bool{
		domain.StatusPending:   false,
		domain.StatusProcessed: true,
		domain.StatusFailed:    true,
	}
	for status, want := range cases {
		if got := status.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %t, want %t", status, got, want)
		}
	}
}

func TestNewLedgerPairBalances(t *testing.T) {
	transfer := domain.NewTransfer(request("k", "alice", "bob", 250), time.Now().UTC())
	debit, credit := domain.NewLedgerPair(transfer, transfer.CreatedAt)

	if debit.WalletID != "alice" || debit.EntryType != domain.EntryDebit {
		t.Errorf("debit = %+v, want a DEBIT against alice", debit)
	}
	if credit.WalletID != "bob" || credit.EntryType != domain.EntryCredit {
		t.Errorf("credit = %+v, want a CREDIT to bob", credit)
	}
	if debit.Amount != credit.Amount || debit.Amount != 250 {
		t.Errorf("debit %d and credit %d must both equal the transfer amount 250", debit.Amount, credit.Amount)
	}
	if debit.TransferID != transfer.ID || credit.TransferID != transfer.ID {
		t.Error("both entries must reference the transfer they record")
	}
	if debit.ID == credit.ID {
		t.Error("the two entries must have distinct identifiers")
	}
}

func TestNewTransferStartsPending(t *testing.T) {
	transfer := domain.NewTransfer(request("k", "alice", "bob", 10), time.Now().UTC())
	if transfer.Status != domain.StatusPending {
		t.Errorf("status = %s, want PENDING", transfer.Status)
	}
	if transfer.ID == "" {
		t.Error("transfer has no identifier")
	}
}
