package domain

import "testing"

func TestCanTransition(t *testing.T) {
	t.Parallel()
	if !CanTransition(StatusPending, StatusProcessed) {
		t.Fatal("pending to processed is allowed")
	}
	if !CanTransition(StatusPending, StatusFailed) {
		t.Fatal("pending to failed is allowed")
	}
	if CanTransition(StatusProcessed, StatusPending) {
		t.Fatal("processed is terminal")
	}
	if CanTransition(StatusFailed, StatusProcessed) {
		t.Fatal("failed is terminal")
	}
	if CanTransition(StatusProcessed, StatusProcessed) {
		t.Fatal("processed cannot transition to itself")
	}
}

func TestValidateTransfer(t *testing.T) {
	t.Parallel()
	valid := func() error {
		return ValidateTransfer("wallet_1", "wallet_2", "abc123", 100)
	}
	if err := valid(); err != nil {
		t.Fatalf("valid request: %v", err)
	}

	cases := []struct {
		name string
		from string
		to   string
		key  string
		amt  int64
	}{
		{name: "zero amount", from: "wallet_1", to: "wallet_2", key: "abc123", amt: 0},
		{name: "negative amount", from: "wallet_1", to: "wallet_2", key: "abc123", amt: -5},
		{name: "same wallet", from: "wallet_1", to: "wallet_1", key: "abc123", amt: 10},
		{name: "empty key", from: "wallet_1", to: "wallet_2", key: "", amt: 10},
		{name: "key with space", from: "wallet_1", to: "wallet_2", key: "bad key", amt: 10},
		{name: "null byte", from: "wallet_1", to: "wall\x00et", key: "abc123", amt: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTransfer(tc.from, tc.to, tc.key, tc.amt)
			if err == nil {
				t.Fatal("expected an input error")
			}
		})
	}
}

func TestFingerprintIgnoresNothingButPayload(t *testing.T) {
	t.Parallel()
	a := Fingerprint("wallet_1", "wallet_2", 100)
	b := Fingerprint("wallet_1", "wallet_2", 100)
	c := Fingerprint("wallet_1", "wallet_2", 101)
	if a != b {
		t.Fatal("same payload produced two hashes")
	}
	if a == c {
		t.Fatal("different amounts produced one hash")
	}
}

func TestCreditOverflow(t *testing.T) {
	t.Parallel()
	if CreditOverflow(10, 5) {
		t.Fatal("small credit does not overflow")
	}
	if !CreditOverflow(1<<62, 1<<62) {
		t.Fatal("large credit overflows int64")
	}
}
