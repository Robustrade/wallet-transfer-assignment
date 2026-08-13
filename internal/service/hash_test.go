package service

import "testing"

// TestRequestHash_NoDelimiterCollision guards against the exact collision a
// naive "|"-joined preimage allows: a wallet ID containing the delimiter can
// make two genuinely different (from, to) pairs hash identically.
func TestRequestHash_NoDelimiterCollision(t *testing.T) {
	a := CreateTransferInput{FromWalletID: "a|b", ToWalletID: "c", Amount: 100}
	b := CreateTransferInput{FromWalletID: "a", ToWalletID: "b|c", Amount: 100}

	if requestHash(a) == requestHash(b) {
		t.Fatalf("requestHash collided for distinct transfers: from=%q,to=%q and from=%q,to=%q both hashed to %s",
			a.FromWalletID, a.ToWalletID, b.FromWalletID, b.ToWalletID, requestHash(a))
	}
}

func TestRequestHash_SameInputSameHash(t *testing.T) {
	in := CreateTransferInput{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	if requestHash(in) != requestHash(in) {
		t.Fatalf("requestHash must be deterministic for identical input")
	}
}
