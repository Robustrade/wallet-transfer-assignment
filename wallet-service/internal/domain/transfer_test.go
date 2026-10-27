package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestValidateRequest(t *testing.T) {
	valid := Request{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: 100}
	for _, tc := range []struct {
		name   string
		change func(*Request)
		want   error
	}{
		{"valid", func(*Request) {}, nil},
		{"maximum amount", func(r *Request) { r.Amount = math.MaxInt64 }, nil},
		{"zero", func(r *Request) { r.Amount = 0 }, ErrInvalidRequest},
		{"negative", func(r *Request) { r.Amount = -1 }, ErrInvalidRequest},
		{"missing key", func(r *Request) { r.IdempotencyKey = "" }, ErrInvalidRequest},
		{"long key", func(r *Request) { r.IdempotencyKey = strings.Repeat("a", 129) }, ErrInvalidRequest},
		{"unicode key", func(r *Request) { r.IdempotencyKey = strings.Repeat("₹", 128) }, nil},
		{"missing wallet", func(r *Request) { r.ToWalletID = "" }, ErrInvalidRequest},
		{"nul", func(r *Request) { r.FromWalletID = "a\x00" }, ErrInvalidRequest},
		{"invalid utf8", func(r *Request) { r.IdempotencyKey = string([]byte{0xff}) }, ErrInvalidRequest},
		{"same wallet", func(r *Request) { r.ToWalletID = "a" }, ErrSelfTransfer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.change(&r)
			if err := ValidateRequest(r); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWalletRules(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to Wallet
		amount   int64
		want     error
	}{
		{"funded", Wallet{ID: "a", Currency: "INR", Balance: 500}, Wallet{ID: "b", Currency: "INR", Balance: 200}, 100, nil},
		{"exact funds", Wallet{Balance: 100}, Wallet{}, 100, nil},
		{"insufficient", Wallet{Balance: 99}, Wallet{}, 100, ErrInsufficientFunds},
		{"overflow", Wallet{Balance: 100}, Wallet{Balance: math.MaxInt64}, 1, ErrBalanceOverflow},
		{"maximum credit", Wallet{Balance: 1}, Wallet{Balance: math.MaxInt64 - 1}, 1, nil},
		{"check funds first", Wallet{}, Wallet{Balance: math.MaxInt64}, 1, ErrInsufficientFunds},
		{"invalid amount", Wallet{}, Wallet{}, 0, ErrInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckTransferBalances(tc.from, tc.to, tc.amount); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	for _, currency := range []string{"INR", "USD", "JPY", "KWD"} {
		if err := ValidateWalletPair(Wallet{Currency: currency}, Wallet{Currency: currency}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateWalletPair(Wallet{Currency: "INR"}, Wallet{Currency: "USD"}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatal(err)
	}
	if err := ValidateWalletPair(Wallet{Currency: "XXX"}, Wallet{Currency: "XXX"}); !errors.Is(err, ErrUnsupportedCurrency) {
		t.Fatal(err)
	}
}

func TestTerminalTransitions(t *testing.T) {
	for _, current := range []Status{Pending, Processed, Failed, "UNKNOWN"} {
		for _, next := range []Status{Pending, Processed, Failed, "UNKNOWN"} {
			for _, code := range []string{"", string(ErrInsufficientFunds), string(ErrBalanceOverflow), "UNKNOWN"} {
				valid := current == Pending && ((next == Processed && code == "") || (next == Failed && (code == string(ErrInsufficientFunds) || code == string(ErrBalanceOverflow))))
				if err := ValidateTransition(current, next, code); (err == nil) != valid {
					t.Fatalf("%s -> %s (%s): %v", current, next, code, err)
				}
			}
		}
	}
}
