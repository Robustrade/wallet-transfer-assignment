package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// TestNewID verifies that NewID produces a non-zero UUID v7.
func TestNewID(t *testing.T) {
	t.Parallel()

	id := domain.NewID()
	if id.Version() != 7 {
		t.Fatalf("expected UUID version 7, got %d", id.Version())
	}

	zeroID := domain.ZeroID()
	if id == zeroID {
		t.Fatal("NewID must not return the zero UUID")
	}
}

// TestNewID_Unique verifies that two consecutive calls produce different IDs.
func TestNewID_Unique(t *testing.T) {
	t.Parallel()

	a := domain.NewID()
	b := domain.NewID()

	if a == b {
		t.Fatal("consecutive calls to NewID must produce unique IDs")
	}
}

// TestTransferStatus_String verifies integer status constants map to the
// expected human-readable strings.
func TestTransferStatus_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status domain.TransferStatus
		want   string
	}{
		{domain.TransferStatusPending, "PENDING"},
		{domain.TransferStatusProcessed, "PROCESSED"},
		{domain.TransferStatusFailed, "FAILED"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.status.String(); got != tc.want {
				t.Fatalf("TransferStatus(%d).String() = %q, want %q", int(tc.status), got, tc.want)
			}
		})
	}
}

// TestEntryType_String verifies integer entry type constants map to expected
// strings.
func TestEntryType_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ  domain.EntryType
		want string
	}{
		{domain.EntryTypeDebit, "DEBIT"},
		{domain.EntryTypeCredit, "CREDIT"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.typ.String(); got != tc.want {
				t.Fatalf("EntryType(%d).String() = %q, want %q", int(tc.typ), got, tc.want)
			}
		})
	}
}

// TestTransfer_CanTransition verifies only valid state transitions are allowed.
func TestTransfer_CanTransition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		from  domain.TransferStatus
		to    domain.TransferStatus
		allow bool
	}{
		{domain.TransferStatusPending, domain.TransferStatusProcessed, true},
		{domain.TransferStatusPending, domain.TransferStatusFailed, true},
		{domain.TransferStatusProcessed, domain.TransferStatusFailed, false},
		{domain.TransferStatusFailed, domain.TransferStatusProcessed, false},
		{domain.TransferStatusPending, domain.TransferStatusPending, false},
	}

	for _, tc := range cases {
		t.Run(tc.from.String()+"->"+tc.to.String(), func(t *testing.T) {
			t.Parallel()
			got := domain.CanTransition(tc.from, tc.to)
			if got != tc.allow {
				t.Fatalf("CanTransition(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.allow)
			}
		})
	}
}

// TestNewTransfer verifies that NewTransfer creates a transfer in PENDING state
// with the provided fields and a non-zero ID.
func TestNewTransfer(t *testing.T) {
	t.Parallel()

	fromID := domain.NewID()
	toID := domain.NewID()
	key := "idem-key-001"
	amount := domain.Amount(150_00) // 150.00 in minor units

	tr := domain.NewTransfer(fromID, toID, key, amount)

	if tr.ID == domain.ZeroID() {
		t.Fatal("NewTransfer must assign a non-zero ID")
	}
	if tr.ID.Version() != 7 {
		t.Fatalf("transfer ID must be UUID v7, got version %d", tr.ID.Version())
	}
	if tr.Status != domain.TransferStatusPending {
		t.Fatalf("new transfer must be PENDING, got %s", tr.Status)
	}
	if tr.FromWalletID != fromID {
		t.Fatal("FromWalletID mismatch")
	}
	if tr.ToWalletID != toID {
		t.Fatal("ToWalletID mismatch")
	}
	if tr.IdempotencyKey != key {
		t.Fatal("IdempotencyKey mismatch")
	}
	if tr.Amount != amount {
		t.Fatal("Amount mismatch")
	}
}

// TestNewWallet verifies a new wallet starts with a zero balance and a valid ID.
func TestNewWallet(t *testing.T) {
	t.Parallel()

	w := domain.NewWallet()

	if w.ID == domain.ZeroID() {
		t.Fatal("NewWallet must assign a non-zero ID")
	}
	if w.ID.Version() != 7 {
		t.Fatalf("wallet ID must be UUID v7, got version %d", w.ID.Version())
	}
	if w.Balance != 0 {
		t.Fatalf("new wallet must have zero balance, got %d", w.Balance)
	}
}

// TestTransferStatus_JSON verifies that TransferStatus marshals to a
// human-readable string and round-trips correctly through JSON.
func TestTransferStatus_JSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status domain.TransferStatus
		want   string
	}{
		{domain.TransferStatusPending, `"PENDING"`},
		{domain.TransferStatusProcessed, `"PROCESSED"`},
		{domain.TransferStatusFailed, `"FAILED"`},
	}

	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			t.Parallel()

			b, err := json.Marshal(tc.status)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if string(b) != tc.want {
				t.Fatalf("MarshalJSON: got %s, want %s", b, tc.want)
			}

			var got domain.TransferStatus
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got != tc.status {
				t.Fatalf("round-trip: got %s, want %s", got, tc.status)
			}
		})
	}
}

// TestNewLedgerEntry verifies that NewLedgerEntry populates all required fields
// and assigns a UUID v7 ID.
func TestNewLedgerEntry(t *testing.T) {
	t.Parallel()

	transferID := domain.NewID()
	walletID := domain.NewID()
	amount := domain.Amount(500_00)

	debit := domain.NewLedgerEntry(transferID, walletID, domain.EntryTypeDebit, amount)
	credit := domain.NewLedgerEntry(transferID, walletID, domain.EntryTypeCredit, amount)

	if debit.ID == domain.ZeroID() {
		t.Fatal("ledger entry must have a non-zero ID")
	}
	if debit.ID.Version() != 7 {
		t.Fatalf("ledger entry ID must be UUID v7, got version %d", debit.ID.Version())
	}
	if debit.Type != domain.EntryTypeDebit {
		t.Fatalf("expected DEBIT, got %s", debit.Type)
	}
	if credit.Type != domain.EntryTypeCredit {
		t.Fatalf("expected CREDIT, got %s", credit.Type)
	}
	if debit.Amount != amount {
		t.Fatal("amount mismatch on debit")
	}
}
